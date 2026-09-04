package main

import (
	"context"
	"testing"
	"time"

	"github.com/judeotine/afterword/services/api/internal/auth"
	"github.com/judeotine/afterword/services/api/internal/credits"
	"github.com/judeotine/afterword/services/api/internal/jobs"
	"github.com/judeotine/afterword/services/api/internal/meetings"
)

func startupTasks() []jobs.ScheduledTask {
	return []jobs.ScheduledTask{
		{Name: auth.TaskRetentionSweep, LockKey: auth.LockKeyRetentionSweep},
		{Name: meetings.TaskShareLinkRequestSweep, LockKey: meetings.LockKeyShareLinkRequestSweep},
		{Name: taskMonthlyCreditGrant, LockKey: credits.LockKeyMonthlyGrant},
		{Name: taskPendingPaymentReaper, LockKey: lockKeyPendingPaymentReaper},
	}
}

func TestEveryStartupTaskRegistersWithoutColliding(t *testing.T) {
	scheduler := jobs.NewScheduler(nil)
	for _, task := range startupTasks() {
		task.Interval = time.Hour
		task.Run = func(context.Context) error { return nil }
		if err := scheduler.Register(task); err != nil {
			t.Fatalf("register %q: %v", task.Name, err)
		}
	}
}

func TestEveryStartupTaskHasItsOwnLockKey(t *testing.T) {
	seen := map[int64]string{}
	for _, task := range startupTasks() {
		if task.LockKey == 0 {
			t.Fatalf("task %q has no advisory lock key", task.Name)
		}
		if other, clash := seen[task.LockKey]; clash {
			t.Fatalf("tasks %q and %q share lock key %#x", other, task.Name, task.LockKey)
		}
		seen[task.LockKey] = task.Name
	}
}
