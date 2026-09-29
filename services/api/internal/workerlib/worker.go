package workerlib

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/rs/zerolog"

	"github.com/judeotine/afterword/services/api/internal/db/sqlcgen"
	"github.com/judeotine/afterword/services/api/internal/jobs"
	"github.com/judeotine/afterword/services/api/internal/meetings"
	"github.com/judeotine/afterword/services/api/internal/storage"
)

type Config struct {
	AudioBucket      string
	TranscriptBucket string
	TranscribeBin    string
	FFmpegBin        string
	Engine           string
	Model            string
	ModelsDir        string
	WorkDir          string
}

type Deps struct {
	Queries *sqlcgen.Queries
	Storage storage.Client
	Config  Config
	Logger  zerolog.Logger
}

func NewTranscribeHandler(deps Deps) jobs.Handler {
	return func(ctx context.Context, job *jobs.Job) error {
		var payload meetings.TranscodePayload
		if err := json.Unmarshal(job.Payload, &payload); err != nil {
			return fmt.Errorf("decode transcribe payload: %w", err)
		}
		return runTranscribe(ctx, deps, payload)
	}
}

func runTranscribe(ctx context.Context, deps Deps, payload meetings.TranscodePayload) error {
	logger := deps.Logger.With().
		Str("meeting_id", payload.MeetingID.String()).
		Str("workspace_id", payload.WorkspaceID.String()).
		Logger()

	jobDir, err := os.MkdirTemp(deps.Config.WorkDir, "transcribe-")
	if err != nil {
		return fmt.Errorf("create work dir: %w", err)
	}
	defer os.RemoveAll(jobDir)

	audioPath := filepath.Join(jobDir, "audio.input")
	if err := download(ctx, deps.Storage, payload.Bucket, payload.Key, audioPath); err != nil {
		return err
	}

	wavPath := filepath.Join(jobDir, "audio.wav")
	if err := toWav(ctx, deps.Config.FFmpegBin, audioPath, wavPath); err != nil {
		return err
	}

	outDir := filepath.Join(jobDir, "out")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("create out dir: %w", err)
	}
	if err := runCLI(ctx, deps.Config, wavPath, outDir); err != nil {
		return err
	}

	transcriptFile := filepath.Join(outDir, "transcripts.json")
	segments, err := parseTranscripts(transcriptFile)
	if err != nil {
		return err
	}

	transcriptKey := storage.TranscriptKey(payload.WorkspaceID, payload.MeetingID)
	if err := upload(ctx, deps.Storage, deps.Config.TranscriptBucket, transcriptKey, transcriptFile); err != nil {
		return err
	}

	if err := writeSegments(ctx, deps.Queries, payload, segments); err != nil {
		return err
	}

	opusKey, opusErr := transcodeAndStoreOpus(ctx, deps, payload, audioPath, jobDir)
	if opusErr != nil {
		logger.Warn().Err(opusErr).Msg("opus transcode failed, keeping original audio object")
		opusKey = ""
	}

	if err := finalizeMeeting(ctx, deps.Queries, payload, transcriptKey, opusKey, segments); err != nil {
		return err
	}

	logger.Info().Int("segments", len(segments)).Msg("transcribe job complete")
	return nil
}

func transcodeAndStoreOpus(ctx context.Context, deps Deps, payload meetings.TranscodePayload, audioPath, jobDir string) (string, error) {
	opusPath := filepath.Join(jobDir, "audio.opus")
	if err := toOpus(ctx, deps.Config.FFmpegBin, audioPath, opusPath); err != nil {
		return "", err
	}
	opusKey := storage.AudioKey(payload.WorkspaceID, payload.MeetingID, "opus")
	if err := upload(ctx, deps.Storage, deps.Config.AudioBucket, opusKey, opusPath); err != nil {
		return "", err
	}
	if opusKey != payload.Key {
		if err := deps.Storage.Delete(ctx, payload.Bucket, payload.Key); err != nil {
			return opusKey, fmt.Errorf("delete original audio object: %w", err)
		}
	}
	return opusKey, nil
}

func writeSegments(ctx context.Context, queries *sqlcgen.Queries, payload meetings.TranscodePayload, segments []Segment) error {
	if _, err := queries.DeleteSegmentsForMeeting(ctx, sqlcgen.DeleteSegmentsForMeetingParams{
		MeetingID:   payload.MeetingID,
		WorkspaceID: payload.WorkspaceID,
	}); err != nil {
		return fmt.Errorf("clear existing segments: %w", err)
	}
	for _, segment := range segments {
		if _, err := queries.CreateTranscriptSegment(ctx, sqlcgen.CreateTranscriptSegmentParams{
			MeetingID:   payload.MeetingID,
			Seq:         segment.Seq,
			Speaker:     nil,
			StartS:      segment.StartS,
			EndS:        segment.EndS,
			Text:        segment.Text,
			Embedding:   nil,
			WorkspaceID: payload.WorkspaceID,
		}); err != nil {
			return fmt.Errorf("insert segment %d: %w", segment.Seq, err)
		}
	}
	return nil
}

func finalizeMeeting(ctx context.Context, queries *sqlcgen.Queries, payload meetings.TranscodePayload, transcriptKey, opusKey string, segments []Segment) error {
	duration := int32(transcriptDurationSeconds(segments))
	status := meetings.StatusReady
	params := sqlcgen.UpdateMeetingParams{
		ID:               payload.MeetingID,
		WorkspaceID:      payload.WorkspaceID,
		TranscriptObject: &transcriptKey,
		Status:           &status,
		DurationS:        &duration,
	}
	if opusKey != "" {
		params.AudioObject = &opusKey
	}
	if _, err := queries.UpdateMeeting(ctx, params); err != nil {
		return fmt.Errorf("update meeting after transcription: %w", err)
	}
	return nil
}

func runCLI(ctx context.Context, cfg Config, wavPath, outDir string) error {
	args := []string{
		"--input", wavPath,
		"--out", outDir,
		"--engine", cfg.Engine,
		"--model", cfg.Model,
		"--models-dir", cfg.ModelsDir,
	}
	cmd := exec.CommandContext(ctx, cfg.TranscribeBin, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return classifyCLIError(err, output)
	}
	return nil
}

func classifyCLIError(err error, output []byte) error {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		switch exitErr.ExitCode() {
		case 2:
			return fmt.Errorf("transcribe CLI could not decode the audio: %s", string(output))
		case 3:
			return fmt.Errorf("transcribe CLI is missing its model: %s", string(output))
		}
	}
	return fmt.Errorf("transcribe CLI failed: %w: %s", err, string(output))
}
