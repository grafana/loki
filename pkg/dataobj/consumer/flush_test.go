package consumer

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/scratch"
)

func TestFlusher_Flush(t *testing.T) {
	var (
		testCtx       = t.Context()
		testBuilder   *mockBuilder
		testKafka     = &mockKafka{}
		testUploader  = &mockUploader{}
		testCommitter = &mockCommitter{}
		now           = time.Now()
	)
	// Init the builder and append some logs so it can be flushed.
	realBuilder, err := logsobj.NewBuilder(testBuilderCfg, scratch.NewMemory())
	require.NoError(t, err)
	testBuilder = &mockBuilder{builder: realBuilder}
	require.NoError(t, testBuilder.Append("test", logproto.Stream{
		Labels: `{foo="bar"}`,
		Entries: []logproto.Entry{
			{Timestamp: now, Line: "baz"},
		},
	}))
	// Create a flusher with mocks.
	f := newFlusher(testUploader, testCommitter, testKafka, 23, 10, log.NewNopLogger(), prometheus.NewRegistry())
	// Flush the builder we created earlier.
	require.NoError(t, f.doJob(testCtx, flushJob{
		builder:   testBuilder,
		startTime: now,
		offset:    1,
	}))
	// Check that the dataobj was flushed and uploaded.
	require.Len(t, testUploader.uploaded, 1)
	// Check that the correct offset was comitted.
	require.Len(t, testCommitter.offsets, 1)
	require.Equal(t, int64(1), testCommitter.offsets[0])
	// Check that a metastore event was produced.
	require.Len(t, testKafka.produced, 1)
	// The produced partition should equal the flushers partition divided by the
	// partition ratio, using integer division rules.
	require.Equal(t, int32(2), testKafka.produced[0].Partition)
}

func TestFlusher_FlushAsync(t *testing.T) {
	t.Run("promise is invoked", func(t *testing.T) {
		var (
			testCtx = t.Context()
			done    = make(chan struct{})
			invoked bool
		)
		f := flusherImpl{
			jobs: make(chan flushJob, 1),
			jobFunc: func(_ context.Context, _ flushJob) error {
				// Mock success so promise is invoked.
				return nil
			},
			logger: log.NewNopLogger(),
		}
		// Start the flusher. It will stop at the end of the test as testCtx
		// is canceled.
		go func() { _ = f.Run(testCtx) }()
		f.FlushAsync(testCtx, &mockBuilder{}, time.Now(), 0, func(err error) {
			require.NoError(t, err)
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
		f := flusherImpl{
			jobs: make(chan flushJob, 1),
			jobFunc: func(_ context.Context, _ flushJob) error {
				return errors.New("mock error")
			},
			logger: log.NewNopLogger(),
		}
		go func() { _ = f.Run(testCtx) }()
		f.FlushAsync(testCtx, &mockBuilder{}, time.Now(), 0, func(err error) {
			require.EqualError(t, err, "mock error")
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
		f := flusherImpl{
			// Use a buffered chan so select inside [FlushAsync] chooses the
			// [ctx.Done()] case.
			jobs:   make(chan flushJob),
			logger: log.NewNopLogger(),
		}
		// Cancel the context so FlushAsync calls the promise.
		cancel()
		f.FlushAsync(cancelCtx, &mockBuilder{}, time.Now(), 0, func(err error) {
			require.EqualError(t, err, "context canceled")
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
