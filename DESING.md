# TranscodeX : Design & Trade-offs

This document explains the internal design decisions, trade-offs, and alternative approaches considered while building TranscodeX.

The goal of this project is not to build a production-ready video platform, but to deeply understand the mechanics behind distributed job processing: queue semantics, worker lifecycle, failure isolation, and infrastructure as code.

---

# 1: Queue Selection : Why SQS

## Decision
SQS was chosen as the job queue over alternatives like RabbitMQ, Kafka, or a simple database queue.

## Why SQS?
- Managed service ; no broker to operate, patch, or monitor
- Native visibility timeout semantics ; exactly what we need for at-least-once delivery
- Built-in DLQ support ; redrive policy configured via Terraform
- LocalStack compatibility ; full SQS emulation locally with zero code changes for production

## Why Not RabbitMQ?
The first version of this project (transcodeX v1) used RabbitMQ. The overhead of operating a broker, managing connections, heartbeats, channel lifecycle, added complexity without adding value for this use case. SQS offloads that entirely.

## Why Not Kafka?
Kafka is optimized for high-throughput event streaming with consumer groups and replay semantics. Video transcoding jobs are not events to be replayed; they are tasks to be executed once. Kafka's operational complexity is not justified here.

## Why Not a Database Queue?
A common pattern is to use a database table as a queue, poll for pending rows, mark as processing. This works but requires implementing visibility timeout, retry counting, and DLQ logic manually. SQS provides all of this natively.

## Trade-off
SQS couples the system to AWS. Switching to a different queue would require code changes in both the upload service (publish) and worker service (consume). This is an acceptable trade-off given the infrastructure-as-code approach; the coupling is explicit and isolated to two files.

---

# 2: Delivery Semantics : At-Least-Once

## Decision
The system implements **at-least-once delivery**.

## Why?
- SQS's native delivery guarantee is at-least-once
- Visibility timeout mechanism means a message can be redelivered if a worker crashes
- Simpler to reason about than exactly-once

## Trade-off
Duplicate processing is possible. A worker can process a job, succeed, and crash before deleting the SQS message. The message becomes visible again and another worker picks it up.

## Mitigation: Idempotency
The system is designed to be idempotent; processing the same job twice produces the same result:
- S3 key is deterministic (`processed/{jobId}/output.mp4`) second write overwrites first
- DynamoDB uses `PutItem` / `UpdateItem` second write overwrites first
- No side effects accumulate

**Idempotency test result:** Same job processed twice. No crash, no data corruption, final state identical. Duplicate detection was intentionally not implemented. The cost (extra DynamoDB read + conditional write on every job) exceeds the benefit given idempotent operations.

## Why Not Exactly-Once?
Exactly-once requires:
- Persistent deduplication tracking (DynamoDB jobId check before processing)
- Conditional writes to prevent double-update
- Distributed consensus in edge cases

This significantly increases complexity. The goal of this project is to understand delivery semantics, not build a full transactional system.

---

# 3: Visibility Timeout

## Decision
Initial visibility timeout: **300 seconds (5 minutes)**.
Extend interval: **30 seconds** (extended during processing).

## Why Needed?
When a worker receives a SQS message, the message becomes invisible to other workers for the visibility timeout duration. If the worker crashes mid-processing, the message automatically becomes visible again after the timeout; enabling recovery without explicit nack.

Without visibility timeout management:
- Short timeout -> false redelivery during long transcoding jobs
- Long timeout -> slow recovery after worker crash

## Dynamic Extension
A background goroutine (`VisibilityExtender`) continuously extends the timeout every 30 seconds while the job is in flight. When the job completes (success or failure), the goroutine is cancelled via context.

```
Job start -> extender goroutine starts
  every 30s: ChangeMessageVisibility(+300s)
Job end -> context cancel -> extender stops
```

**Visibility timeout extend test result:** 10-second artificial delay. Extender fired twice at 5s intervals. Job completed without duplicate delivery.

## Why 300 Seconds as Base?
A conservative estimate for transcoding duration. For real production workloads, this should be tuned to `p99 transcoding time * 1.5`. The dynamic extension mechanism means the base value is a fallback, not a hard limit.

## Trade-off
Short timeout:
- Faster recovery after crash
- Higher false redelivery risk for long jobs

Long timeout:
- Slower recovery after crash
- Lower duplicate probability

The dynamic extension approach decouples base timeout from job duration, eliminating this trade-off in practice.

---

# 4: Worker Concurrency

