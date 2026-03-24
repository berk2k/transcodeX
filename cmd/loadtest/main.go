package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/joho/godotenv"
)

type UploadResponse struct {
	JobID     string `json:"jobId"`
	Status    string `json:"status"`
	CreatedAt string `json:"createdAt"`
}

type JobStatus struct {
	JobID     string `json:"jobId"`
	Status    string `json:"status"`
	UpdatedAt string `json:"updatedAt"`
}

type Result struct {
	JobID          string
	UploadDuration time.Duration
	TotalDuration  time.Duration
	Success        bool
}

func uploadVideo(uploadURL, filePath string) (UploadResponse, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return UploadResponse{}, err
	}
	defer f.Close()

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("video", filePath)
	if err != nil {
		return UploadResponse{}, err
	}
	io.Copy(part, f)
	writer.Close()

	resp, err := http.Post(uploadURL, writer.FormDataContentType(), &buf)
	if err != nil {
		return UploadResponse{}, err
	}
	defer resp.Body.Close()

	var uploadResp UploadResponse
	json.NewDecoder(resp.Body).Decode(&uploadResp)
	return uploadResp, nil
}

func waitForCompletion(jobsURL, jobID string, timeout time.Duration) (bool, time.Duration) {
	start := time.Now()
	for time.Since(start) < timeout {
		resp, err := http.Get(fmt.Sprintf("%s?id=%s", jobsURL, jobID))
		if err != nil {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		var status JobStatus
		json.NewDecoder(resp.Body).Decode(&status)
		resp.Body.Close()

		if status.Status == "completed" || status.Status == "failed" {
			return status.Status == "completed", time.Since(start)
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false, timeout
}

func runLoadTest(uploadURL, jobsURL, filePath string, jobCount int) {
	fmt.Printf("\n=== Load Test: %d concurrent jobs ===\n", jobCount)

	results := make([]Result, jobCount)
	var wg sync.WaitGroup
	start := time.Now()

	for i := 0; i < jobCount; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			uploadStart := time.Now()
			uploadResp, err := uploadVideo(uploadURL, filePath)
			if err != nil {
				results[idx] = Result{Success: false}
				return
			}
			uploadDuration := time.Since(uploadStart)

			success, waitDuration := waitForCompletion(jobsURL, uploadResp.JobID, 60*time.Second)
			results[idx] = Result{
				JobID:          uploadResp.JobID,
				UploadDuration: uploadDuration,
				TotalDuration:  uploadDuration + waitDuration,
				Success:        success,
			}
		}(i)
	}

	wg.Wait()
	totalDuration := time.Since(start)

	var successful, failed int
	var totalLatency time.Duration
	var maxLatency time.Duration
	var minLatency = time.Hour

	for _, r := range results {
		if r.Success {
			successful++
			totalLatency += r.TotalDuration
			if r.TotalDuration > maxLatency {
				maxLatency = r.TotalDuration
			}
			if r.TotalDuration < minLatency {
				minLatency = r.TotalDuration
			}
		} else {
			failed++
		}
	}

	avgLatency := time.Duration(0)
	if successful > 0 {
		avgLatency = totalLatency / time.Duration(successful)
	}

	throughput := float64(successful) / totalDuration.Seconds()

	fmt.Printf("Jobs:         %d total, %d completed, %d failed\n", jobCount, successful, failed)
	fmt.Printf("Total time:   %v\n", totalDuration.Round(time.Millisecond))
	fmt.Printf("Throughput:   %.2f jobs/sec\n", throughput)
	fmt.Printf("Avg e2e:      %v\n", avgLatency.Round(time.Millisecond))
	fmt.Printf("Min e2e:      %v\n", minLatency.Round(time.Millisecond))
	fmt.Printf("Max e2e:      %v\n", maxLatency.Round(time.Millisecond))
}

func main() {
	godotenv.Load()

	uploadURL := "http://localhost:8080/upload"
	jobsURL := "http://localhost:8080/jobs"
	filePath := "testdata/upload_test.mov"

	for _, count := range []int{1, 2, 4, 8} {
		runLoadTest(uploadURL, jobsURL, filePath, count)
		time.Sleep(3 * time.Second)
	}
}
