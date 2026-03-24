package main

import (
	"log/slog"
	"net/http"
	"os"

	appconfig "github.com/berk2k/transcodeX/internal/config"
	"github.com/berk2k/transcodeX/internal/upload"
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

	handler := &upload.Handler{
		S3Client:       s3Client,
		SQSClient:      sqsClient,
		DynamoDBClient: dynamoClient,
		RawBucket:      os.Getenv("RAW_BUCKET"),
		QueueURL:       os.Getenv("SQS_QUEUE_URL"),
		TableName:      os.Getenv("DYNAMODB_TABLE"),
	}

	http.HandleFunc("/upload", handler.UploadVideo)
	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	http.HandleFunc("/jobs", handler.GetJob)

	port := os.Getenv("UPLOAD_PORT")
	if port == "" {
		port = "8080"
	}

	logger.Info("upload service starting", "port", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		logger.Error("server failed", "error", err)
		os.Exit(1)
	}
}
