package index

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore/multitenancy"
)

// stubCalculator drives SimpleIndexer down a chosen path without building a
// real index.
type stubCalculator struct {
	obj      *dataobj.Object
	calcErr  error
	flushErr error
	// closeErr, when set, is returned by the closer handed back from Flush.
	closeErr error

	calcCalls  int
	flushCalls int
	resetCalls int
}

func (c *stubCalculator) Calculate(_ context.Context, _ log.Logger, _ *dataobj.Object, _ string) error {
	c.calcCalls++
	return c.calcErr
}

func (c *stubCalculator) Flush() (*dataobj.Object, io.Closer, []multitenancy.TimeRange, error) {
	c.flushCalls++
	if c.flushErr != nil {
		return nil, nil, nil, c.flushErr
	}
	ranges := []multitenancy.TimeRange{{
		Tenant:  "test",
		MinTime: time.Now(),
		MaxTime: time.Now().Add(time.Hour),
	}}
	return c.obj, errCloser{err: c.closeErr}, ranges, nil
}

// errCloser fails to close when err is set.
type errCloser struct{ err error }

func (c errCloser) Close() error { return c.err }

func (c *stubCalculator) Reset()       { c.resetCalls++ }
func (c *stubCalculator) IsFull() bool { return false }

func newTestSimpleIndexer(t *testing.T, calc calculator) (*SimpleIndexer, *objstore.InMemBucket, prometheus.Gatherer) {
	t.Helper()
	reg := prometheus.NewRegistry()
	bucket := objstore.NewInMemBucket()
	idx, err := NewSimpleIndexer(calc, log.NewNopLogger(), bucket, reg)
	require.NoError(t, err)
	return idx, bucket, reg
}

func TestSimpleIndexer_Index(t *testing.T) {
	t.Run("should upload the index and record it in the metastore", func(t *testing.T) {
		calc := &stubCalculator{obj: createTestLogObject(t, 1)}
		idx, bucket, _ := newTestSimpleIndexer(t, calc)

		require.NoError(t, idx.Index(t.Context(), calc.obj, "objects/test"))

		require.Equal(t, 1, calc.calcCalls)
		require.Equal(t, 0, calc.resetCalls)
		// One index object plus at least one Table of Contents file.
		var indexes, tocs int
		for name := range bucket.Objects() {
			switch {
			case strings.HasPrefix(name, "indexes/"):
				indexes++
			default:
				tocs++
			}
		}
		require.Equal(t, 1, indexes)
		require.Positive(t, tocs)

		require.Equal(t, float64(1), testutil.ToFloat64(idx.metrics.attempts))
		require.Equal(t, float64(0), testutil.ToFloat64(idx.metrics.failures))
		require.Equal(t, float64(0), testutil.ToFloat64(idx.metrics.empty))
	})

	t.Run("should count a failed release without failing the index", func(t *testing.T) {
		calc := &stubCalculator{obj: createTestLogObject(t, 1), closeErr: errors.New("mock close error")}
		idx, bucket, _ := newTestSimpleIndexer(t, calc)

		// The index is uploaded and recorded by the time it is released, so a
		// cleanup failure must not discard that work.
		require.NoError(t, idx.Index(t.Context(), calc.obj, "objects/test"))

		require.NotEmpty(t, bucket.Objects())
		require.Equal(t, float64(0), testutil.ToFloat64(idx.metrics.failures))
		require.Equal(t, float64(1), testutil.ToFloat64(idx.metrics.releaseFailures))
	})

	t.Run("should count an empty index without writing anything", func(t *testing.T) {
		calc := &stubCalculator{flushErr: indexobj.ErrBuilderEmpty}
		idx, bucket, _ := newTestSimpleIndexer(t, calc)

		// An object that produces no index is not an error, but it is also not
		// discoverable by queries, so it must be counted.
		require.NoError(t, idx.Index(t.Context(), nil, "objects/test"))

		require.Empty(t, bucket.Objects())
		require.Equal(t, 0, calc.resetCalls)
		require.Equal(t, float64(1), testutil.ToFloat64(idx.metrics.attempts))
		require.Equal(t, float64(0), testutil.ToFloat64(idx.metrics.failures))
		require.Equal(t, float64(1), testutil.ToFloat64(idx.metrics.empty))
	})

	t.Run("should reset the calculator and count a failure when calculation fails", func(t *testing.T) {
		calc := &stubCalculator{calcErr: errors.New("mock error")}
		idx, bucket, _ := newTestSimpleIndexer(t, calc)

		err := idx.Index(t.Context(), nil, "objects/test")
		require.ErrorContains(t, err, "mock error")

		require.Empty(t, bucket.Objects())
		// Partial state must be discarded so the retry starts clean.
		require.Equal(t, 1, calc.resetCalls)
		require.Equal(t, float64(1), testutil.ToFloat64(idx.metrics.attempts))
		require.Equal(t, float64(1), testutil.ToFloat64(idx.metrics.failures))
	})

	t.Run("should reset the calculator and count a failure when the flush fails", func(t *testing.T) {
		calc := &stubCalculator{flushErr: errors.New("mock error")}
		idx, _, _ := newTestSimpleIndexer(t, calc)

		err := idx.Index(t.Context(), nil, "objects/test")
		require.ErrorContains(t, err, "mock error")

		require.Equal(t, 1, calc.resetCalls)
		require.Equal(t, float64(1), testutil.ToFloat64(idx.metrics.failures))
	})

	t.Run("should count every retry as an attempt", func(t *testing.T) {
		calc := &stubCalculator{calcErr: errors.New("mock error")}
		idx, _, _ := newTestSimpleIndexer(t, calc)

		for range 3 {
			require.Error(t, idx.Index(t.Context(), nil, "objects/test"))
		}
		require.Equal(t, float64(3), testutil.ToFloat64(idx.metrics.attempts))
		require.Equal(t, float64(3), testutil.ToFloat64(idx.metrics.failures))
	})
}

