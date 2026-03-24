package worker

import (
	"context"
	"log/slog"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/berk2k/transcodeX/internal/ffmpeg"
	"github.com/berk2k/transcodeX/internal/observability"
)

type Processor struct {
	S3Client        *s3.Client
	SQSClient       *sqs.Client
	DynamoDBClient  *dynamodb.Client
	RawBucket       string
	ProcessedBucket string
	QueueURL        string
	TableName       string
	Logger          *slog.Logger
}

func (p *Processor) Process(ctx context.Context, job Job) {
	p.Logger.Info("processing job", "jobID", job.JobID, "s3Key", job.S3Key)
	observability.Global.JobsReceived.Add(1)

	// 1. Update status to processing
	if err := p.updateJobStatus(ctx, job.JobID, "processing"); err != nil {
		p.Logger.Error("failed to update status", "jobID", job.JobID, "error", err)
		observability.Global.JobsFailed.Add(1)
		return
	}

	// Start visibility timeout extender
	extCtx, cancelExt := context.WithCancel(ctx)
	defer cancelExt()

	extender := &VisibilityExtender{
		Client:        p.SQSClient,
		QueueURL:      p.QueueURL,
		ReceiptHandle: job.ReceiptHandle,
		Logger:        p.Logger,
	}
	extender.Start(extCtx)

	// 2. Download from S3
	localPath, err := p.downloadFromS3(ctx, job.Bucket, job.S3Key, job.JobID)
	if err != nil {
		p.Logger.Error("failed to download", "jobID", job.JobID, "error", err)
		p.updateJobStatus(ctx, job.JobID, "failed")
		return
	}

	// 3. Transcode with FFmpeg
	result, err := ffmpeg.Transcode(ctx, localPath, job.JobID)
	if err != nil {
		p.Logger.Error("failed to transcode", "jobID", job.JobID, "error", err)
		p.updateJobStatus(ctx, job.JobID, "failed")
		ffmpeg.Cleanup(localPath)
		return
	}

	// 4. Upload processed video to S3
	outputKey := "processed/" + job.JobID + "/output.mp4"
	if err := p.uploadToS3(ctx, result.OutputPath, outputKey); err != nil {
		p.Logger.Error("failed to upload processed", "jobID", job.JobID, "error", err)
		p.updateJobStatus(ctx, job.JobID, "failed")
		ffmpeg.Cleanup(localPath, result.OutputPath)
		return
	}

	// 5. Update status to completed
	p.updateJobStatus(ctx, job.JobID, "completed")

	// 6. Delete SQS message
	p.deleteSQSMessage(ctx, job.ReceiptHandle)

	// 7. Cleanup temp files
	ffmpeg.Cleanup(localPath, result.OutputPath)

	p.Logger.Info("job completed", "jobID", job.JobID, "outputKey", outputKey)
	observability.Global.JobsCompleted.Add(1)
}
