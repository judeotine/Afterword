# Afterword transcribe worker

Claims `transcribe` jobs from the shared Postgres job queue, pulls the meeting
audio from object storage, runs the `afterword-transcribe` CLI, writes the
transcript object and `transcript_segments` rows, transcodes the audio to Opus
24 kbps, and marks the meeting ready.

The worker binary lives inside the API Go module so it can use the same
`internal/` packages (`jobs`, `storage`, `meetings`, `db/sqlcgen`) the API uses.
Go forbids importing another module's `internal/` packages, so a separate
module is not possible.

- Command: `services/api/cmd/transcribe-worker`
- Job logic: `services/api/internal/workerlib`
- Image build: this `Dockerfile`, built with the repository root as context so
  it can compile the Rust CLI from the Cargo workspace

Credits are debited by the API when a job is enqueued, not by the worker, so
the worker never charges a workspace.

## Build

```bash
docker build -f services/transcribe-worker/Dockerfile -t afterword-transcribe-worker .
```

## Environment

| Variable | Default | Meaning |
| --- | --- | --- |
| `DATABASE_URL` | required | Postgres connection string |
| `S3_ENDPOINT` | required | Object storage endpoint |
| `S3_ACCESS_KEY` | required | Object storage access key |
| `S3_SECRET_KEY` | required | Object storage secret key |
| `S3_REGION` | `us-east-1` | Object storage region |
| `S3_USE_PATH_STYLE` | `true` | Path-style bucket addressing |
| `AUDIO_BUCKET` | `audio` | Bucket for audio objects |
| `TRANSCRIPT_BUCKET` | `transcripts` | Bucket for transcript objects |
| `TRANSCRIBE_ENGINE` | `whisper` | Engine passed to the CLI |
| `TRANSCRIBE_MODEL` | `base` | Model passed to the CLI |
| `MODELS_DIR` | `/models` | Model directory passed to the CLI |
| `WORK_DIR` | `/work` | Scratch directory for a job |
| `WORKER_CONCURRENCY` | `1` | Concurrent transcribe jobs |
