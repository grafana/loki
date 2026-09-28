package index

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore/multitenancy"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/pointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

// failingBucket fails every upload, which is the simplest way to make a real
// index build fail.
type failingBucket struct {
	objstore.Bucket
}

func (failingBucket) Upload(_ context.Context, _ string, _ io.Reader) error {
	return errors.New("mock upload error")
}

func newTestSimpleIndexer(t *testing.T, bucket objstore.Bucket) (*SimpleIndexer, prometheus.Gatherer) {
	t.Helper()
	reg := prometheus.NewRegistry()
	idx, err := newTestSimpleIndexerWithMetrics(t, bucket, reg)
	require.NoError(t, err)
	return idx, reg
}

func newTestSimpleIndexerWithMetrics(t *testing.T, bucket objstore.Bucket, reg prometheus.Registerer) (*SimpleIndexer, error) {
	t.Helper()
	metrics := NewIndexerMetrics(reg)

	return NewSimpleIndexer(testCalculatorConfig, nil, log.NewNopLogger(), bucket,
		metrics, indexobj.NewBuilderMetrics(reg), NewCalculatorMetrics(reg))
}

func TestSimpleIndexer_Index(t *testing.T) {
	t.Run("should upload an index describing the data object", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		idx, _ := newTestSimpleIndexer(t, bucket)

		const objPath = "objects/test"
		res, err := idx.Index(t.Context(), createTestLogObject(t, 2), objPath)
		require.NoError(t, err)

		require.Contains(t, bucket.Objects(), res.Path)
		require.Len(t, bucket.Objects(), 1, "one index object per data object")

		// The ranges are what the caller records in the Table of Contents, so
		// they must describe the data object: both of its tenants, the span of
		// the fixture's entries, and the size of the index just uploaded.
		require.Len(t, res.TimeRanges, 2)
		require.ElementsMatch(t, []string{"tenant-0", "tenant-1"}, tenantsOf(res.TimeRanges))
		for _, tr := range res.TimeRanges {
			require.Equal(t, uint64(len(bucket.Objects()[res.Path])), tr.FileSize)
			require.Equal(t, time.Unix(10, 0).UTC(), tr.MinTime)
			require.Equal(t, time.Unix(25, 0).UTC(), tr.MaxTime)
			require.Positive(t, tr.UncompressedLogsSize)
		}

		// Read the uploaded bytes back: they must decode as an index object
		// holding a streams and a pointers section for each tenant.
		idxObj, err := dataobj.FromBucket(t.Context(), bucket, res.Path, 0)
		require.NoError(t, err)
		require.Equal(t, 2, idxObj.Sections().Count(streams.CheckSection))
		require.GreaterOrEqual(t, idxObj.Sections().Count(pointers.CheckSection), 2)

		// Every stream of the data object is recorded, per tenant.
		for _, tenant := range []string{"tenant-0", "tenant-1"} {
			require.ElementsMatch(t, []string{
				`{app="bar", cluster="test", env="dev"}`,
				`{app="foo", cluster="test", env="prod"}`,
			}, indexedStreams(t, idxObj, tenant), "tenant %s", tenant)
		}

		// The index points back at the data object it was built from.
		require.Equal(t, []string{objPath}, indexedPointerPaths(t, idxObj))
	})

	t.Run("should index every tenant in the data object", func(t *testing.T) {
		idx, _ := newTestSimpleIndexer(t, objstore.NewInMemBucket())

		res, err := idx.Index(t.Context(), createTestLogObject(t, 3), "objects/test")
		require.NoError(t, err)
		require.Len(t, res.TimeRanges, 3)
	})

	t.Run("should propagate an upload failure", func(t *testing.T) {
		idx, _ := newTestSimpleIndexer(t, failingBucket{objstore.NewInMemBucket()})

		res, err := idx.Index(t.Context(), createTestLogObject(t, 1), "objects/test")
		require.ErrorContains(t, err, "mock upload error")
		require.Empty(t, res.Path)
	})

	t.Run("should count an attempt against its outcome", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		idx, reg := newTestSimpleIndexer(t, bucket)

		// Both outcomes are reported from the start, so a rate over failures
		// reads as zero rather than going missing.
		require.Equal(t, uint64(0), indexAttempts(t, reg, resultOK))
		require.Equal(t, uint64(0), indexAttempts(t, reg, resultError))

		obj := createTestLogObject(t, 1)
		_, err := idx.Index(t.Context(), obj, "objects/ok")
		require.NoError(t, err)

		// Reuse the indexer with a bucket that fails, so both outcomes are
		// observed by the same histogram.
		idx.idxBucket = failingBucket{bucket}
		for range 2 {
			_, err = idx.Index(t.Context(), obj, "objects/err")
			require.Error(t, err)
		}

		require.Equal(t, uint64(1), indexAttempts(t, reg, resultOK))
		require.Equal(t, uint64(2), indexAttempts(t, reg, resultError))
	})

	t.Run("should count a cancelled attempt apart from failures", func(t *testing.T) {
		idx, reg := newTestSimpleIndexer(t, objstore.NewInMemBucket())

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		_, err := idx.Index(ctx, createTestLogObject(t, 1), "objects/test")
		require.ErrorIs(t, err, context.Canceled)

		require.Equal(t, uint64(1), indexAttempts(t, reg, resultCancelled))
		require.Equal(t, uint64(0), indexAttempts(t, reg, resultError))
		require.Equal(t, uint64(0), indexAttempts(t, reg, resultOK))
	})
}