## Decision
Default `WORKER_COUNT=2`, configurable via environment variable.

## Why Not 1?
Single worker leaves CPU underutilized during S3 download/upload I/O; two workers can process concurrently during I/O-bound phases.

## Why Not 4+?
FFmpeg is CPU-intensive. Each FFmpeg process uses ~80% of a single core. On a t2.micro (1 vCPU, 1GB RAM):
- 2 concurrent FFmpeg processes: manageable
- 4 concurrent FFmpeg processes: potential memory exhaustion with large video files

## Benchmark Results : Worker Scaling (4 concurrent jobs)

| Workers | Throughput | Avg e2e | Min e2e | Max e2e |
|---------|-----------|---------|---------|---------|
| 1       | 0.97/sec  | 2.70s   | 1.31s   | 4.12s   |
| 2       | 1.14/sec  | 2.73s   | 1.98s   | 3.52s   |
| 4       | 1.44/sec  | 2.75s   | 2.71s   | 2.78s   |

**Observations:**
- 4 workers provides the best throughput (+48% vs 1 worker)
- 4 workers shows the tightest latency distribution (min/max spread: 70ms vs 2.81s for 1 worker). All jobs processed simultaneously, no queue wait
- Test videos were small (372KB). For production-sized videos, 4 workers on t2.micro would risk OOM. `WORKER_COUNT` should be tuned based on actual video size distribution and available memory.

## Benchmark Results : Throughput Under Load (2 workers)

| Concurrent Jobs | Throughput  | Avg e2e | Min e2e | Max e2e |
|-----------------|------------|---------|---------|---------|
| 1               | 1.02/sec   | 977ms   | 977ms   | 977ms   |
| 2               | 1.36/sec   | 1.45s   | 1.44s   | 1.46s   |
| 4               | 1.34/sec   | 2.33s   | 1.61s   | 2.98s   |
| 8               | 1.25/sec   | 4.07s   | 1.77s   | 6.40s   |

**Observation:** Throughput plateaus at ~1.3 jobs/sec with 2 workers. Beyond 2 concurrent jobs, jobs queue in the channel and wait for a free worker. This is expected and correct behavior, the system applies backpressure rather than overloading the worker pool.

## Why Configurable?
`WORKER_COUNT` is read from environment variable. Different deployment targets have different resources:
- Local dev (t2.micro equivalent): 2
- Staging: 2
- Production (c5.xlarge, 4 vCPU): 4

No code change required, only environment configuration.

---

# 5: Multipart Upload

## Decision
Files under 100MB use `PutObject` (single upload). Files 100MB and above use S3 multipart upload.

## Why Multipart?
Single `PutObject` for large files has several failure modes:
- Network interruption -> entire upload fails, must restart from zero
- Memory: the entire file body must be held in memory during upload
- S3 single PUT limit: 5GB

Multipart upload splits the file into 10MB parts. Each part is uploaded independently. On failure, only the failed part needs to be retried.

## Abort on Failure
If any part upload fails, `AbortMultipartUpload` is called immediately. This prevents orphaned incomplete uploads accumulating in S3 (which would incur storage costs in production).

## Why 100MB Threshold?
AWS recommends multipart for files over 100MB. Below that, the overhead of initiating a multipart upload (extra API calls) outweighs the benefit.

## Trade-off
The current implementation buffers each part in memory (`buf := make([]byte, partSize)`). An alternative is streaming directly from the multipart reader to S3 without buffering. The current approach is simpler and acceptable given the 10MB part size limit.

---

# 6: Partial Failure & Cleanup

## Problem
FFmpeg can fail mid-execution, the process crashes, context is cancelled, or the output is corrupted. At that point:
- `tmp/{jobId}/input.mov` exists on disk
- `tmp/{jobId}/output.mp4` may be partially written
- S3 does not have the processed video yet
- DynamoDB status is `processing`

## Solution
`exec.CommandContext` is used instead of `exec.Command`. When the context is cancelled (graceful shutdown or job failure), the FFmpeg process is killed by the OS. The `Cleanup` function then calls `os.RemoveAll` on the temp directory; removing both input and partial output.

```
FFmpeg killed -> Cleanup(inputPath, outputPath) -> os.RemoveAll(dir)
DynamoDB status -> "failed"
SQS message -> not deleted -> requeued after visibility timeout
```

**Graceful shutdown test result:** SIGTERM sent during active FFmpeg process. FFmpeg cancelled within 100ms. `tmp/` directory cleaned up. SQS message not deleted, job will be retried by next worker start.

