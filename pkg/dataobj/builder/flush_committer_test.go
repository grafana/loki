package builder

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index"
	"github.com/grafana/loki/v3/pkg/logproto"
)

// newTestFlushBuilder returns a builder holding a single log line, ready to be
// handed to the flush committer.
func newTestFlushBuilder(t *testing.T, reg prometheus.Registerer) builder {
	t.Helper()
	return newTestFlushBuilderAt(t, reg, time.Now())
}

// newTestFlushBuilderAt returns a builder holding a single log line that was
// read from Kafka at recTime.
func newTestFlushBuilderAt(t *testing.T, reg prometheus.Registerer, recTime time.Time) builder {
	t.Helper()
	b := newTestBuilder(t, reg)
	require.NoError(t, b.Append("test", logproto.Stream{
		Labels:  `{foo="bar"}`,
		Entries: []logproto.Entry{{Timestamp: recTime, Line: "test"}},
	}, recTime))
	return b
}

func TestFlushCommitter(t *testing.T) {
	t.Run("should succeed", func(t *testing.T) {
		var (
			reg            = prometheus.NewRegistry()
			flusher        = &mockFlusher{obj: &dataobj.Object{}}
			indexer        = &mockIndexer{}
			tocWriter      = &mockTOCWriter{}
			committer      = &mockCommitter{}
			flushCommitter = newFlushCommitter(flusher, committer, indexer, tocWriter, 0, 1, log.NewNopLogger(), reg)
		)
		b := newTestFlushBuilder(t, reg)
		require.NoError(t, flushCommitter.Flush(t.Context(), []builder{b}, "test", 1))
		// The builder was flushed, the flushed object was indexed under the
		// path it was uploaded to, and the offset was committed.
		require.Equal(t, 1, flusher.flushes)
		require.Equal(t, []string{"object_001"}, indexer.paths)
		require.Equal(t, []*dataobj.Object{flusher.obj}, indexer.objs)
		// The built index is recorded in the ToC before the offset is committed.
		require.Equal(t, []string{"index/object_001"}, tocWriter.paths)
		require.Equal(t, []string{"test"}, tocWriter.tenants)
		require.Equal(t, []int64{1}, committer.offsets)
		// The object is released once indexing is done with it.
		require.Equal(t, 1, flusher.closer.closed)
		requireFlushResults(t, reg, map[string]uint64{resultOK: 1, resultError: 0, resultCancelled: 0})
	})

	for _, tc := range []struct {
		name string
		err  error
	}{
		{name: "should fail without retrying when the error wraps ErrUnprocessableObject", err: fmt.Errorf("%w: test", index.ErrUnprocessableObject)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var (
				reg            = prometheus.NewRegistry()
				flusher        = &mockFlusher{obj: &dataobj.Object{}}
				indexer        = &mockIndexer{errs: []error{fmt.Errorf("calculate object: %w", tc.err)}}
				tocWriter      = &mockTOCWriter{}
				committer      = &mockCommitter{}
				flushCommitter = newFlushCommitter(flusher, committer, indexer, tocWriter, 0, 1, log.NewNopLogger(), reg)
			)
			b := newTestFlushBuilder(t, reg)
			err := flushCommitter.Flush(t.Context(), []builder{b}, "test", 1)
			require.ErrorIs(t, err, tc.err)
			// A retry would have succeeded, as the mock only fails once.
			require.Len(t, indexer.paths, 1)
			require.Empty(t, tocWriter.paths)
			require.Empty(t, committer.offsets)
			requireFlushResults(t, reg, map[string]uint64{resultOK: 0, resultError: 1, resultCancelled: 0})
		})
	}

	t.Run("should fail when the flush fails", func(t *testing.T) {
		var (
			reg            = prometheus.NewRegistry()
			flusher        = &failureFlusher{}
			indexer        = &mockIndexer{}
			tocWriter      = &mockTOCWriter{}
			committer      = &mockCommitter{}
			flushCommitter = newFlushCommitter(flusher, committer, indexer, tocWriter, 0, 1, log.NewNopLogger(), reg)
		)
		b := newTestFlushBuilder(t, reg)
		err := flushCommitter.Flush(t.Context(), []builder{b}, "test", 1)
		require.EqualError(t, err, "failed to flush data object: mock error")
		// Nothing was flushed, so nothing should be indexed or committed.
		require.Empty(t, indexer.paths)
		require.Empty(t, tocWriter.paths)
		require.Empty(t, committer.offsets)
		requireFlushResults(t, reg, map[string]uint64{resultOK: 0, resultError: 1, resultCancelled: 0})
	})

	t.Run("should flush and index every builder but commit a single offset", func(t *testing.T) {
		var (
			reg            = prometheus.NewRegistry()
			flusher        = &mockFlusher{obj: &dataobj.Object{}}
			indexer        = &mockIndexer{}
			tocWriter      = &mockTOCWriter{}
			committer      = &mockCommitter{}
			flushCommitter = newFlushCommitter(flusher, committer, indexer, tocWriter, 0, 1, log.NewNopLogger(), reg)
		)
		// Build a slice of builders, mimicking a partition split across tenants and windows.
		var builders []builder
		for i := 0; i < 3; i++ {
			builders = append(builders, newTestFlushBuilder(t, prometheus.NewRegistry()))
		}

		require.NoError(t, flushCommitter.Flush(t.Context(), builders, "test", 7))
		// Each builder is flushed and indexed separately, keeping one index
		// object per tenant and window.
		require.Equal(t, 3, flusher.flushes)
		require.Equal(t, []string{"object_001", "object_002", "object_003"}, indexer.paths)
		require.Equal(t, []string{"index/object_001", "index/object_002", "index/object_003"}, tocWriter.paths)
		require.Equal(t, 3, flusher.closer.closed)
		// But only a single offset is committed for the whole flush.
		require.Equal(t, []int64{7}, committer.offsets)
	})

	t.Run("should retry indexing until it succeeds", func(t *testing.T) {
		// Run in a synctest bubble so the retry backoff does not actually sleep.
		synctest.Test(t, func(t *testing.T) {
			var (
				reg     = prometheus.NewRegistry()
				flusher = &mockFlusher{obj: &dataobj.Object{}}
				// Fail twice before succeeding. A transient index failure must
				// not fail the flush, otherwise the partition restarts and
				// replays an object that is already durable.
				indexer        = &mockIndexer{errs: []error{errors.New("boom"), errors.New("boom")}}
				tocWriter      = &mockTOCWriter{}
				committer      = &mockCommitter{}
				flushCommitter = newFlushCommitter(flusher, committer, indexer, tocWriter, 0, 1, log.NewNopLogger(), reg)
			)
			b := newTestFlushBuilder(t, reg)
			require.NoError(t, flushCommitter.Flush(t.Context(), []builder{b}, "test", 1))
			require.Len(t, indexer.paths, 3)
			// Only the successful attempt is recorded in the ToC.
			require.Equal(t, []string{"index/object_001"}, tocWriter.paths)
			require.Equal(t, []int64{1}, committer.offsets)
			require.Equal(t, 1, flusher.closer.closed)
		})
	})

	t.Run("should not fail the flush when releasing the object fails", func(t *testing.T) {
		var (
			reg = prometheus.NewRegistry()
			// The object is uploaded and indexed by the time it is released, so
			// failing to release it must not discard that work.
			flusher        = &mockFlusher{obj: &dataobj.Object{}, closer: countingCloser{err: errors.New("close error")}}
			indexer        = &mockIndexer{}
			tocWriter      = &mockTOCWriter{}
			committer      = &mockCommitter{}
			flushCommitter = newFlushCommitter(flusher, committer, indexer, tocWriter, 0, 1, log.NewNopLogger(), reg)
		)
		b := newTestFlushBuilder(t, reg)
		require.NoError(t, flushCommitter.Flush(t.Context(), []builder{b}, "test", 1))
		require.Equal(t, []string{"object_001"}, indexer.paths)
		require.Equal(t, []int64{1}, committer.offsets)
		require.Equal(t, 1, flusher.closer.closed)
	})

	t.Run("should flush nothing when the context is canceled before the flush", func(t *testing.T) {
		var (
			reg            = prometheus.NewRegistry()
			flusher        = &mockFlusher{obj: &dataobj.Object{}}
			indexer        = &mockIndexer{}
			tocWriter      = &mockTOCWriter{}
			committer      = &mockCommitter{}
			flushCommitter = newFlushCommitter(flusher, committer, indexer, tocWriter, 0, 1, log.NewNopLogger(), reg)
		)
		cancelCtx, cancel := context.WithCancel(t.Context())
		cancel()

		b := newTestFlushBuilder(t, reg)
		err := flushCommitter.Flush(cancelCtx, []builder{b}, "test", 1)
		require.ErrorIs(t, err, context.Canceled)
		require.Zero(t, flusher.flushes)
		require.Empty(t, committer.offsets)
		requireFlushResults(t, reg, map[string]uint64{resultOK: 0, resultError: 0, resultCancelled: 1})
	})

	t.Run("should not commit when indexing is canceled", func(t *testing.T) {
		cancelCtx, cancel := context.WithCancel(t.Context())
		defer cancel()

		var (
			reg            = prometheus.NewRegistry()
			flusher        = &mockFlusher{obj: &dataobj.Object{}, onFlush: cancel}
			indexer        = &mockIndexer{}
			tocWriter      = &mockTOCWriter{}
			committer      = &mockCommitter{}
			flushCommitter = newFlushCommitter(flusher, committer, indexer, tocWriter, 0, 1, log.NewNopLogger(), reg)
		)

		b := newTestFlushBuilder(t, reg)
		err := flushCommitter.Flush(cancelCtx, []builder{b}, "test", 1)
		// The cancellation must stay visible so the processor shuts down
		// gracefully instead of treating this as an unrecoverable flush.
		require.ErrorIs(t, err, context.Canceled)
		require.ErrorContains(t, err, "failed to index data object")
		require.Empty(t, tocWriter.paths)
		require.Empty(t, committer.offsets)
		// The object is still released on the way out.
		require.Equal(t, 1, flusher.closer.closed)
		requireFlushResults(t, reg, map[string]uint64{resultOK: 0, resultError: 0, resultCancelled: 1})
	})

	t.Run("should not commit when updating the ToC fails", func(t *testing.T) {
		var (
			reg            = prometheus.NewRegistry()
			flusher        = &mockFlusher{obj: &dataobj.Object{}}
			indexer        = &mockIndexer{}
			tocWriter      = &mockTOCWriter{err: context.Canceled}
			committer      = &mockCommitter{}
			flushCommitter = newFlushCommitter(flusher, committer, indexer, tocWriter, 0, 1, log.NewNopLogger(), reg)
		)
		b := newTestFlushBuilder(t, reg)
		err := flushCommitter.Flush(t.Context(), []builder{b}, "test", 1)
		require.ErrorIs(t, err, context.Canceled)
		require.ErrorContains(t, err, "failed to update metastore ToC")
		// An index missing from the ToC is not queryable, so committing would
		// lose the data it covers.
		require.Empty(t, committer.offsets)
		require.Equal(t, float64(0), testutil.ToFloat64(flushCommitter.e2eLagSeconds))
		require.Equal(t, 1, flusher.closer.closed)
		requireFlushResults(t, reg, map[string]uint64{resultOK: 0, resultError: 0, resultCancelled: 1})
	})

	t.Run("should fail when the commit is canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		var (
			reg     = prometheus.NewRegistry()
			flusher = &mockFlusher{obj: &dataobj.Object{}}
			indexer = &mockIndexer{}
			// Cancel once the ToC is updated so the commit never gets a
			// chance to run.
			tocWriter      = &mockTOCWriter{afterWrite: cancel}
			committer      = &mockCommitter{}
			flushCommitter = newFlushCommitter(flusher, committer, indexer, tocWriter, 0, 1, log.NewNopLogger(), reg)
		)
		b := newTestFlushBuilder(t, reg)
		err := flushCommitter.Flush(ctx, []builder{b}, "test", 1)
		// Reporting success here would make the processor believe the offset
		// is committed when it is not.
		require.ErrorIs(t, err, context.Canceled)
		require.ErrorContains(t, err, "failed to commit data object offset 1")
		require.Equal(t, []string{"index/object_001"}, tocWriter.paths)
		require.Empty(t, committer.offsets)
		requireFlushResults(t, reg, map[string]uint64{resultOK: 0, resultError: 0, resultCancelled: 1})
	})
}