func TestIndexerMetrics_ObserveIndex(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{name: "success", err: nil, want: resultOK},
		{name: "failure", err: errors.New("boom"), want: resultError},
		{name: "cancelled", err: context.Canceled, want: resultCancelled},
		{name: "wrapped cancellation", err: fmt.Errorf("upload: %w", context.Canceled), want: resultCancelled},
		{name: "deadline exceeded", err: context.DeadlineExceeded, want: resultError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reg := prometheus.NewRegistry()
			metrics := NewIndexerMetrics(reg)

			metrics.observeIndex(time.Second, tc.err)

			require.Equal(t, uint64(1), indexAttempts(t, reg, tc.want))
		})
	}
}

// indexAttempts reports the number of index attempts recorded for an outcome,
// which is the sample count of that outcome's duration histogram.
func indexAttempts(t *testing.T, g prometheus.Gatherer, result string) uint64 {
	t.Helper()

	const name = "loki_dataobj_builder_index_duration_seconds"
	mfs, err := g.Gather()
	require.NoError(t, err)

	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		for _, m := range mf.GetMetric() {
			for _, l := range m.GetLabel() {
				if l.GetName() == "result" && l.GetValue() == result {
					return m.GetHistogram().GetSampleCount()
				}
			}
		}
		t.Fatalf("metric %q has no series with result=%q", name, result)
	}
	t.Fatalf("metric %q was not gathered", name)
	return 0
}

// TestSimpleIndexer_IndexConcurrently indexes several data objects at once.
// Sharing one calculator would merge their indexes into a single object, so
// each call building its own is what keeps the results independent. Run with
// -race to also cover the shared metrics.
func TestSimpleIndexer_IndexConcurrently(t *testing.T) {
	bucket := objstore.NewInMemBucket()
	idx, _ := newTestSimpleIndexer(t, bucket)

	const objects = 8

	// Build the objects up front: the fixtures are not concurrency-safe.
	objs := make([]*dataobj.Object, objects)
	for i := range objs {
		objs[i] = createTestLogObject(t, i+1)
	}

	var wg sync.WaitGroup
	results := make([]Result, objects)
	errs := make([]error, objects)
	for i := range objects {
		wg.Go(func() {
			results[i], errs[i] = idx.Index(t.Context(), objs[i], fmt.Sprintf("objects/test-%d", i))
		})
	}
	wg.Wait()

	require.NoError(t, errors.Join(errs...))

	// Each data object gets its own index, covering only its own tenants.
	paths := make(map[string]struct{}, objects)
	for i, res := range results {
		require.NotEmpty(t, res.Path)
		require.Len(t, res.TimeRanges, i+1)
		paths[res.Path] = struct{}{}
	}
	require.Len(t, paths, objects)
	require.Len(t, bucket.Objects(), objects)
}

