package auth

import (
	"context"
	"time"

	"github.com/judeotine/afterword/services/api/internal/jobs"
)

const (
	TaskRetentionSweep            = "auth-retention-sweep"
	LockKeyRetentionSweep         = int64(0x6166776f74707274)
	DefaultRetentionSweepInterval = time.Hour
	DefaultOTPRetention           = 24 * time.Hour
)

func MaintenanceTasks(store *Store) []jobs.ScheduledTask {
	if store == nil {
		return nil
	}
	return []jobs.ScheduledTask{{
		Name:     TaskRetentionSweep,
		Interval: DefaultRetentionSweepInterval,
		LockKey:  LockKeyRetentionSweep,
		Run: func(ctx context.Context) error {
			now := time.Now().UTC()
			if _, err := store.DeleteExpiredOTPs(ctx, now.Add(-DefaultOTPRetention)); err != nil {
				return err
			}
			if _, err := store.DeleteOTPVerifyAttemptsBefore(ctx, now.Add(-DefaultOTPRetention)); err != nil {
				return err
			}
			if _, err := store.DeleteExpiredRefreshTokens(ctx, now); err != nil {
				return err
			}
			if _, err := store.DeleteExpiredOAuthStates(ctx, now); err != nil {
				return err
			}
			return nil
		},
	}}
}
