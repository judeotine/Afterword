package credits

import "time"

func PeriodOf(at time.Time) time.Time {
	utc := at.UTC()
	return time.Date(utc.Year(), utc.Month(), 1, 0, 0, 0, 0, time.UTC)
}

func PeriodEnd(period time.Time) time.Time {
	start := PeriodOf(period)
	return start.AddDate(0, 1, 0)
}

func UnusedGrantMinutes(remaining, usedSince int64) int64 {
	if remaining <= 0 {
		return 0
	}
	if usedSince <= 0 {
		return remaining
	}
	if usedSince >= remaining {
		return 0
	}
	return remaining - usedSince
}

func ExpiryMinutes(balance, remaining, usedSince int64) int64 {
	unused := UnusedGrantMinutes(remaining, usedSince)
	if balance <= 0 || unused <= 0 {
		return 0
	}
	if balance < unused {
		return balance
	}
	return unused
}
