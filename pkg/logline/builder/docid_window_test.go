package builder

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestDocIDWindowEnd pins the docID window's end date to the instants
// documented in docid_window.go and CLAUDE.md: docIDEpoch
// (2026-01-01T00:00:00Z) + (2^32 / document_shards) × document_interval. If
// this test fails, either the epoch or the window math changed — update every
// documented window-end date along with it.
func TestDocIDWindowEnd(t *testing.T) {
	require.Equal(t, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), docIDEpoch)

	for _, tt := range []struct {
		name       string
		interval   time.Duration
		shards     int
		wantLength time.Duration
		wantEnd    time.Time
	}{
		{
			name:       "100ms time-only",
			interval:   100 * time.Millisecond,
			wantLength: time.Duration(1<<32) * (100 * time.Millisecond),
			wantEnd:    time.Date(2039, 8, 12, 0, 38, 49, 600_000_000, time.UTC),
		},
		{
			name:       "100ms with document_shards 1 matches time-only",
			interval:   100 * time.Millisecond,
			shards:     1,
			wantLength: time.Duration(1<<32) * (100 * time.Millisecond),
			wantEnd:    time.Date(2039, 8, 12, 0, 38, 49, 600_000_000, time.UTC),
		},
		{
			name:       "v5 default 16s x 32",
			interval:   16 * time.Second,
			shards:     32,
			wantLength: time.Duration(1<<27) * (16 * time.Second),
			wantEnd:    time.Date(2094, 1, 19, 3, 14, 8, 0, time.UTC),
		},
		{
			name:       "16s time-only overflows time.Duration and clamps",
			interval:   16 * time.Second,
			wantLength: math.MaxInt64,
			wantEnd:    docIDEpoch.Add(math.MaxInt64),
		},
		{
			name:       "500ms x 128 ends in 2026",
			interval:   500 * time.Millisecond,
			shards:     128,
			wantLength: time.Duration(1<<25) * (500 * time.Millisecond),
			wantEnd:    time.Date(2026, 7, 14, 4, 20, 16, 0, time.UTC),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.wantLength, docIDWindowLength(tt.interval, tt.shards))
			require.Equal(t, tt.wantEnd, docIDWindowEnd(tt.interval, tt.shards))
		})
	}
}
