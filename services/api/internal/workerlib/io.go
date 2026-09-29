package workerlib

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/judeotine/afterword/services/api/internal/storage"
)

const transferTTL = 30 * time.Minute

func download(ctx context.Context, client storage.Client, bucket, key, destPath string) error {
	signed, err := client.PresignDownload(ctx, bucket, key, transferTTL)
	if err != nil {
		return fmt.Errorf("presign download %s/%s: %w", bucket, key, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, signed.URL, nil)
	if err != nil {
		return fmt.Errorf("build download request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download object: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download object %s/%s: unexpected status %d", bucket, key, resp.StatusCode)
	}
	file, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("create local file: %w", err)
	}
	defer file.Close()
	if _, err := io.Copy(file, resp.Body); err != nil {
		return fmt.Errorf("write downloaded object: %w", err)
	}
	return nil
}

func upload(ctx context.Context, client storage.Client, bucket, key, srcPath string) error {
	info, err := os.Stat(srcPath)
	if err != nil {
		return fmt.Errorf("stat upload source: %w", err)
	}
	signed, err := client.PresignUpload(ctx, storage.UploadRequest{
		Bucket:      bucket,
		Key:         key,
		TTL:         transferTTL,
		ContentType: "application/octet-stream",
		SizeBytes:   info.Size(),
	})
	if err != nil {
		return fmt.Errorf("presign upload %s/%s: %w", bucket, key, err)
	}
	file, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("open upload source: %w", err)
	}
	defer file.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, signed.URL, file)
	if err != nil {
		return fmt.Errorf("build upload request: %w", err)
	}
	req.ContentLength = info.Size()
	req.Header.Set("Content-Type", "application/octet-stream")
	for name, value := range signed.Headers {
		req.Header.Set(name, value)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("upload object: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("upload object %s/%s: unexpected status %d", bucket, key, resp.StatusCode)
	}
	return nil
}

func toWav(ctx context.Context, ffmpegBin, srcPath, destPath string) error {
	args := []string{"-y", "-i", srcPath, "-ac", "1", "-ar", "16000", "-c:a", "pcm_s16le", destPath}
	return runFFmpeg(ctx, ffmpegBin, args)
}

func toOpus(ctx context.Context, ffmpegBin, srcPath, destPath string) error {
	args := []string{"-y", "-i", srcPath, "-c:a", "libopus", "-b:a", "24k", destPath}
	return runFFmpeg(ctx, ffmpegBin, args)
}

func runFFmpeg(ctx context.Context, ffmpegBin string, args []string) error {
	cmd := exec.CommandContext(ctx, ffmpegBin, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg failed: %w: %s", err, string(output))
	}
	return nil
}
