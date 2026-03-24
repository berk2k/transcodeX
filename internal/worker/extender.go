package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

const (
	visibilityTimeout = 300
	extendInterval    = 30 * time.Second
)

type VisibilityExtender struct {
	Client        *sqs.Client
	QueueURL      string
	ReceiptHandle string
	Logger        *slog.Logger
}

func (e *VisibilityExtender) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(extendInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				_, err := e.Client.ChangeMessageVisibility(ctx, &sqs.ChangeMessageVisibilityInput{
					QueueUrl:          aws.String(e.QueueURL),
					ReceiptHandle:     aws.String(e.ReceiptHandle),
					VisibilityTimeout: visibilityTimeout,
				})
				if err != nil {
					e.Logger.Error("failed to extend visibility", "error", err)
					return
				}
				e.Logger.Info("visibility timeout extended", "receiptHandle", e.ReceiptHandle[:8])
			case <-ctx.Done():
				return
			}
		}
	}()
}
