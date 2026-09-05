package meetings

import (
	"math"
	"testing"

	"github.com/judeotine/afterword/services/api/internal/credits"
)

func TestTranscribeMinutesArePricedFromTheStoredBytes(t *testing.T) {
	bytesPerMinute := MaxAudioBytesPerSecond * SecondsPerMinute

	cases := []struct {
		name  string
		bytes int64
		want  int32
	}{
		{"no object", 0, 0},
		{"a negative size cannot be priced", -1, 0},
		{"a single byte still costs a credit", 1, 1},
		{"exactly one minute", bytesPerMinute, 1},
		{"one byte over a minute", bytesPerMinute + 1, 2},
		{"exactly two minutes", 2 * bytesPerMinute, 2},
		{"a day of the densest audio", MaxBillableMinutes * bytesPerMinute, int32(MaxBillableMinutes)},
		{"more than a day is clamped", 10 * MaxBillableMinutes * bytesPerMinute, int32(MaxBillableMinutes)},
		{"the largest int64 cannot overflow", math.MaxInt64, int32(MaxBillableMinutes)},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := TranscribeMinutes(testCase.bytes); got != testCase.want {
				t.Fatalf("TranscribeMinutes(%d) = %d, want %d", testCase.bytes, got, testCase.want)
			}
		})
	}
}

func TestSummaryMinutesAreATenthOfTheTranscriptRoundedUp(t *testing.T) {
	bytesPerMinute := MaxAudioBytesPerSecond * SecondsPerMinute

	cases := []struct {
		name  string
		bytes int64
		want  int32
	}{
		{"a transcript with no audio still costs a credit", 0, 1},
		{"one minute", bytesPerMinute, 1},
		{"ten minutes", 10 * bytesPerMinute, 1},
		{"eleven minutes", 11 * bytesPerMinute, 2},
		{"twenty minutes", 20 * bytesPerMinute, 2},
		{"a day", MaxBillableMinutes * bytesPerMinute, 144},
		{"the largest int64 cannot overflow", math.MaxInt64, int32(MaxBillableMinutes)},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := SummaryMinutes(testCase.bytes); got != testCase.want {
				t.Fatalf("SummaryMinutes(%d) = %d, want %d", testCase.bytes, got, testCase.want)
			}
		})
	}
}

func TestUsageForPicksTheReasonFromTheKind(t *testing.T) {
	minutes, reason := UsageFor(KindTranscribe, MaxAudioBytesPerSecond*SecondsPerMinute)
	if minutes != 1 || reason != credits.ReasonTranscribeUsage {
		t.Fatalf("UsageFor(transcribe) = %d, %s", minutes, reason)
	}

	minutes, reason = UsageFor(KindSummarise, MaxAudioBytesPerSecond*SecondsPerMinute)
	if minutes != 1 || reason != credits.ReasonSummaryUsage {
		t.Fatalf("UsageFor(summarise) = %d, %s", minutes, reason)
	}
}

func TestCeilDivNeverOverflowsOrDividesByZero(t *testing.T) {
	if got := ceilDiv(math.MaxInt64, 1); got != math.MaxInt64 {
		t.Fatalf("ceilDiv(maxint64, 1) = %d", got)
	}
	if got := ceilDiv(math.MaxInt64, 0); got != 0 {
		t.Fatalf("ceilDiv(maxint64, 0) = %d, want 0", got)
	}
	if got := ceilDiv(-1, 60); got != 0 {
		t.Fatalf("ceilDiv(-1, 60) = %d, want 0", got)
	}
}
