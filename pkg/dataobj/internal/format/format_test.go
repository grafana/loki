package format

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestColumnarPageHasChecksum(t *testing.T) {
	for _, version := range []uint32{0, 1, 2, 3} {
		t.Run(fmt.Sprintf("returns false for version %d", version), func(t *testing.T) {
			require.False(t, ColumnarPageHasChecksum(version))
		})
	}

	for _, version := range []uint32{4, 5, 100} {
		t.Run(fmt.Sprintf("returns true for version %d", version), func(t *testing.T) {
			require.True(t, ColumnarPageHasChecksum(version))
		})
	}
}
