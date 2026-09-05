package credits

import (
	"testing"
	"time"
)

func TestPeriodOfTruncatesToTheStartOfTheUTCMonth(t *testing.T) {
	cases := []struct {
		name string
		at   time.Time
		want time.Time
	}{
		{"mid month", time.Date(2026, time.March, 17, 13, 45, 12, 999, time.UTC), time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)},
		{"first instant", time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)},
		{"last instant", time.Date(2026, time.March, 31, 23, 59, 59, 0, time.UTC), time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)},
		{"non utc zone is converted first", time.Date(2026, time.April, 1, 1, 30, 0, 0, time.FixedZone("EAT", 3*60*60)), time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := PeriodOf(tc.at); !got.Equal(tc.want) {
				t.Fatalf("PeriodOf(%s) = %s, want %s", tc.at, got, tc.want)
			}
		})
	}
}

func TestPeriodEndIsTheFirstInstantOfTheNextMonth(t *testing.T) {
	cases := []struct {
		period time.Time
		want   time.Time
	}{
		{time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC)},
		{time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)},
		{time.Date(2026, time.December, 1, 0, 0, 0, 0, time.UTC), time.Date(2027, time.January, 1, 0, 0, 0, 0, time.UTC)},
	}
	for _, tc := range cases {
		if got := PeriodEnd(tc.period); !got.Equal(tc.want) {
			t.Fatalf("PeriodEnd(%s) = %s, want %s", tc.period, got, tc.want)
		}
	}
}

func TestPeriodEndNeverSkipsAMonthOnAShortMonth(t *testing.T) {
	period := PeriodOf(time.Date(2026, time.January, 31, 12, 0, 0, 0, time.UTC))
	if got := PeriodEnd(period); !got.Equal(time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("PeriodEnd(January) = %s, want 2026-02-01", got)
	}
}

func TestUnusedGrantMinutes(t *testing.T) {
	cases := []struct {
		name      string
		remaining int64
		usedSince int64
		want      int64
	}{
		{"nothing used", 300, 0, 300},
		{"some used", 300, 120, 180},
		{"all used", 300, 300, 0},
		{"more than the grant used", 300, 1200, 0},
		{"already clawed back", 0, 0, 0},
		{"negative usage is ignored", 300, -50, 300},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := UnusedGrantMinutes(tc.remaining, tc.usedSince); got != tc.want {
				t.Fatalf("UnusedGrantMinutes(%d, %d) = %d, want %d", tc.remaining, tc.usedSince, got, tc.want)
			}
		})
	}
}

func TestExpiryMinutesIsTheSmallerOfTheBalanceAndTheUnusedGrant(t *testing.T) {
	cases := []struct {
		name      string
		balance   int64
		remaining int64
		usedSince int64
		want      int64
	}{
		{"untouched grant expires whole", 300, 300, 0, 300},
		{"purchased credits survive", 1300, 300, 0, 300},
		{"partly spent grant expires the rest", 50, 300, 250, 50},
		{"grant spent beyond itself takes nothing", 100, 300, 1200, 0},
		{"empty balance takes nothing", 0, 300, 0, 0},
		{"negative balance takes nothing", -10, 300, 0, 0},
		{"balance smaller than the unused remainder is the cap", 40, 300, 100, 40},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ExpiryMinutes(tc.balance, tc.remaining, tc.usedSince)
			if got != tc.want {
				t.Fatalf("ExpiryMinutes(%d, %d, %d) = %d, want %d", tc.balance, tc.remaining, tc.usedSince, got, tc.want)
			}
		})
	}
}
