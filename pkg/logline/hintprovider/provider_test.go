package hintprovider

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFormatHintRanges_Empty(t *testing.T) {
	require.Equal(t, "[]", FormatHintRanges(nil))
	require.Equal(t, "[]", FormatHintRanges([]TimeRange{}))

	var hints *Hints
	require.Equal(t, "[]", hints.String())
}

func TestFormatHintRanges_Basic(t *testing.T) {
	base := time.Date(2026, 2, 26, 10, 0, 0, 123*int(time.Millisecond), time.UTC)
	ranges := []TimeRange{
		{Start: base, End: base.Add(10 * time.Second)},
		{Start: base.Add(time.Minute), End: base.Add(2 * time.Minute)},
	}

	got := FormatHintRanges(ranges)
	require.Equal(
		t,
		"[2026-02-26T10:00:00.123Z +10s];[2026-02-26T10:01:00.123Z +1m0s]",
		got,
	)
	require.Equal(t, got, (&Hints{TimeRanges: ranges}).String())
}

func TestFormatHintRanges_Passthrough(t *testing.T) {
	end := time.Date(2026, 7, 9, 8, 42, 59, 500*int(time.Millisecond), time.UTC)
	got := FormatHintRanges([]TimeRange{{End: end}})
	require.Equal(t, "[passthrough,2026-07-09T08:42:59.500Z]", got)
}

func TestFormatHintRanges_Truncates(t *testing.T) {
	base := time.Date(2026, 2, 26, 10, 0, 0, 0, time.UTC)
	ranges := make([]TimeRange, 0, maxLoggedHintRanges+2)
	for i := range maxLoggedHintRanges + 2 {
		start := base.Add(time.Duration(i) * time.Minute)
		ranges = append(ranges, TimeRange{Start: start, End: start.Add(10 * time.Second)})
	}

	got := FormatHintRanges(ranges)
	require.Contains(t, got, "...(+2 more)")
	require.NotContains(t, got, "[2026-02-26T10:10:00.000Z +10s]")
}
