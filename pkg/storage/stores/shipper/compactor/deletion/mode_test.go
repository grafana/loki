package deletion

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAllModes(t *testing.T) {
	modes := AllModes()
	require.ElementsMatch(t, []string{"disabled", "filter-only", "filter-and-delete"}, modes)
}

func TestParseMode(t *testing.T) {
	mode, err := ParseMode("disabled")
	require.NoError(t, err)
	require.Equal(t, Disabled, mode)

	mode, err = ParseMode("filter-only")
	require.NoError(t, err)
	require.Equal(t, FilterOnly, mode)

	mode, err = ParseMode("filter-and-delete")
	require.NoError(t, err)
	require.Equal(t, FilterAndDelete, mode)

	_, err = ParseMode("something-else")
	require.ErrorIs(t, errUnknownMode, err)
}

func TestFilteringEnabled(t *testing.T) {
	enabled, err := FilteringEnabled("disabled")
	require.NoError(t, err)
	require.False(t, enabled)

	enabled, err = FilteringEnabled("filter-only")
	require.NoError(t, err)
	require.True(t, enabled)

	enabled, err = FilteringEnabled("filter-and-delete")
	require.NoError(t, err)
	require.True(t, enabled)

	enabled, err = FilteringEnabled("some other value")
	require.Error(t, err)
	require.False(t, enabled)
}
