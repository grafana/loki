package builder

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestDocIDWindowEndAtDefaultInterval pins the docID window's end date at the
// default 100ms document_interval to the exact instant documented in
// docid_window.go and CLAUDE.md: docIDEpoch (2026-01-01T00:00:00Z) + 2^32 ×
// 100ms = 429,496,729.6s ≈ 4971.03 days = 2039-08-12T00:38:49.6Z. If this
// test fails, either the epoch or the window math changed — update every
// documented window-end date along with it.
func TestDocIDWindowEndAtDefaultInterval(t *testing.T) {
	require.Equal(t, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), docIDEpoch)

	length := docIDWindowLength(100 * time.Millisecond)
	require.Equal(t, time.Duration(1<<32)*(100*time.Millisecond), length)

	end := docIDWindowEnd(100 * time.Millisecond)
	require.Equal(t, time.Date(2039, 8, 12, 0, 38, 49, 600_000_000, time.UTC), end,
		"window end at the default interval must match the date documented in docid_window.go and CLAUDE.md")
}
