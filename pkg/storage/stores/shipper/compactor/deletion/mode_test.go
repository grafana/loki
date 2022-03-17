package deletion

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAllModes(t *testing.T) {
	modes := AllModes()
	require.ElementsMatch(t, []string{"disabled", "whole-stream-deletion", "filter-only", "filter-and-delete"}, modes)
}

func TestParseMode(t *testing.T) {
	mode, err := ParseMode("disabled")
	require.NoError(t, err)
	require.Equal(t, Disabled, mode)

	mode, err = ParseMode("whole-stream-deletion")
	require.NoError(t, err)
	require.Equal(t, WholeStreamDeletion, mode)

	mode, err = ParseMode("filter-only")
	require.NoError(t, err)
	require.Equal(t, FilterOnly, mode)

	mode, err = ParseMode("filter-and-delete")
	require.NoError(t, err)
	require.Equal(t, FilterAndDelete, mode)

	_, err = ParseMode("something-else")
	require.ErrorIs(t, errUnknownMode, err)
}
