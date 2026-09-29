package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/rs/zerolog"

	"github.com/judeotine/afterword/services/api/internal/db"
	"github.com/judeotine/afterword/services/api/internal/db/sqlcgen"
	"github.com/judeotine/afterword/services/api/internal/jobs"
	"github.com/judeotine/afterword/services/api/internal/meetings"
	"github.com/judeotine/afterword/services/api/internal/storage"
	"github.com/judeotine/afterword/services/api/internal/workerlib"
)

func main() {
	logger := zerolog.New(os.Stderr).With().Timestamp().Str("service", "transcribe-worker").Logger()
	if err := run(logger); err != nil {
		logger.Fatal().Err(err).Msg("transcribe worker exited")
	}
}

func run(logger zerolog.Logger) error {
	level := strings.TrimSpace(os.Getenv("LOG_LEVEL"))
	if parsed, err := zerolog.ParseLevel(level); err == nil && level != "" {
		zerolog.SetGlobalLevel(parsed)
	}

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return errors.New("DATABASE_URL is required")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	connectCtx, cancelConnect := context.WithTimeout(ctx, 10*time.Second)
	defer cancelConnect()

	pool, err := db.Connect(connectCtx, db.Options{
		URL:             databaseURL,
		MaxConns:        envInt32("DATABASE_MAX_CONNS", 4),
		MinConns:        1,
		MaxConnLifetime: time.Hour,
		MaxConnIdleTime: 30 * time.Minute,
		ConnectTimeout:  5 * time.Second,
	}, logger)
	if err != nil {
		return err
	}
	defer pool.Close()

	client, err := storage.NewS3Client(storage.Options{
		Endpoint:     os.Getenv("S3_ENDPOINT"),
		Region:       envString("S3_REGION", "us-east-1"),
		AccessKey:    os.Getenv("S3_ACCESS_KEY"),
		SecretKey:    os.Getenv("S3_SECRET_KEY"),
		UsePathStyle: envBool("S3_USE_PATH_STYLE", true),
	})
	if err != nil {
		return err
	}

	buckets := storage.Buckets{
		Audio:       os.Getenv("AUDIO_BUCKET"),
		Transcripts: os.Getenv("TRANSCRIPT_BUCKET"),
		Clips:       os.Getenv("CLIP_BUCKET"),
	}.WithDefaults()

	deps := workerlib.Deps{
		Queries: sqlcgen.New(pool.Pool()),
		Storage: client,
		Config: workerlib.Config{
			AudioBucket:      buckets.Audio,
			TranscriptBucket: buckets.Transcripts,
			TranscribeBin:    envString("TRANSCRIBE_BIN", "afterword-transcribe"),
			FFmpegBin:        envString("FFMPEG_BIN", "ffmpeg"),
			Engine:           envString("TRANSCRIBE_ENGINE", "whisper"),
			Model:            envString("TRANSCRIBE_MODEL", "base"),
			ModelsDir:        envString("MODELS_DIR", "/models"),
			WorkDir:          envString("WORK_DIR", "/work"),
		},
		Logger: logger,
	}

	if err := os.MkdirAll(deps.Config.WorkDir, 0o755); err != nil {
		return err
	}

	runner := jobs.NewRunner(jobs.NewQueue(pool.Pool()), jobs.WithLogger(logger))
	concurrency := int(envInt32("WORKER_CONCURRENCY", 1))
	if concurrency < 1 {
		concurrency = 1
	}
	if err := runner.Register(meetings.KindTranscribe, concurrency, workerlib.NewTranscribeHandler(deps)); err != nil {
		return err
	}
	clipDeps := workerlib.ClipDeps{
		Queries:     deps.Queries,
		Storage:     client,
		AudioBucket: buckets.Audio,
		ClipBucket:  buckets.Clips,
		FFmpegBin:   deps.Config.FFmpegBin,
		WorkDir:     deps.Config.WorkDir,
	}
	if err := runner.Register(meetings.KindClip, concurrency, workerlib.NewClipHandler(clipDeps)); err != nil {
		return err
	}

	logger.Info().Int("concurrency", concurrency).Msg("transcribe worker started")
	if err := runner.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

func envString(key, def string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return def
	}
	return value
}

func envBool(key string, def bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return def
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return def
	}
	return parsed
}

func envInt32(key string, def int32) int32 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return def
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return def
	}
	return int32(parsed)
}
