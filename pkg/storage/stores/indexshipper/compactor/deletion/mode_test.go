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

func TestDeleteEnabled(t *testing.T) {
	enabled, err := DeleteEnabled("disabled")
	require.NoError(t, err)
	require.False(t, enabled)

	enabled, err = DeleteEnabled("filter-only")
	require.NoError(t, err)
	require.True(t, enabled)

	enabled, err = DeleteEnabled("filter-and-delete")
	require.NoError(t, err)
	require.True(t, enabled)

	enabled, err = DeleteEnabled("some other value")
	require.Error(t, err)
	require.False(t, enabled)
}
