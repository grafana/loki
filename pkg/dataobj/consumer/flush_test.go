package consumer

import (
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

const (
	expectSuccessMetrics = `
	# HELP loki_dataobj_consumer_flushes_total Total number of flushes.
	# TYPE loki_dataobj_consumer_flushes_total counter
	loki_dataobj_consumer_flushes_total{reason="builder_full"} 1
	loki_dataobj_consumer_flushes_total{reason="idle"} 0
	loki_dataobj_consumer_flushes_total{reason="max_age"} 0
	# HELP loki_dataobj_consumer_flush_failures_total Total number of failed flushes.
	# TYPE loki_dataobj_consumer_flush_failures_total counter
	loki_dataobj_consumer_flush_failures_total 0
	`
	expectFailureMetrics = `
	# HELP loki_dataobj_consumer_flushes_total Total number of flushes.
	# TYPE loki_dataobj_consumer_flushes_total counter
	loki_dataobj_consumer_flushes_total{reason="builder_full"} 1
	loki_dataobj_consumer_flushes_total{reason="idle"} 0
	loki_dataobj_consumer_flushes_total{reason="max_age"} 0
	# HELP loki_dataobj_consumer_flush_failures_total Total number of failed flushes.
	# TYPE loki_dataobj_consumer_flush_failures_total counter
	loki_dataobj_consumer_flush_failures_total 1
	`
)

var flushMetricNames = []string{
	"loki_dataobj_consumer_flushes_total",
	"loki_dataobj_consumer_flush_failures_total",
}

// newTestMockBuilder returns a mockBuilder wrapping a real builder that already
// holds a log line, so it can be flushed.
func newTestMockBuilder(t *testing.T) *mockBuilder {
	t.Helper()
	realBuilder, err := logsobj.NewBuilder(testBuilderCfg, scratch.NewMemory(), logsobj.NewBuilderMetrics(), log.NewNopLogger(), nil)
	require.NoError(t, err)
	b := &mockBuilder{builder: realBuilder}
	now := time.Now()
	require.NoError(t, b.Append("test", logproto.Stream{
		Labels:  `{foo="bar"}`,
		Entries: []logproto.Entry{{Timestamp: now, Line: "baz"}},
	}, now))
	return b
}

func TestFlusher_Flush(t *testing.T) {
	t.Run("should succeed", func(t *testing.T) {
		var (
			reg          = prometheus.NewRegistry()
			testCtx      = t.Context()
			testBuilder  = newTestMockBuilder(t)
			testSorter   = &mockSorter{}
			testUploader = &mockUploader{}
		)
		f := newFlusher(testSorter, testUploader, log.NewNopLogger(), reg)
		obj, objCloser, objPath, err := f.Flush(testCtx, testBuilder, flushReasonBuilderFull)
		require.NoError(t, err)
		require.NotNil(t, obj)
		require.NotNil(t, objCloser)
		require.Equal(t, "object_001", objPath)
		// Check that the dataobj was flushed and uploaded.
		require.Len(t, testUploader.uploaded, 1)
		// The sorted object is the caller's to release, so the flusher must not
		// have closed it.
		require.Equal(t, 0, testSorter.closer.closed)
		require.NoError(t, objCloser.Close())
		require.Equal(t, 1, testSorter.closer.closed)
		require.NoError(t, testutil.GatherAndCompare(reg, strings.NewReader(expectSuccessMetrics), flushMetricNames...))
	})

	t.Run("should fail when the builder fails", func(t *testing.T) {
		var (
			reg         = prometheus.NewRegistry()
			testCtx     = t.Context()
			testBuilder = newTestMockBuilder(t)
		)
		testBuilder.nextErr = errors.New("mock error")
		f := newFlusher(&mockSorter{}, &mockUploader{}, log.NewNopLogger(), reg)
		obj, objCloser, objPath, err := f.Flush(testCtx, testBuilder, flushReasonBuilderFull)
		require.EqualError(t, err, "failed to flush data object builder: mock error")
		// Nothing was built, so there is nothing for the caller to release.
		require.Nil(t, obj)
		require.Nil(t, objCloser)
		require.Empty(t, objPath)
		require.NoError(t, testutil.GatherAndCompare(reg, strings.NewReader(expectFailureMetrics), flushMetricNames...))
	})

	t.Run("should release the sorted object when the upload fails", func(t *testing.T) {
		var (
			reg         = prometheus.NewRegistry()
			testCtx     = t.Context()
			testBuilder = newTestMockBuilder(t)
			testSorter  = &mockSorter{}
		)
		f := newFlusher(testSorter, &failureUploader{}, log.NewNopLogger(), reg)
		obj, objCloser, objPath, err := f.Flush(testCtx, testBuilder, flushReasonBuilderFull)
		require.EqualError(t, err, "failed to upload object: mock error")
		require.Nil(t, obj)
		require.Nil(t, objCloser)
		require.Empty(t, objPath)
		// The caller gets nothing to close, so the flusher must release the
		// sorted object itself rather than leaking its scratch storage.
		require.Equal(t, 1, testSorter.closer.closed)
		require.NoError(t, testutil.GatherAndCompare(reg, strings.NewReader(expectFailureMetrics), flushMetricNames...))
	})

	t.Run("should not fail the flush when releasing the unsorted object fails", func(t *testing.T) {
		var (
			reg         = prometheus.NewRegistry()
			testCtx     = t.Context()
			testBuilder = newTestMockBuilder(t)
			testSorter  = &mockSorter{}
		)
		// The object is already uploaded by the time the unsorted object is
		// released, so a failure to release it must be logged, not returned.
		testBuilder.flushCloser = &countingCloser{err: errors.New("close error")}
		f := newFlusher(testSorter, &mockUploader{}, log.NewNopLogger(), reg)
		obj, objCloser, objPath, err := f.Flush(testCtx, testBuilder, flushReasonBuilderFull)
		require.NoError(t, err)
		require.NotNil(t, obj)
		require.Equal(t, "object_001", objPath)
		require.Equal(t, 1, testBuilder.flushCloser.closed)
		require.NoError(t, objCloser.Close())
		require.NoError(t, testutil.GatherAndCompare(reg, strings.NewReader(expectSuccessMetrics), flushMetricNames...))
	})

	t.Run("should return the upload error when releasing the sorted object also fails", func(t *testing.T) {
		var (
			testCtx     = t.Context()
			testBuilder = newTestMockBuilder(t)
			testSorter  = &mockSorter{closer: countingCloser{err: errors.New("close error")}}
		)
		f := newFlusher(testSorter, &failureUploader{}, log.NewNopLogger(), prometheus.NewRegistry())
		_, _, _, err := f.Flush(testCtx, testBuilder, flushReasonBuilderFull)
		require.EqualError(t, err, "failed to upload object: mock error")
		require.Equal(t, 1, testSorter.closer.closed)
	})
}
