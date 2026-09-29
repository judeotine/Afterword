package workerlib

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/judeotine/afterword/services/api/internal/db/sqlcgen"
	"github.com/judeotine/afterword/services/api/internal/jobs"
	"github.com/judeotine/afterword/services/api/internal/meetings"
	"github.com/judeotine/afterword/services/api/internal/storage"
)

type SummariseDeps struct {
	Queries          *sqlcgen.Queries
	Storage          storage.Client
	TranscriptBucket string
	SummariseBin     string
	TemplatePath     string
	Provider         string
	OllamaURL        string
	Model            string
	WorkDir          string
}

func NewSummariseHandler(deps SummariseDeps) jobs.Handler {
	return func(ctx context.Context, job *jobs.Job) error {
		var payload meetings.TranscodePayload
		if err := json.Unmarshal(job.Payload, &payload); err != nil {
			return fmt.Errorf("decode summarise payload: %w", err)
		}
		return runSummarise(ctx, deps, payload)
	}
}

func runSummarise(ctx context.Context, deps SummariseDeps, payload meetings.TranscodePayload) error {
	jobDir, err := os.MkdirTemp(deps.WorkDir, "summarise-")
	if err != nil {
		return fmt.Errorf("create work dir: %w", err)
	}
	defer os.RemoveAll(jobDir)

	transcriptPath := filepath.Join(jobDir, "transcripts.json")
	if err := download(ctx, deps.Storage, payload.Bucket, payload.Key, transcriptPath); err != nil {
		return err
	}

	summaryPath := filepath.Join(jobDir, "summary.md")
	if err := runSummariseCLI(ctx, deps, transcriptPath, summaryPath); err != nil {
		return err
	}

	markdown, err := os.ReadFile(summaryPath)
	if err != nil {
		return fmt.Errorf("read summary output: %w", err)
	}

	model := deps.Model
	if deps.Provider == "offline" {
		model = "offline"
	}
	if _, err := deps.Queries.CreateSummary(ctx, sqlcgen.CreateSummaryParams{
		MeetingID:   payload.MeetingID,
		TemplateID:  nil,
		Language:    "en",
		Markdown:    string(markdown),
		Model:       model,
		ActionItems: []byte("[]"),
		WorkspaceID: payload.WorkspaceID,
	}); err != nil {
		return fmt.Errorf("store summary: %w", err)
	}
	return nil
}

func runSummariseCLI(ctx context.Context, deps SummariseDeps, transcriptPath, summaryPath string) error {
	args := []string{
		"--transcript", transcriptPath,
		"--template", deps.TemplatePath,
		"--out", summaryPath,
		"--provider", deps.Provider,
	}
	if deps.Provider == "ollama" {
		args = append(args, "--ollama-url", deps.OllamaURL, "--model", deps.Model)
	}
	cmd := exec.CommandContext(ctx, deps.SummariseBin, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("summarise CLI failed: %w: %s", err, string(output))
	}
	return nil
}