// TestSimpleIndexer_SharesDownstreamMetrics covers the reason the calculator
// and builder metrics are passed in: they are created per data object, so they
// report through the sets the caller registered rather than registering their
// own, which would fail on the second object.
func TestSimpleIndexer_SharesDownstreamMetrics(t *testing.T) {
	idx, reg := newTestSimpleIndexer(t, objstore.NewInMemBucket())

	obj := createTestLogObject(t, 1)
	for i := range 2 {
		_, err := idx.Index(t.Context(), obj, fmt.Sprintf("objects/test-%d", i))
		require.NoError(t, err)
	}

	require.NoError(t, testutil.GatherAndCompare(reg, strings.NewReader(`
	# HELP loki_indexobj_flush_total Total number of flushes.
	# TYPE loki_indexobj_flush_total counter
	loki_indexobj_flush_total 2
	`), "loki_indexobj_flush_total"))

	n, err := testutil.GatherAndCount(reg, "loki_index_calculator_step_duration_seconds")
	require.NoError(t, err)
	require.Positive(t, n)
}

// TestSimpleIndexer_MultipleInstances covers the reason the metrics are created
// by the caller: several indexers must be able to report to one registry, which
// each of them registering its own metrics would prevent.
func TestSimpleIndexer_MultipleInstances(t *testing.T) {
	reg := prometheus.NewRegistry()
	bucket := objstore.NewInMemBucket()

	metrics := NewIndexerMetrics(reg)
	builderMetrics := indexobj.NewBuilderMetrics(reg)
	calculatorMetrics := NewCalculatorMetrics(reg)

	obj := createTestLogObject(t, 1)
	for i := range 2 {
		idx, err := NewSimpleIndexer(testCalculatorConfig, nil, log.NewNopLogger(), bucket,
			metrics, builderMetrics, calculatorMetrics)
		require.NoError(t, err)

		_, err = idx.Index(t.Context(), obj, fmt.Sprintf("objects/test-%d", i))
		require.NoError(t, err)
	}

	// Both indexers report through the one registered set.
	require.Equal(t, uint64(2), indexAttempts(t, reg, resultOK))
}

// TestSimpleIndexer_RejectsInvalidConfig checks the config is validated up
// front rather than on the first data object.
func TestSimpleIndexer_RejectsInvalidConfig(t *testing.T) {
	reg := prometheus.NewRegistry()
	metrics := NewIndexerMetrics(reg)

	_, err := NewSimpleIndexer(logsobj.BuilderBaseConfig{}, nil, log.NewNopLogger(),
		objstore.NewInMemBucket(), metrics, indexobj.NewBuilderMetrics(reg), NewCalculatorMetrics(reg))
	require.Error(t, err)
}

func tenantsOf(ranges []multitenancy.TimeRange) []string {
	tenants := make([]string, 0, len(ranges))
	for _, tr := range ranges {
		tenants = append(tenants, tr.Tenant)
	}
	return tenants
}

// indexedStreams returns the label sets the index object records for tenant.
func indexedStreams(t *testing.T, obj *dataobj.Object, tenant string) []string {
	t.Helper()

	var got []string
	for _, section := range obj.Sections().Filter(streams.CheckSection) {
		if section.Tenant != tenant {
			continue
		}
		sec, err := streams.Open(t.Context(), section)
		require.NoError(t, err)

		reader := streams.NewRowReader(sec)
		t.Cleanup(func() { _ = reader.Close() })
		require.NoError(t, reader.Open(t.Context()))

		buf := make([]streams.Stream, 128)
		for {
			n, err := reader.Read(t.Context(), buf)
			if !errors.Is(err, io.EOF) {
				require.NoError(t, err)
			}
			if n == 0 && errors.Is(err, io.EOF) {
				break
			}
			for _, stream := range buf[:n] {
				got = append(got, stream.Labels.String())
			}
		}
	}
	return got
}

// indexedPointerPaths returns the distinct data object paths the index points at.
func indexedPointerPaths(t *testing.T, obj *dataobj.Object) []string {
	t.Helper()

	seen := make(map[string]struct{})
	for _, section := range obj.Sections().Filter(pointers.CheckSection) {
		sec, err := pointers.Open(t.Context(), section)
		require.NoError(t, err)

		reader := pointers.NewRowReader(sec)
		t.Cleanup(func() { _ = reader.Close() })
		require.NoError(t, reader.Open(t.Context()))

		buf := make([]pointers.SectionPointer, 128)
		for {
			n, err := reader.Read(t.Context(), buf)
			if !errors.Is(err, io.EOF) {
				require.NoError(t, err)
			}
			if n == 0 && errors.Is(err, io.EOF) {
				break
			}
			for _, pointer := range buf[:n] {
				seen[pointer.Path] = struct{}{}
			}
		}
	}

	paths := make([]string, 0, len(seen))
	for path := range seen {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}