## Trade-off
`os.RemoveAll` deletes the entire `tmp/{jobId}/` directory. If multiple goroutines were sharing a temp directory, this would be dangerous. Each job uses a unique jobId-scoped directory, so this is safe.

---

# 7: Retry & Dead Letter Queue

## Decision
`maxReceiveCount: 3` : after 3 failed attempts, the message is automatically moved to the DLQ by SQS.

## Why 3?
- 1 retry: too aggressive, transient failures (network blip, S3 throttle) would go directly to DLQ
- 3 retries: enough to handle transient failures, not so many that a poison message consumes resources indefinitely
- 5+ retries: delays DLQ isolation, poison messages can thrash the system for too long

## DLQ Purpose
The DLQ isolates poison messages, jobs that consistently fail regardless of retry. Without a DLQ, a permanently broken job would retry forever, consuming worker capacity.

**DLQ test result:** FFmpeg command set to `ffmpeg_broken` (non-existent binary). Job failed 3 times at 10-second intervals. Message moved to DLQ automatically. `ApproximateNumberOfMessages: 1` confirmed in DLQ.

## Trade-off
Messages in the DLQ are not automatically reprocessed. Operational tooling (manual requeue, DLQ replay) is needed for production. This is an intentional omission. The focus is on the delivery mechanics, not operational tooling.

---

# 8: Graceful Shutdown

## Shutdown Sequence
```
SIGTERM / SIGINT received
  -> context cancelled
  -> SQS poller stops (no new messages accepted)
  -> active FFmpeg processes killed via CommandContext
  -> partial files cleaned up
  -> worker goroutines drain and exit
  -> pool.Stop() waits for WaitGroup
  -> process exits
```

## Why Drain Before Exit?
Immediate exit on SIGTERM would leave:
- Orphaned FFmpeg processes (OS handles eventually, but messy)
- Partial temp files on disk
- SQS messages stuck in `processing` state until visibility timeout

Draining ensures clean state.

## Why CommandContext?
`exec.CommandContext` ties the FFmpeg process lifecycle to the Go context. When context is cancelled, the OS sends SIGKILL to the FFmpeg subprocess. This is the only reliable way to kill a child process in Go.

## Trade-off
Current implementation has no drain timeout. `pool.Stop()` waits indefinitely for all workers to finish. In production (Kubernetes), the pod would receive SIGKILL after 30 seconds. A drain timeout (e.g., 25 seconds) would ensure graceful exit within Kubernetes's termination grace period. This is a known limitation.

---

# 9: S3 Storage Design

## Two Buckets
`transcodex-raw-videos` : raw uploaded videos. Temporary storage; could be lifecycle-deleted after processing.

`transcodex-processed-videos` : transcoded output. Permanent storage.

## Key Structure
```
raw:       uploads/{jobId}/{original_filename}
processed: processed/{jobId}/output.mp4
```

`jobId` as path prefix enables:
- Idempotent overwrites (same jobId -> same key)
- Easy cleanup by jobId prefix
- Clear ownership per job

## Why Not a Single Bucket?
Separate buckets allow different lifecycle policies, access controls, and cost allocation. Raw videos can be deleted after processing; processed videos are retained. In production, raw videos would have a 7-day lifecycle rule.

---

# 10: FFmpeg Configuration

## Parameters
```
-vf scale=1280:720    -> 1080p to 720p downscale
-c:v libx264          -> H.264 video codec
-crf 23               -> Constant Rate Factor (quality)
-preset fast          -> encoding speed/compression trade-off
-c:a aac              -> AAC audio codec
-b:a 128k             -> 128kbps audio bitrate
-y                    -> overwrite output if exists (idempotency)
```

## Why CRF 23?
CRF (Constant Rate Factor) produces variable bitrate output at a target quality level. CRF 23 is the FFmpeg default and matches Netflix's recommended value for H.264. Alternative: fixed bitrate (`-b:v 2M`), produces predictable file sizes but variable quality. CRF was chosen because it produces consistent visual quality regardless of content complexity.

## Why `-preset fast`?
The `preset` controls encoding speed vs compression efficiency:
- `slow`: better compression, 3x longer encoding time
- `fast`: acceptable compression, suitable for server workloads
- `ultrafast`: poor compression, only for real-time use

`fast` was chosen as the balance point for server-side transcoding.

