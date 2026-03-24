package upload

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/google/uuid"
)

type Handler struct {
	S3Client       *s3.Client
	SQSClient      *sqs.Client
	DynamoDBClient *dynamodb.Client
	RawBucket      string
	QueueURL       string
	TableName      string
}

type JobMessage struct {
	JobID  string `json:"jobId"`
	S3Key  string `json:"s3Key"`
	Bucket string `json:"bucket"`
}

func (h *Handler) UploadVideo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	err := r.ParseMultipartForm(500 << 20) // 500MB limit
	if err != nil {
		http.Error(w, "failed to parse form", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("video")
	if err != nil {
		http.Error(w, "video file required", http.StatusBadRequest)
		return
	}
	defer file.Close()

	jobID := uuid.New().String()
	s3Key := fmt.Sprintf("uploads/%s/%s", jobID, header.Filename)

	// Upload to S3
	size := r.ContentLength
	if err := h.uploadToS3(r.Context(), file, size, h.RawBucket, s3Key); err != nil {
		http.Error(w, "failed to upload to S3", http.StatusInternalServerError)
		return
	}

	// Write initial job status to DynamoDB
	err = createJob(context.TODO(), h.DynamoDBClient, h.TableName, jobID, s3Key)
	if err != nil {
		http.Error(w, "failed to create job", http.StatusInternalServerError)
		return
	}

	// Publish job message to SQS
	msg := JobMessage{
		JobID:  jobID,
		S3Key:  s3Key,
		Bucket: h.RawBucket,
	}
	msgBytes, _ := json.Marshal(msg)
	msgStr := string(msgBytes)

	_, err = h.SQSClient.SendMessage(context.TODO(), &sqs.SendMessageInput{
		QueueUrl:    &h.QueueURL,
		MessageBody: &msgStr,
	})
	if err != nil {
		http.Error(w, "failed to publish job", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{
		"jobId":     jobID,
		"status":    "queued",
		"createdAt": time.Now().UTC().Format(time.RFC3339),
	})
}
