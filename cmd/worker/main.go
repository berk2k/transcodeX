package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	appconfig "github.com/berk2k/transcodeX/internal/config"
	"github.com/berk2k/transcodeX/internal/observability"
	"github.com/berk2k/transcodeX/internal/worker"
	"github.com/joho/godotenv"
)

func main() {
	godotenv.Load()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg, err := appconfig.NewAWSConfig()
	if err != nil {
		logger.Error("failed to load AWS config", "error", err)
		os.Exit(1)
	}

	s3Client := appconfig.NewS3Client(cfg)
	sqsClient := appconfig.NewSQSClient(cfg)
	dynamoClient := appconfig.NewDynamoDBClient(cfg)

	processor := &worker.Processor{
		S3Client:        s3Client,
		SQSClient:       sqsClient,
		DynamoDBClient:  dynamoClient,
		RawBucket:       os.Getenv("RAW_BUCKET"),
		ProcessedBucket: os.Getenv("PROCESSED_BUCKET"),
		QueueURL:        os.Getenv("SQS_QUEUE_URL"),
		TableName:       os.Getenv("DYNAMODB_TABLE"),
		Logger:          logger,
	}

	workerCount := 2
	if v := os.Getenv("WORKER_COUNT"); v != "" {
		fmt.Sscanf(v, "%d", &workerCount)
	}
	pool := worker.NewPool(workerCount, processor, logger)

	ctx, cancel := context.WithCancel(context.Background())

	poller := &worker.SQSPoller{
		Client:   sqsClient,
		QueueURL: os.Getenv("SQS_QUEUE_URL"),
		Pool:     pool,
		Logger:   logger,
	}

	observability.StartServer("9090")
	logger.Info("metrics server starting", "port", "9090")
	// Start pool and poller
	pool.Start(ctx)
	go poller.Start(ctx)

	// Wait for shutdown signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutdown signal received, draining...")
	cancel()
	pool.Stop()
	logger.Info("worker stopped")
}