func TestFlushCommitter_Concurrency(t *testing.T) {
	newBuilders := func(t *testing.T, n int) []builder {
		builders := make([]builder, n)
		for i := range builders {
			builders[i] = newTestFlushBuilder(t, prometheus.NewRegistry())
		}
		return builders
	}

	t.Run("should keep at most the concurrency limit of objects open at the same time", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			var (
				reg            = prometheus.NewRegistry()
				flusher        = &mockFlusher{obj: &dataobj.Object{}}
				indexer        = &mockIndexer{delay: time.Second}
				tocWriter      = &mockTOCWriter{}
				committer      = &mockCommitter{}
				flushCommitter = newFlushCommitter(flusher, committer, indexer, tocWriter, 0, 4, log.NewNopLogger(), reg)
			)
			start := time.Now()
			require.NoError(t, flushCommitter.Flush(t.Context(), newBuilders(t, 8), "test", 7))

			// Each index call sleeps 1s on the fake clock of the bubble, so 2s
			// means that two groups of 4 builders overlapped.
			require.Equal(t, 2*time.Second, time.Since(start))
			// An object stays open from its flush until the ToC records its
			// index, so this bounds the whole flush of a builder.
			require.Equal(t, 4, flusher.maxOpen)
			require.Equal(t, 8, flusher.flushes)
			require.ElementsMatch(t, []string{
				"object_001", "object_002", "object_003", "object_004",
				"object_005", "object_006", "object_007", "object_008",
			}, indexer.paths)
			require.Len(t, tocWriter.paths, 8)
			require.Equal(t, 8, flusher.closer.closed)
			require.Equal(t, []int64{7}, committer.offsets)
			requireFlushResults(t, reg, map[string]uint64{resultOK: 1, resultError: 0, resultCancelled: 0})
		})
	})

	t.Run("should commit once after every builder is in the ToC and released", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			var (
				reg       = prometheus.NewRegistry()
				flusher   = &mockFlusher{obj: &dataobj.Object{}}
				indexer   = &mockIndexer{delay: time.Second}
				tocWriter = &mockTOCWriter{}
				committer = &mockCommitter{}

				tocPathsAtCommit, closedAtCommit int
			)
			committer.onCommit = func() {
				tocPathsAtCommit = len(tocWriter.paths)
				closedAtCommit = flusher.closer.closed
			}
			flushCommitter := newFlushCommitter(flusher, committer, indexer, tocWriter, 0, 3, log.NewNopLogger(), reg)

			require.NoError(t, flushCommitter.Flush(t.Context(), newBuilders(t, 10), "test", 7))
			require.Equal(t, 10, tocPathsAtCommit)
			require.Equal(t, 10, closedAtCommit)
			require.Equal(t, []int64{7}, committer.offsets)
		})
	})

	t.Run("should cancel the other builders and not commit when one fails", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			var (
				reg     = prometheus.NewRegistry()
				flusher = &mockFlusher{obj: &dataobj.Object{}}
				// The second object fails once the other two in progress are
				// blocked in indexing, so the failure must cancel them.
				indexer = &blockingIndexer{
					failPath: "object_002",
					err:      fmt.Errorf("%w: test", index.ErrUnprocessableObject),
					blocked:  make(chan struct{}),
					others:   2,
				}
				tocWriter      = &mockTOCWriter{}
				committer      = &mockCommitter{}
				flushCommitter = newFlushCommitter(flusher, committer, indexer, tocWriter, 0, 3, log.NewNopLogger(), reg)
			)

			// synctest.Test fails the test if a flush goroutine is still
			// blocked after Flush returns.
			err := flushCommitter.Flush(t.Context(), newBuilders(t, 5), "test", 7)
			// Flush returns the first failure, not the cancellations that it
			// caused. The processor treats a cancellation as a shutdown, so a
			// cancellation would hide the unrecoverable flush.
			require.ErrorIs(t, err, index.ErrUnprocessableObject)
			require.NotErrorIs(t, err, context.Canceled)
			// The last two builders never started.
			require.Equal(t, 3, flusher.flushes)
			require.Equal(t, 3, flusher.closer.closed)
			require.Empty(t, tocWriter.paths)
			require.Empty(t, committer.offsets)
			requireFlushResults(t, reg, map[string]uint64{resultOK: 0, resultError: 1, resultCancelled: 0})
		})
	})

	t.Run("should return a cancellation and not commit when the flush is canceled with builders in progress", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			var (
				reg            = prometheus.NewRegistry()
				flusher        = &mockFlusher{obj: &dataobj.Object{}}
				indexer        = &blockingIndexer{blocked: make(chan struct{})}
				tocWriter      = &mockTOCWriter{}
				committer      = &mockCommitter{}
				flushCommitter = newFlushCommitter(flusher, committer, indexer, tocWriter, 0, 3, log.NewNopLogger(), reg)
			)
			go func() {
				for range 3 {
					<-indexer.blocked
				}
				cancel()
			}()

			err := flushCommitter.Flush(ctx, newBuilders(t, 5), "test", 7)
			require.ErrorIs(t, err, context.Canceled)
			require.Equal(t, 3, flusher.flushes)
			require.Equal(t, 3, flusher.closer.closed)
			require.Empty(t, tocWriter.paths)
			require.Empty(t, committer.offsets)
			requireFlushResults(t, reg, map[string]uint64{resultOK: 0, resultError: 0, resultCancelled: 1})
		})
	})

	// Not run under synctest: the real builders use zstd, which pools
	// goroutines and channels. Binding those to a bubble crashes later tests.
	t.Run("should flush real builders concurrently and record each index in the ToC", func(t *testing.T) {
		tenants := make([]benchTenant, 8)
		for i := range tenants {
			tenants[i] = benchTenant{name: fmt.Sprintf("tenant-%d", i), lines: 500, seed: uint64(i)}
		}
		flush := newBenchFlush(t, benchBucketLatency{}, 4)
		builders := make([]builder, len(tenants))
		for i, tenant := range tenants {
			builders[i] = tenant.fill(t, flush.factory)
		}

		require.NoError(t, flush.committer.Flush(t.Context(), builders, "test", 7))
		require.Equal(t, []int64{7}, flush.offsets.offsets)
		require.Len(t, flush.bucket.tocPaths(), len(tenants))
	})

	t.Run("should start the largest builder first and keep the order of builders of equal size", func(t *testing.T) {
		var (
			flusher        = &mockFlusher{obj: &dataobj.Object{}}
			committer      = &mockCommitter{}
			flushCommitter = newFlushCommitter(flusher, committer, &mockIndexer{}, &mockTOCWriter{}, 0, 1, log.NewNopLogger(), prometheus.NewRegistry())

			small     = &fixedSizeBuilder{size: 10}
			largeA    = &fixedSizeBuilder{size: 30}
			medium    = &fixedSizeBuilder{size: 20}
			largeB    = &fixedSizeBuilder{size: 30}
			given     = []builder{small, largeA, medium, largeB}
			givenCopy = slices.Clone(given)
		)

		require.NoError(t, flushCommitter.Flush(t.Context(), given, "test", 7))
		require.Equal(t, []builder{largeA, largeB, medium, small}, flusher.builders)
		require.Equal(t, givenCopy, given)
		require.Equal(t, []int64{7}, committer.offsets)
	})
}

