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

type Result struct {
	JobID    string
	Duration time.Duration
	Success  bool
}

func uploadVideo(uploadURL, filePath string) (Result, error) {
	start := time.Now()

	f, err := os.Open(filePath)
	if err != nil {
		return Result{}, err
	}
	defer f.Close()

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("video", filePath)
	if err != nil {
		return Result{}, err
	}
	io.Copy(part, f)
	writer.Close()

	resp, err := http.Post(uploadURL, writer.FormDataContentType(), &buf)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()

	var uploadResp UploadResponse
	json.NewDecoder(resp.Body).Decode(&uploadResp)

	return Result{
		JobID:    uploadResp.JobID,
		Duration: time.Since(start),
		Success:  resp.StatusCode == http.StatusAccepted,
	}, nil
}

func runLoadTest(uploadURL, filePath string, jobCount int) {
	fmt.Printf("\n=== Load Test: %d jobs ===\n", jobCount)

	results := make([]Result, jobCount)
	var wg sync.WaitGroup
	start := time.Now()

	for i := 0; i < jobCount; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			result, err := uploadVideo(uploadURL, filePath)
			if err != nil {
				results[idx] = Result{Success: false}
				return
			}
			results[idx] = result
		}(i)
	}

	wg.Wait()
	totalDuration := time.Since(start)

	// Calculate stats
	var successful, failed int
	var totalLatency time.Duration
	var maxLatency time.Duration

	for _, r := range results {
		if r.Success {
			successful++
			totalLatency += r.Duration
			if r.Duration > maxLatency {
				maxLatency = r.Duration
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

	fmt.Printf("Jobs:        %d total, %d successful, %d failed\n", jobCount, successful, failed)
	fmt.Printf("Duration:    %v\n", totalDuration.Round(time.Millisecond))
	fmt.Printf("Throughput:  %.2f jobs/sec (upload)\n", throughput)
	fmt.Printf("Avg latency: %v\n", avgLatency.Round(time.Millisecond))
	fmt.Printf("Max latency: %v\n", maxLatency.Round(time.Millisecond))
}

func main() {
	godotenv.Load()

	uploadURL := "http://localhost:8080/upload"
	filePath := "testdata/upload_test.mov"

	if len(os.Args) > 1 {
		filePath = os.Args[1]
	}

	// Test with different job counts
	for _, count := range []int{5, 10, 20} {
		runLoadTest(uploadURL, filePath, count)
		time.Sleep(2 * time.Second) // wait between tests
	}
}
