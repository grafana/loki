package util

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestJitter(t *testing.T) {
	j := NewJitter(time.Second, 100*time.Millisecond)
	for i := 0; i < 1000; i++ {
		d := j.Duration()
		require.Greater(t, d, 900*time.Millisecond)
		require.Less(t, d, 1100*time.Millisecond)
	}
}

func TestTickerWithJitter(t *testing.T) {
	ticker := NewTickerWithJitter(100*time.Millisecond, 10*time.Millisecond)
	defer ticker.Stop()
	ts := <-ticker.C
	require.NotNil(t, ts)
}
