package consumer

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/scratch"
)

func TestFlusher_Flush(t *testing.T) {
	t.Run("should succeed", func(t *testing.T) {
		var (
			reg          = prometheus.NewRegistry()
			testCtx      = t.Context()
			testBuilder  *mockBuilder
			testSorter   = &mockSorter{}
			testUploader = &mockUploader{}
			now          = time.Now()
		)
		// Create a builder and append some logs so it can be flushed.
		realBuilder, err := logsobj.NewBuilder(testBuilderCfg, scratch.NewMemory())
		require.NoError(t, err)
		testBuilder = &mockBuilder{builder: realBuilder}
		require.NoError(t, testBuilder.Append("test", logproto.Stream{
			Labels: `{foo="bar"}`,
			Entries: []logproto.Entry{
				{Timestamp: now, Line: "baz"},
			},
		}))
		f := newFlusher(testSorter, testUploader, log.NewNopLogger(), reg)
		// Flush the builder we created earlier.
		objectPath, err := f.Flush(testCtx, testBuilder, "test_sync")
		require.NoError(t, err)
		require.Equal(t, "object_001", objectPath)
		// Check that the dataobj was flushed and uploaded.
		require.Len(t, testUploader.uploaded, 1)
		require.NoError(t, testutil.GatherAndCompare(reg, strings.NewReader(`
	# HELP loki_dataobj_consumer_flushes_total Total number of flushes.
	# TYPE loki_dataobj_consumer_flushes_total counter
	loki_dataobj_consumer_flushes_total{reason="test_sync"} 1
	# HELP loki_dataobj_consumer_flush_failures_total Total number of failed flushes.
	# TYPE loki_dataobj_consumer_flush_failures_total counter
	loki_dataobj_consumer_flush_failures_total 0
	`), "loki_dataobj_consumer_flushes_total", "loki_dataobj_consumer_flush_failures_total"))
	})

	t.Run("should fail", func(t *testing.T) {
		var (
			reg         = prometheus.NewRegistry()
			testCtx     = t.Context()
			testBuilder *mockBuilder
		)
		f := newFlusher(nil, nil, log.NewNopLogger(), reg)
		// Override the flush func to force a failure.
		f.flushFunc = func(_ context.Context, _ flushJob) (string, error) {
			return "", errors.New("mock error")
		}
		// Flush the builder we created earlier.
		objectPath, err := f.Flush(testCtx, testBuilder, "test_sync")
		require.EqualError(t, err, "mock error")
		require.Equal(t, "", objectPath)
		require.NoError(t, testutil.GatherAndCompare(reg, strings.NewReader(`
		# HELP loki_dataobj_consumer_flushes_total Total number of flushes.
		# TYPE loki_dataobj_consumer_flushes_total counter
		loki_dataobj_consumer_flushes_total{reason="test_sync"} 1
		# HELP loki_dataobj_consumer_flush_failures_total Total number of failed flushes.
		# TYPE loki_dataobj_consumer_flush_failures_total counter
		loki_dataobj_consumer_flush_failures_total 1
		`), "loki_dataobj_consumer_flushes_total", "loki_dataobj_consumer_flush_failures_total"))
	})
}

func TestFlusher_FlushAsync(t *testing.T) {
	t.Run("promise is invoked", func(t *testing.T) {
		var (
			testCtx = t.Context()
			done    = make(chan struct{})
			invoked bool
		)
		f := newFlusher(nil, nil, log.NewNopLogger(), prometheus.NewRegistry())
		f.flushFunc = func(_ context.Context, _ flushJob) (string, error) {
			// Mock success so promise is invoked.
			return "", nil
		}
		f.FlushAsync(testCtx, &mockBuilder{}, "flush_async", func(res flushJobResult) {
			require.NoError(t, res.err)
			invoked = true
			close(done)
		})
		select {
		case <-testCtx.Done():
			t.Fatal("context canceled")
		case <-done:
		}
		require.True(t, invoked)
	})

	t.Run("promise is invoked with error", func(t *testing.T) {
		var (
			testCtx = t.Context()
			done    = make(chan struct{})
			invoked bool
		)
		f := newFlusher(nil, nil, log.NewNopLogger(), prometheus.NewRegistry())
		f.flushFunc = func(_ context.Context, _ flushJob) (string, error) {
			return "", errors.New("mock error")
		}
		f.FlushAsync(testCtx, &mockBuilder{}, "flush_async", func(res flushJobResult) {
			require.EqualError(t, res.err, "mock error")
			invoked = true
			close(done)
		})
		select {
		case <-testCtx.Done():
			t.Fatal("context canceled")
		case <-done:
		}
		require.True(t, invoked)
	})

	t.Run("promise is invoked when context is canceled", func(t *testing.T) {
		var (
			testCtx           = t.Context()
			cancelCtx, cancel = context.WithCancel(testCtx)
			done              = make(chan struct{})
			invoked           bool
		)
		f := newFlusher(nil, nil, log.NewNopLogger(), prometheus.NewRegistry())
		f.flushFunc = func(ctx context.Context, _ flushJob) (string, error) {
			<-ctx.Done()
			return "", ctx.Err()
		}
		// Cancel the context so FlushAsync calls the promise.
		cancel()
		f.FlushAsync(cancelCtx, &mockBuilder{}, "flush_async", func(res flushJobResult) {
			require.EqualError(t, res.err, "context canceled")
			invoked = true
			close(done)
		})
		select {
		case <-done:
		case <-testCtx.Done():
			t.Fatal("test timed out")
		}
		require.True(t, invoked)
	})
}