## Why `-y`?
Overwrites output file without prompting. Required for idempotency, if the same job is processed twice, FFmpeg does not block on "file already exists" confirmation.

## Observed Performance (test video: 3.83s, 372KB, 1920x1080)
- Single worker FFmpeg duration: ~0.65s (speed: 5.4x realtime)
- Concurrent (2 workers): ~1.2s each (speed: 3.1x realtime)
- Output: 158KB, 343kbps, 1280x720

---

# 11: Infrastructure as Code : Terraform + LocalStack

## Why Terraform?
All AWS resources (SQS queues, S3 buckets, DynamoDB table) are defined as code. Benefits:
- Reproducible environment : `terraform apply` recreates everything from scratch
- Version controlled infrastructure : changes are reviewed and tracked
- Environment parity : same Terraform code targets LocalStack locally and real AWS in production (only provider endpoint changes)

## Why LocalStack?
LocalStack emulates AWS services locally, SQS, S3, DynamoDB, without an AWS account or network calls. Benefits:
- Zero cost
- Fast feedback loop (no network latency)
- No credentials or billing risk during development

Trade-off: LocalStack does not perfectly replicate all AWS behaviors. Edge cases (IAM permission errors, S3 ACL nuances, SQS FIFO ordering) may behave differently. For this project, the emulated behavior was sufficient.

## Stateless LocalStack
LocalStack runs in-memory. Container restart wipes all state. `terraform apply` is required on every LocalStack restart. This is intentional the infrastructure definition is the source of truth, not the running state.

## Provider Configuration
```hcl
s3_use_path_style = true
```
Required for LocalStack. AWS SDK defaults to virtual-hosted-style S3 URLs (`bucket.s3.amazonaws.com`). LocalStack requires path-style (`localhost:4566/bucket`). This flag is the only difference between local and production provider configuration.

---

# 12: Observability

## Metrics Endpoints
- `/metrics/json` : human-readable JSON snapshot for debugging
- `/metrics` : Prometheus exposition format for scraping
- `/debug/pprof/` : Go pprof profiling endpoints

## Metrics Collected
| Metric | Type | Description |
|--------|------|-------------|
| `transcodex_jobs_received` | Counter | Total jobs polled from SQS |
| `transcodex_jobs_completed` | Counter | Total jobs successfully processed |
| `transcodex_jobs_failed` | Counter | Total jobs that failed (any stage) |
| `transcodex_jobs_retried` | Counter | Total retry attempts |

## Why Atomic Counters?
`sync/atomic` provides lock-free counter increments. No mutex required, metrics updates from multiple goroutines are safe without contention.

## Why pprof?
Go's built-in profiling enables CPU profiling, goroutine dumps, heap analysis, and mutex contention analysis, all via HTTP. No external tooling required. Useful for diagnosing worker pool behavior under load.

## Trade-off
Current implementation uses snapshot-based counters. No histograms, no percentile tracking. For production, the Prometheus Go client library with histogram buckets would provide p50/p99 latency tracking. This is an intentional simplification; the focus is on the mechanics, not the instrumentation stack.

---

# 13: Known Limitations

- **In-memory LocalStack** : no persistence across restarts
- **No drain timeout** : graceful shutdown waits indefinitely; Kubernetes incompatible
- **No DLQ replay** : dead-lettered jobs require manual requeue
- **No multipart retry** : failed parts abort the entire upload; no per-part retry
- **Fixed FFmpeg parameters** : no per-job quality/resolution configuration
- **No authentication** : upload endpoint is unauthenticated

These are intentional omissions to keep focus on delivery semantics and worker lifecycle.

---

# 14: Future Improvements

- Drain timeout for Kubernetes-compatible shutdown
- Per-part retry in multipart upload
- DLQ replay tooling (CLI or HTTP endpoint)
- Prometheus histogram buckets for latency percentiles
- Job priority via SQS message attributes
- S3 lifecycle rules for raw video cleanup
- Authentication on upload endpoint (JWT or API key)
- Per-job FFmpeg parameter configuration

---

# Systems Learning Outcomes

This project demonstrates understanding of:

- Queue semantics and delivery guarantees (at-least-once, visibility timeout)
- Worker pool design and concurrency control
- Failure isolation (DLQ, partial failure cleanup)
- Graceful shutdown with in-flight job management
- Idempotent job processing
- Infrastructure as code with Terraform
- Performance profiling and bottleneck identification
- Trade-off driven design decisions

The implementation focuses on clarity of semantics over feature completeness.
