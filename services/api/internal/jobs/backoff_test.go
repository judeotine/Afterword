package jobs

import (
	"testing"
	"time"
)

func TestDefaultBackoffGrowsExponentiallyAndCaps(t *testing.T) {
	backoff := DefaultBackoff()

	cases := []struct {
		attempt int32
		want    time.Duration
	}{
		{attempt: 1, want: 30 * time.Second},
		{attempt: 2, want: time.Minute},
		{attempt: 3, want: 2 * time.Minute},
		{attempt: 4, want: 4 * time.Minute},
		{attempt: 5, want: 8 * time.Minute},
		{attempt: 8, want: time.Hour},
		{attempt: 40, want: time.Hour},
	}
	for _, tc := range cases {
		if got := backoff.Delay(tc.attempt); got != tc.want {
			t.Errorf("Delay(%d) = %s, want %s", tc.attempt, got, tc.want)
		}
	}
}

func TestBackoffDelayNeverGoesBelowBase(t *testing.T) {
	backoff := DefaultBackoff()
	for _, attempt := range []int32{-5, 0, 1} {
		if got := backoff.Delay(attempt); got != backoff.Base {
			t.Errorf("Delay(%d) = %s, want %s", attempt, got, backoff.Base)
		}
	}
}

func TestBackoffWithoutFactorFallsBackToBase(t *testing.T) {
	backoff := Backoff{Base: 5 * time.Second}
	if got := backoff.Delay(4); got != 5*time.Second {
		t.Fatalf("Delay(4) = %s, want 5s", got)
	}
}
