package compactor

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
)

func TestNewLogAgeBuckets(t *testing.T) {
	t.Run("doubles from one hour and ends at the span of two windows", func(t *testing.T) {
		b := newLogAgeBuckets(1)
		require.Equal(t, []string{"1h", "2h", "4h", "8h", "16h", "24h", "+Inf"}, b.labels())
	})

	t.Run("ends at the span of four windows when the lookback is three", func(t *testing.T) {
		b := newLogAgeBuckets(3)
		require.Equal(t, []string{"1h", "2h", "4h", "8h", "16h", "32h", "48h", "+Inf"}, b.labels())
	})
}

func TestLogAgeBucketsLabel(t *testing.T) {
	now := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	b := newLogAgeBuckets(1)

	for _, tc := range []struct {
		name string
		age  time.Duration
		want string
	}{
		{name: "an age of exactly one hour goes to the 1h bucket", age: time.Hour, want: "1h"},
		{name: "an age just above one hour goes to the 2h bucket", age: time.Hour + time.Second, want: "2h"},
		{name: "an age of twelve hours goes to the 16h bucket", age: 12 * time.Hour, want: "16h"},
		{name: "an age of the full span goes to the 24h bucket", age: 2 * metastore.MetastoreWindowSize, want: "24h"},
		{name: "an age above the span goes to the overflow bucket", age: 25 * time.Hour, want: "+Inf"},
		{name: "a timestamp after now goes to the first bucket", age: -time.Minute, want: "1h"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, b.label(now.Add(-tc.age), now))
		})
	}
}