// fixedSizeBuilder is a builder with a fixed estimated size and no records.
// It supports only the calls that the flush committer makes with a
// [mockFlusher]. Any other call panics on the nil embedded builder.
type fixedSizeBuilder struct {
	builder
	size int
}

func (b *fixedSizeBuilder) GetEstimatedSize() int { return b.size }

func (b *fixedSizeBuilder) GetEarliestRecordTime() time.Time { return time.Time{} }

// blockingIndexer fails to index the object at failPath. Every other call
// blocks until its context is done.
//
// The failing call waits until others calls block, so the failure happens
// while they are in progress. Besides the failing call, exactly others calls
// must arrive. Any later call blocks forever on the send to blocked.
//
// If failPath is empty, every call blocks, and the test must receive from
// blocked.
type blockingIndexer struct {
	failPath string
	err      error
	blocked  chan struct{}
	others   int
}

func (m *blockingIndexer) Index(ctx context.Context, _ *dataobj.Object, objPath string) (index.Result, error) {
	if m.failPath != "" && objPath == m.failPath {
		for range m.others {
			<-m.blocked
		}
		return index.Result{}, m.err
	}
	m.blocked <- struct{}{}
	<-ctx.Done()
	return index.Result{}, ctx.Err()
}

func TestFlushCommitter_E2ELag(t *testing.T) {
	t.Run("should report the lag of the oldest record across builders", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			var (
				reg            = prometheus.NewRegistry()
				flusher        = &mockFlusher{obj: &dataobj.Object{}}
				indexer        = &mockIndexer{}
				tocWriter      = &mockTOCWriter{}
				committer      = &mockCommitter{}
				flushCommitter = newFlushCommitter(flusher, committer, indexer, tocWriter, 0, 1, log.NewNopLogger(), reg)
				now            = time.Now()
			)
			builders := []builder{
				newTestFlushBuilderAt(t, reg, now.Add(-5*time.Minute)),
				newTestFlushBuilderAt(t, prometheus.NewRegistry(), now.Add(-10*time.Minute)),
				// A builder that never received a record has no record time and
				// must not be mistaken for one from the epoch.
				newTestBuilder(t, prometheus.NewRegistry()),
			}

			require.NoError(t, flushCommitter.Flush(t.Context(), builders, "test", 1))
			require.Equal(t, (10 * time.Minute).Seconds(), testutil.ToFloat64(flushCommitter.e2eLagSeconds))
		})
	})

	// Not run under synctest: this flushes a real builder, and the zstd encoder
	// it uses pools goroutines and channels. Binding those to a synctest bubble
	// crashes later tests with "receive on synctest channel from outside
	// bubble", so this one uses the real clock and an approximate assertion.
	t.Run("should read the record time before the builder is reset", func(t *testing.T) {
		var (
			reg = prometheus.NewRegistry()
			// This flusher really flushes, which resets the builder and clears
			// its earliest record time.
			flusher        = &flushingMockFlusher{}
			indexer        = &mockIndexer{}
			tocWriter      = &mockTOCWriter{}
			committer      = &mockCommitter{}
			flushCommitter = newFlushCommitter(flusher, committer, indexer, tocWriter, 0, 1, log.NewNopLogger(), reg)
		)
		b := newTestFlushBuilderAt(t, reg, time.Now().Add(-10*time.Minute))

		require.NoError(t, flushCommitter.Flush(t.Context(), []builder{b}, "test", 1))
		require.Equal(t, 1, flusher.flushes)
		// Reading the record time after the flush would report 0 here.
		require.InDelta(t, (10 * time.Minute).Seconds(), testutil.ToFloat64(flushCommitter.e2eLagSeconds), 60)
	})

	t.Run("should not report a lag when the flush fails", func(t *testing.T) {
		var (
			reg            = prometheus.NewRegistry()
			flusher        = &failureFlusher{}
			indexer        = &mockIndexer{}
			tocWriter      = &mockTOCWriter{}
			committer      = &mockCommitter{}
			flushCommitter = newFlushCommitter(flusher, committer, indexer, tocWriter, 0, 1, log.NewNopLogger(), reg)
		)
		b := newTestFlushBuilderAt(t, reg, time.Now().Add(-10*time.Minute))

		require.Error(t, flushCommitter.Flush(t.Context(), []builder{b}, "test", 1))
		// Nothing became queryable, so the lag must not be reported.
		require.Equal(t, float64(0), testutil.ToFloat64(flushCommitter.e2eLagSeconds))
	})
}

