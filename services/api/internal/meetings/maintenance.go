package meetings

import (
	"context"
	"time"

	"github.com/judeotine/afterword/services/api/internal/jobs"
)

const (
	TaskShareLinkRequestSweep        = "share-link-request-sweep"
	LockKeyShareLinkRequestSweep     = int64(0x6166777377656570)
	DefaultShareLinkSweepInterval    = time.Hour
	DefaultShareLinkRequestRetention = 24 * time.Hour
)

func MaintenanceTasks(service *Service) []jobs.ScheduledTask {
	if service == nil {
		return nil
	}
	return []jobs.ScheduledTask{{
		Name:     TaskShareLinkRequestSweep,
		Interval: DefaultShareLinkSweepInterval,
		LockKey:  LockKeyShareLinkRequestSweep,
		Run: func(ctx context.Context) error {
			_, err := service.SweepShareLinkRequests(ctx)
			return err
		},
	}}
}
