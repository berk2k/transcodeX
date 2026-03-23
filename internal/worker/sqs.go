package worker

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

type SQSPoller struct {
	Client   *sqs.Client
	QueueURL string
	Pool     *Pool
	Logger   *slog.Logger
}

type sqsMessage struct {
	JobID  string `json:"jobId"`
	S3Key  string `json:"s3Key"`
	Bucket string `json:"bucket"`
}

func (s *SQSPoller) Start(ctx context.Context) {
	s.Logger.Info("sqs poller starting", "queueURL", s.QueueURL)

	for {
		select {
		case <-ctx.Done():
			s.Logger.Info("sqs poller stopping")
			return
		default:
		}

		result, err := s.Client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl:            aws.String(s.QueueURL),
			MaxNumberOfMessages: 1,
			WaitTimeSeconds:     10, // long polling
		})
		if err != nil {
			s.Logger.Error("failed to receive message", "error", err)
			continue
		}

		for _, msg := range result.Messages {
			var payload sqsMessage
			if err := json.Unmarshal([]byte(*msg.Body), &payload); err != nil {
				s.Logger.Error("failed to parse message", "error", err)
				continue
			}

			job := Job{
				MessageID:     *msg.MessageId,
				ReceiptHandle: *msg.ReceiptHandle,
				JobID:         payload.JobID,
				S3Key:         payload.S3Key,
				Bucket:        payload.Bucket,
			}

			s.Logger.Info("job received", "jobID", job.JobID)
			s.Pool.Submit(job)
		}
	}
}