// requireFlushResults asserts how many flushes were observed for each result
// label of the flush-commit duration histogram.
func requireFlushResults(t *testing.T, reg prometheus.Gatherer, want map[string]uint64) {
	t.Helper()
	families, err := reg.Gather()
	require.NoError(t, err)

	got := make(map[string]uint64)
	for _, mf := range families {
		if mf.GetName() != "loki_dataobj_builder_flush_commit_duration_seconds" {
			continue
		}
		for _, m := range mf.GetMetric() {
			for _, l := range m.GetLabel() {
				if l.GetName() == "result" {
					got[l.GetValue()] = m.GetHistogram().GetSampleCount()
				}
			}
		}
	}
	require.Equal(t, want, got)
}

// flushingMockFlusher is a flusher that actually flushes the builder it is
// given, resetting the builder's buffered state (including its earliest record
// time) as a real flusher would.
type flushingMockFlusher struct {
	flushes int
	closer  countingCloser
}

func (m *flushingMockFlusher) Flush(_ context.Context, b builder, _ string) (*dataobj.Object, io.Closer, string, error) {
	m.flushes++
	obj, closer, err := b.Flush()
	if err != nil {
		return nil, nil, "", err
	}
	m.closer.inner = closer
	return obj, &m.closer, fmt.Sprintf("object_%03d", m.flushes), nil
}
