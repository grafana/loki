package stats

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStatSize(t *testing.T) {
	t.Run("counts the object path, sort schema and label values but not the label names", func(t *testing.T) {
		stat := Stat{
			ObjectPath:       "obj",
			SortSchema:       "label:service",
			Labels:           map[string]string{"service": "api"},
			UncompressedSize: 1000,
		}
		require.Equal(t, int64(3+13+3), stat.Size())
	})
}
