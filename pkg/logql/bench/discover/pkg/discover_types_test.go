package discover

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestConfigDefaults verifies that Config applies the correct
// defaults when time fields are left at zero-values.
func TestConfigDefaults(t *testing.T) {
	t.Run("To defaults to approximately now", func(t *testing.T) {
		before := time.Now()
		cfg := Config{}
		to := cfg.effectiveTo()
		after := time.Now()

		require.True(t, !to.Before(before),
			"effectiveTo() %v should not be before test start %v", to, before)
		require.True(t, !to.After(after),
			"effectiveTo() %v should not be after test end %v", to, after)
	})

	t.Run("From defaults to 24h before effectiveTo when both are zero", func(t *testing.T) {
		cfg := Config{}
		to := cfg.effectiveTo()
		from := cfg.effectiveFrom()

		diff := to.Sub(from)
		// Allow a small epsilon for timing between calls
		require.InDelta(t, (24 * time.Hour).Seconds(), diff.Seconds(), 1.0,
			"default window should be 24h; got %v", diff)
	})

	t.Run("explicit From is preserved", func(t *testing.T) {
		fixedFrom := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		cfg := Config{From: fixedFrom}
		require.Equal(t, fixedFrom, cfg.effectiveFrom())
	})

	t.Run("explicit To is preserved", func(t *testing.T) {
		fixedTo := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
		cfg := Config{To: fixedTo}
		require.Equal(t, fixedTo, cfg.effectiveTo())
	})

	t.Run("From defaults to 24h before explicit To", func(t *testing.T) {
		fixedTo := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)
		cfg := Config{To: fixedTo}
		expected := fixedTo.Add(-24 * time.Hour)
		require.Equal(t, expected, cfg.effectiveFrom())
	})
}
