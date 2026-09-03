package meetings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/judeotine/afterword/services/api/internal/jobs"
	"github.com/judeotine/afterword/services/api/internal/storage"
)

func PurgeObjects(ctx context.Context, client storage.Client, payload PurgePayload) error {
	var failures []error
	for _, object := range payload.Objects {
		if err := client.Delete(ctx, object.Bucket, object.Key); err != nil {
			failures = append(failures, fmt.Errorf("purge %s/%s: %w", object.Bucket, object.Key, err))
		}
	}
	return errors.Join(failures...)
}

func NewPurgeHandler(client storage.Client) jobs.Handler {
	return func(ctx context.Context, job *jobs.Job) error {
		var payload PurgePayload
		if err := json.Unmarshal(job.Payload, &payload); err != nil {
			return fmt.Errorf("decode purge payload: %w", err)
		}
		return PurgeObjects(ctx, client, payload)
	}
}
