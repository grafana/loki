package consumer

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
)

// TestProcessor_Stopping_Drain verifies that processor.stopping() drains
// buffered records from the channel and logs the correct outcome.
func TestProcessor_Stopping_Drain(t *testing.T) {
	t.Run("empty channel logs clean drain", func(t *testing.T) {
		var buf bytes.Buffer
		logger := log.NewLogfmtLogger(&buf)

		reg := prometheus.NewRegistry()
		builder := newTestBuilder(t, reg)
		fc := &mockFlushCommitter{}
		ch := make(chan *kgo.Record, 10)

		proc := newProcessor(builder, ch, fc, time.Hour, time.Hour, logger, reg)

		err := proc.stopping(nil)
		require.NoError(t, err)
		require.Contains(t, buf.String(), "inmemory channel drained cleanly on shutdown")
	})

	t.Run("records drained and logged clean", func(t *testing.T) {
		var buf bytes.Buffer
		logger := log.NewLogfmtLogger(&buf)

		reg := prometheus.NewRegistry()
		builder := newTestBuilder(t, reg)
		fc := &mockFlushCommitter{}
		ch := make(chan *kgo.Record, 10)

		// Pre-fill 3 records into channel.
		now := time.Now()
		for i := 0; i < 3; i++ {
			ch <- newTestRecord(t, "tenant1", now.Add(time.Duration(i)*time.Second))
		}

		proc := newProcessor(builder, ch, fc, time.Hour, time.Hour, logger, reg)

		err := proc.stopping(nil)
		require.NoError(t, err)
		require.True(t,
			strings.Contains(buf.String(), "inmemory channel drained cleanly on shutdown"),
			"expected clean drain log, got: %s", buf.String(),
		)
	})
}
