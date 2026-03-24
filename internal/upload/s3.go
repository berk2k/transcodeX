package upload

import (
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

const multipartThreshold = 100 * 1024 * 1024 // 100MB

func (h *Handler) uploadToS3(ctx context.Context, file multipart.File, size int64, bucket, key string) error {
	if size > 0 && size < multipartThreshold {
		return h.uploadSingle(ctx, file, bucket, key)
	}
	return h.uploadMultipart(ctx, file, bucket, key)
}

func (h *Handler) uploadSingle(ctx context.Context, file multipart.File, bucket, key string) error {
	_, err := h.S3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   file,
	})
	return err
}

func (h *Handler) uploadMultipart(ctx context.Context, file multipart.File, bucket, key string) error {
	// 1. Initiate multipart upload
	createResp, err := h.S3Client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("create multipart upload: %w", err)
	}

	uploadID := createResp.UploadId
	partSize := int64(10 * 1024 * 1024) // 10MB per part
	var completedParts []types.CompletedPart
	partNumber := int32(1)

	// 2. Upload parts
	buf := make([]byte, partSize)
	for {
		n, err := io.ReadFull(file, buf)
		if n == 0 {
			break
		}
		if err != nil && err != io.ErrUnexpectedEOF {
			h.abortMultipart(ctx, bucket, key, uploadID)
			return fmt.Errorf("read part: %w", err)
		}

		partResp, err := h.S3Client.UploadPart(ctx, &s3.UploadPartInput{
			Bucket:     aws.String(bucket),
			Key:        aws.String(key),
			UploadId:   uploadID,
			PartNumber: aws.Int32(partNumber),
			Body:       &bytesReader{data: buf[:n]},
		})
		if err != nil {
			h.abortMultipart(ctx, bucket, key, uploadID)
			return fmt.Errorf("upload part %d: %w", partNumber, err)
		}

		completedParts = append(completedParts, types.CompletedPart{
			ETag:       partResp.ETag,
			PartNumber: aws.Int32(partNumber),
		})
		partNumber++

		if err == io.ErrUnexpectedEOF {
			break
		}
	}

	// 3. Complete multipart upload
	_, err = h.S3Client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket:   aws.String(bucket),
		Key:      aws.String(key),
		UploadId: uploadID,
		MultipartUpload: &types.CompletedMultipartUpload{
			Parts: completedParts,
		},
	})
	if err != nil {
		h.abortMultipart(ctx, bucket, key, uploadID)
		return fmt.Errorf("complete multipart upload: %w", err)
	}

	return nil
}

func (h *Handler) abortMultipart(ctx context.Context, bucket, key string, uploadID *string) {
	h.S3Client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
		Bucket:   aws.String(bucket),
		Key:      aws.String(key),
		UploadId: uploadID,
	})
}

type bytesReader struct {
	data []byte
	pos  int
}

func (r *bytesReader) Read(p []byte) (n int, err error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n = copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

// TempFile helper — multipart için gerekli
func saveToTemp(file multipart.File, jobID string) (string, int64, error) {
	tmpPath := fmt.Sprintf("tmp/%s/upload_tmp", jobID)
	if err := os.MkdirAll(fmt.Sprintf("tmp/%s", jobID), 0755); err != nil {
		return "", 0, err
	}

	f, err := os.Create(tmpPath)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()

	size, err := io.Copy(f, file)
	return tmpPath, size, err
}
