package jobs

import (
	"math"
	"time"
)

const (
	DefaultBackoffBase   = 30 * time.Second
	DefaultBackoffFactor = 2.0
	DefaultBackoffMax    = time.Hour
	DefaultMaxAttempts   = 5
)

type Backoff struct {
	Base   time.Duration
	Factor float64
	Max    time.Duration
}

func DefaultBackoff() Backoff {
	return Backoff{Base: DefaultBackoffBase, Factor: DefaultBackoffFactor, Max: DefaultBackoffMax}
}

func (b Backoff) Delay(attempt int32) time.Duration {
	base := b.Base
	if base <= 0 {
		base = DefaultBackoffBase
	}
	if attempt < 1 {
		attempt = 1
	}
	if b.Factor <= 1 {
		return b.clamp(base)
	}
	scaled := float64(base) * math.Pow(b.Factor, float64(attempt-1))
	if math.IsInf(scaled, 0) || scaled >= float64(math.MaxInt64) {
		return b.clamp(time.Duration(math.MaxInt64))
	}
	return b.clamp(time.Duration(scaled))
}

func (b Backoff) clamp(delay time.Duration) time.Duration {
	if b.Max > 0 && delay > b.Max {
		return b.Max
	}
	return delay
}
