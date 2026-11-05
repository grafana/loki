package postings

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestTimeRange(t *testing.T) {
	t10 := time.Unix(10, 0).UTC()
	t20 := time.Unix(20, 0).UTC()
	t30 := time.Unix(30, 0).UTC()

	t.Run("At returns a range with equal min and max", func(t *testing.T) {
		r := At(t20)
		require.Equal(t, t20, r.Min())
		require.Equal(t, t20, r.Max())
	})

	t.Run("Extend lowers the min when the timestamp is earlier", func(t *testing.T) {
		r := At(t20).Extend(t10)
		require.Equal(t, t10, r.Min())
		require.Equal(t, t20, r.Max())
	})

	t.Run("Extend raises the max when the timestamp is later", func(t *testing.T) {
		r := At(t20).Extend(t30)
		require.Equal(t, t20, r.Min())
		require.Equal(t, t30, r.Max())
	})

	t.Run("Extend keeps the range when the timestamp is inside it", func(t *testing.T) {
		r := At(t10).Extend(t30).Extend(t20)
		require.Equal(t, t10, r.Min())
		require.Equal(t, t30, r.Max())
	})

	t.Run("Extend does not modify the receiver", func(t *testing.T) {
		r := At(t20)
		_ = r.Extend(t10)
		require.Equal(t, t20, r.Min())
	})
}