func TestSimpleIndexer_RegistersMetrics(t *testing.T) {
	calc := &stubCalculator{obj: createTestLogObject(t, 1)}
	idx, _, gatherer := newTestSimpleIndexer(t, calc)
	require.NoError(t, idx.Index(t.Context(), calc.obj, "objects/test"))

	require.NoError(t, testutil.GatherAndCompare(gatherer, strings.NewReader(`
	# HELP loki_dataobj_builder_index_attempts_total Total number of attempts to index a data object, including retries.
	# TYPE loki_dataobj_builder_index_attempts_total counter
	loki_dataobj_builder_index_attempts_total 1
	# HELP loki_dataobj_builder_index_empty_total Total number of data objects that produced no index and are therefore not discoverable by queries.
	# TYPE loki_dataobj_builder_index_empty_total counter
	loki_dataobj_builder_index_empty_total 0
	# HELP loki_dataobj_builder_index_failures_total Total number of failed attempts to index a data object. Failures are retried, so this also counts retries.
	# TYPE loki_dataobj_builder_index_failures_total counter
	loki_dataobj_builder_index_failures_total 0
	`),
		"loki_dataobj_builder_index_attempts_total",
		"loki_dataobj_builder_index_empty_total",
		"loki_dataobj_builder_index_failures_total",
	))

	// The Table of Contents writer's metrics must be exported too; they were
	// never registered before the writer became constructor-scoped.
	mfs, err := gatherer.Gather()
	require.NoError(t, err)
	var names []string
	for _, mf := range mfs {
		names = append(names, mf.GetName())
	}
	require.Contains(t, names, "loki_dataobj_builder_index_duration_seconds")
	require.Contains(t, names, "loki_metastore_toc_processing_seconds")
	require.Contains(t, names, "loki_dataobj_consumer_metastore_writes_total")
}
