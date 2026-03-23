package worker

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

func (p *Processor) downloadFromS3(ctx context.Context, bucket, key, jobID string) (string, error) {
	result, err := p.S3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return "", fmt.Errorf("s3 get object: %w", err)
	}
	defer result.Body.Close()

	tmpDir := filepath.Join("tmp", jobID)
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return "", fmt.Errorf("create tmp dir: %w", err)
	}

	localPath := filepath.Join(tmpDir, "input"+filepath.Ext(key))
	f, err := os.Create(localPath)
	if err != nil {
		return "", fmt.Errorf("create local file: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(f, result.Body); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}

	return localPath, nil
}

func (p *Processor) uploadToS3(ctx context.Context, localPath, outputKey string) error {
	f, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	_, err = p.S3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(p.ProcessedBucket),
		Key:    aws.String(outputKey),
		Body:   f,
	})
	return err
}

func (p *Processor) deleteSQSMessage(ctx context.Context, receiptHandle string) {
	p.SQSClient.DeleteMessage(ctx, &sqs.DeleteMessageInput{
		QueueUrl:      aws.String(p.QueueURL),
		ReceiptHandle: aws.String(receiptHandle),
	})
}
