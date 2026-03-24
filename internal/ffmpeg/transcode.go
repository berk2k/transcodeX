package ffmpeg

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

type Result struct {
	OutputPath string
	InputPath  string
}

func Transcode(ctx context.Context, inputPath, jobID string) (*Result, error) {
	outputDir := filepath.Join("tmp", jobID)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, fmt.Errorf("create output dir: %w", err)
	}

	outputPath := filepath.Join(outputDir, "output.mp4")

	args := []string{
		"-i", inputPath,
		"-vf", "scale=1280:720",
		"-c:v", "libx264",
		"-crf", "23",
		"-preset", "fast",
		"-c:a", "aac",
		"-b:a", "128k",
		"-y",
		outputPath,
	}

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			Cleanup(inputPath, outputPath)
			return nil, fmt.Errorf("ffmpeg cancelled: %w", ctx.Err())
		}
		return nil, fmt.Errorf("ffmpeg failed: %w", err)
	}

	return &Result{
		OutputPath: outputPath,
		InputPath:  inputPath,
	}, nil
}

func Cleanup(paths ...string) {
	for _, p := range paths {
		os.RemoveAll(filepath.Dir(p))
	}
}
