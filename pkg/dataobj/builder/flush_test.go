package builder

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v4/pkg/dataobj/logsobj"
	"github.com/grafana/loki/v4/pkg/logproto"
	"github.com/grafana/loki/v4/pkg/scratch"
)

// newTestMockBuilder returns a mockBuilder wrapping a real builder that already
// holds a log line, so it can be flushed.
func newTestMockBuilder(t *testing.T) *mockBuilder {
	t.Helper()
	realBuilder, err := logsobj.NewBuilder(testBuilderCfg, scratch.NewMemory(), logsobj.NewBuilderMetrics(), log.NewNopLogger(), logsobj.NoTenantOverrides{})
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
	const (
		expectSuccessMetrics = `
		# HELP loki_dataobj_builder_flushes_total Total number of flushes.
		# TYPE loki_dataobj_builder_flushes_total counter
		loki_dataobj_builder_flushes_total{reason="builder_full"} 1
		loki_dataobj_builder_flushes_total{reason="idle"} 0
		loki_dataobj_builder_flushes_total{reason="max_age"} 0
		# HELP loki_dataobj_builder_flush_failures_total Total number of failed flushes.
		# TYPE loki_dataobj_builder_flush_failures_total counter
		loki_dataobj_builder_flush_failures_total 0
		`
		expectFailureMetrics = `
		# HELP loki_dataobj_builder_flushes_total Total number of flushes.
		# TYPE loki_dataobj_builder_flushes_total counter
		loki_dataobj_builder_flushes_total{reason="builder_full"} 1
		loki_dataobj_builder_flushes_total{reason="idle"} 0
		loki_dataobj_builder_flushes_total{reason="max_age"} 0
		# HELP loki_dataobj_builder_flush_failures_total Total number of failed flushes.
		# TYPE loki_dataobj_builder_flush_failures_total counter
		loki_dataobj_builder_flush_failures_total 1
		`
	)
	flushMetricNames := []string{
		"loki_dataobj_builder_flushes_total",
		"loki_dataobj_builder_flush_failures_total",
	}

	t.Run("should succeed and transfer ownership of the object to the caller", func(t *testing.T) {
		var (
			reg          = prometheus.NewRegistry()
			testCtx      = t.Context()
			testBuilder  = newTestMockBuilder(t)
			testUploader = &mockUploader{}
		)
		testBuilder.flushCloser = &countingCloser{}
		f := newFlusher(testUploader, log.NewNopLogger(), reg)
		obj, objCloser, objPath, err := f.Flush(testCtx, testBuilder, flushReasonBuilderFull)
		require.NoError(t, err)
		require.NotNil(t, obj)
		require.NotNil(t, objCloser)
		require.Equal(t, "object_001", objPath)
		require.Len(t, testUploader.uploaded, 1)
		require.Equal(t, 0, testBuilder.flushCloser.closed)
		require.NoError(t, objCloser.Close())
		require.Equal(t, 1, testBuilder.flushCloser.closed)
		require.NoError(t, testutil.GatherAndCompare(reg, strings.NewReader(expectSuccessMetrics), flushMetricNames...))
	})

	t.Run("should fail when the builder fails", func(t *testing.T) {
		var (
			reg         = prometheus.NewRegistry()
			testCtx     = t.Context()
			testBuilder = newTestMockBuilder(t)
		)
		testBuilder.nextErr = errors.New("mock error")
		f := newFlusher(&mockUploader{}, log.NewNopLogger(), reg)
		obj, objCloser, objPath, err := f.Flush(testCtx, testBuilder, flushReasonBuilderFull)
		require.EqualError(t, err, "failed to flush data object builder: mock error")
		require.Nil(t, obj)
		require.Nil(t, objCloser)
		require.Empty(t, objPath)
		require.NoError(t, testutil.GatherAndCompare(reg, strings.NewReader(expectFailureMetrics), flushMetricNames...))
	})

	t.Run("should release the object when the upload fails", func(t *testing.T) {
		var (
			reg         = prometheus.NewRegistry()
			testCtx     = t.Context()
			testBuilder = newTestMockBuilder(t)
		)
		testBuilder.flushCloser = &countingCloser{}
		f := newFlusher(&failureUploader{}, log.NewNopLogger(), reg)
		obj, objCloser, objPath, err := f.Flush(testCtx, testBuilder, flushReasonBuilderFull)
		require.EqualError(t, err, "failed to upload object: mock error")
		require.Nil(t, obj)
		require.Nil(t, objCloser)
		require.Empty(t, objPath)
		require.Equal(t, 1, testBuilder.flushCloser.closed)
		require.NoError(t, testutil.GatherAndCompare(reg, strings.NewReader(expectFailureMetrics), flushMetricNames...))
	})

	t.Run("should count a failure to release the uploaded object without failing the flush", func(t *testing.T) {
		var (
			reg         = prometheus.NewRegistry()
			testCtx     = t.Context()
			testBuilder = newTestMockBuilder(t)
		)
		testBuilder.flushCloser = &countingCloser{err: errors.New("close error")}
		f := newFlusher(&mockUploader{}, log.NewNopLogger(), reg)
		obj, objCloser, objPath, err := f.Flush(testCtx, testBuilder, flushReasonBuilderFull)
		require.NoError(t, err)
		require.NotNil(t, obj)
		require.Equal(t, "object_001", objPath)
		require.NoError(t, objCloser.Close())
		require.Equal(t, 1, testBuilder.flushCloser.closed)
		require.NoError(t, testutil.GatherAndCompare(reg, strings.NewReader(expectSuccessMetrics), flushMetricNames...))
		require.NoError(t, testutil.GatherAndCompare(reg, strings.NewReader(`
		# HELP loki_dataobj_builder_release_failures_total Total number of failures to release a flushed data object's scratch storage. These do not fail the flush.
		# TYPE loki_dataobj_builder_release_failures_total counter
		loki_dataobj_builder_release_failures_total 1
		`), "loki_dataobj_builder_release_failures_total"))
	})

	t.Run("should return the upload error when releasing the object also fails", func(t *testing.T) {
		var (
			testCtx     = t.Context()
			testBuilder = newTestMockBuilder(t)
		)
		testBuilder.flushCloser = &countingCloser{err: errors.New("close error")}
		f := newFlusher(&failureUploader{}, log.NewNopLogger(), prometheus.NewRegistry())
		_, _, _, err := f.Flush(testCtx, testBuilder, flushReasonBuilderFull)
		require.EqualError(t, err, "failed to upload object: mock error")
		require.Equal(t, 1, testBuilder.flushCloser.closed)
	})
}
