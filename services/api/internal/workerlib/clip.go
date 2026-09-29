package workerlib

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/judeotine/afterword/services/api/internal/db/sqlcgen"
	"github.com/judeotine/afterword/services/api/internal/jobs"
	"github.com/judeotine/afterword/services/api/internal/meetings"
	"github.com/judeotine/afterword/services/api/internal/storage"
)

type ClipDeps struct {
	Queries     *sqlcgen.Queries
	Storage     storage.Client
	AudioBucket string
	ClipBucket  string
	FFmpegBin   string
	WorkDir     string
}

func NewClipHandler(deps ClipDeps) jobs.Handler {
	return func(ctx context.Context, job *jobs.Job) error {
		var payload meetings.ClipPayload
		if err := json.Unmarshal(job.Payload, &payload); err != nil {
			return fmt.Errorf("decode clip payload: %w", err)
		}
		return runClip(ctx, deps, payload)
	}
}

func runClip(ctx context.Context, deps ClipDeps, payload meetings.ClipPayload) error {
	jobDir, err := os.MkdirTemp(deps.WorkDir, "clip-")
	if err != nil {
		return fmt.Errorf("create work dir: %w", err)
	}
	defer os.RemoveAll(jobDir)

	sourcePath := filepath.Join(jobDir, "source")
	if err := download(ctx, deps.Storage, deps.AudioBucket, payload.SourceKey, sourcePath); err != nil {
		return err
	}

	clipPath := filepath.Join(jobDir, "clip.opus")
	if err := cutOpus(ctx, deps.FFmpegBin, sourcePath, clipPath, payload.StartS, payload.EndS); err != nil {
		return err
	}

	if err := upload(ctx, deps.Storage, deps.ClipBucket, payload.TargetKey, clipPath); err != nil {
		return err
	}

	object := payload.TargetKey
	if _, err := deps.Queries.UpdateClip(ctx, sqlcgen.UpdateClipParams{
		ID:          payload.ClipID,
		WorkspaceID: payload.WorkspaceID,
		Object:      &object,
	}); err != nil {
		return fmt.Errorf("update clip object: %w", err)
	}
	return nil
}

func cutOpus(ctx context.Context, ffmpegBin, srcPath, destPath string, startS, endS float64) error {
	duration := endS - startS
	if duration <= 0 {
		return fmt.Errorf("clip duration must be positive")
	}
	args := []string{
		"-y",
		"-ss", strconv.FormatFloat(startS, 'f', 3, 64),
		"-t", strconv.FormatFloat(duration, 'f', 3, 64),
		"-i", srcPath,
		"-c:a", "libopus",
		"-b:a", "24k",
		destPath,
	}
	cmd := exec.CommandContext(ctx, ffmpegBin, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg clip cut failed: %w: %s", err, string(output))
	}
	return nil
}
