package main

import (
	"context"
	"testing"
	"time"

	"github.com/judeotine/afterword/services/api/internal/auth"
	"github.com/judeotine/afterword/services/api/internal/config"
	"github.com/judeotine/afterword/services/api/internal/jobs"
	"github.com/judeotine/afterword/services/api/internal/meetings"
)

func noopRun(context.Context) error { return nil }

func billingTaskConfig() config.BillingConfig {
	return config.BillingConfig{FreeGrantMinutes: 300, GrantInterval: time.Hour}
}

func TestStartupTasksRegisterWithoutColliding(t *testing.T) {
	tasks := startupTasks(billingTaskConfig(), nil, nil, noopRun, noopRun)
	if len(tasks) != 2 {
		t.Fatalf("startupTasks built %d tasks, want the grant and the reaper", len(tasks))
	}

	scheduler := jobs.NewScheduler(nil)
	for _, task := range tasks {
		if err := scheduler.Register(task); err != nil {
			t.Fatalf("register %q: %v", task.Name, err)
		}
	}
}

func TestStartupTasksDoNotShareANameOrALockKeyWithTheMaintenanceSweeps(t *testing.T) {
	tasks := startupTasks(billingTaskConfig(), nil, nil, noopRun, noopRun)
	names := map[string]bool{
		auth.TaskRetentionSweep:            true,
		meetings.TaskShareLinkRequestSweep: true,
	}
	keys := map[int64]string{
		auth.LockKeyRetentionSweep:            auth.TaskRetentionSweep,
		meetings.LockKeyShareLinkRequestSweep: meetings.TaskShareLinkRequestSweep,
	}

	for _, task := range tasks {
		if task.LockKey == 0 {
			t.Fatalf("task %q has no advisory lock key", task.Name)
		}
		if names[task.Name] {
			t.Fatalf("task %q collides with a maintenance sweep", task.Name)
		}
		if other, clash := keys[task.LockKey]; clash {
			t.Fatalf("tasks %q and %q share lock key %#x", other, task.Name, task.LockKey)
		}
		names[task.Name] = true
		keys[task.LockKey] = task.Name
	}
}

func TestTheGrantTaskIsDroppedWhenGrantsAreDisabled(t *testing.T) {
	cfg := billingTaskConfig()
	cfg.FreeGrantMinutes = 0

	for _, task := range startupTasks(cfg, nil, nil, noopRun, noopRun) {
		if task.Name == taskMonthlyCreditGrant {
			t.Fatal("the monthly grant task was registered with FREE_GRANT_MINUTES at zero")
		}
	}
}

func TestTheReaperIsDroppedWithoutABillingService(t *testing.T) {
	for _, task := range startupTasks(billingTaskConfig(), nil, nil, noopRun, nil) {
		if task.Name == taskPendingPaymentReaper {
			t.Fatal("the pending payment reaper was registered without a billing service")
		}
	}
}
