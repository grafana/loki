package log

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUnsafeBytes_SafeCopyString(t *testing.T) {
	t.Run("returns a string that does not change when the owner overwrites the line", func(t *testing.T) {
		buf := []byte("line one")
		got := UnsafeBytes{UnsafeData: buf}.SafeCopyString()

		copy(buf, "XXXXXXXX")

		require.Equal(t, "line one", got)
	})

	t.Run("returns an empty string for the zero value", func(t *testing.T) {
		require.Empty(t, UnsafeBytes{}.SafeCopyString())
	})
}
