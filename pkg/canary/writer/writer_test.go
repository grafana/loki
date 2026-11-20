package writer

import (
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
)

type captureWriter struct {
	mu      sync.Mutex
	entries []time.Time
}

func (c *captureWriter) WriteEntry(ts time.Time, _ string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = append(c.entries, ts)
}

func (c *captureWriter) Stop() {}

func TestWriter_OutOfOrderMinEqualsMax(t *testing.T) {
	sent := make(chan time.Time, 10)
	cw := &captureWriter{}
	w := NewWriter(cw, sent, 10*time.Millisecond, 5*time.Second, 5*time.Second, 100, 50, log.NewNopLogger())
	defer w.Stop()

	select {
	case ts := <-sent:
		back := time.Since(ts)
		require.GreaterOrEqual(t, back, 5*time.Second)
		require.Less(t, back, 6*time.Second)
	case <-time.After(2 * time.Second):
		require.FailNow(t, "writer did not send an entry")
	}
}
