package meetings

import (
	"testing"
	"time"
)

func TestMaintenanceTasksDescribesTheHourlySweeps(t *testing.T) {
	if tasks := MaintenanceTasks(nil); tasks != nil {
		t.Fatalf("a nil service produced tasks: %+v", tasks)
	}

	tasks := MaintenanceTasks(&Service{})
	if len(tasks) != 2 {
		t.Fatalf("tasks %+v", tasks)
	}
	task := tasks[0]
	if task.Name != TaskShareLinkRequestSweep {
		t.Fatalf("name %q", task.Name)
	}
	if task.Interval != time.Hour {
		t.Fatalf("interval %s", task.Interval)
	}
	if task.LockKey != LockKeyShareLinkRequestSweep {
		t.Fatalf("lock key %d", task.LockKey)
	}
	if task.Run == nil {
		t.Fatal("the sweep task has no handler")
	}

	sweep := tasks[1]
	if sweep.Name != TaskAbandonedUploadSweep {
		t.Fatalf("name %q", sweep.Name)
	}
	if sweep.Interval != time.Hour {
		t.Fatalf("interval %s", sweep.Interval)
	}
	if sweep.LockKey != LockKeyAbandonedUploadSweep {
		t.Fatalf("lock key %d", sweep.LockKey)
	}
	if sweep.LockKey == task.LockKey {
		t.Fatal("the two sweeps share a leader lock key")
	}
	if sweep.Run == nil {
		t.Fatal("the abandoned upload sweep has no handler")
	}
}
