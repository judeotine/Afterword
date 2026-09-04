package meetings

import (
	"testing"
	"time"
)

func TestMaintenanceTasksDescribesTheHourlyShareSweep(t *testing.T) {
	if tasks := MaintenanceTasks(nil); tasks != nil {
		t.Fatalf("a nil service produced tasks: %+v", tasks)
	}

	tasks := MaintenanceTasks(&Service{})
	if len(tasks) != 1 {
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
}
