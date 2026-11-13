package builder

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
)

var (
	testWindow1 = time.Date(2026, time.April, 17, 0, 0, 0, 0, time.UTC)
	testWindow2 = testWindow1.Add(metastore.MetastoreWindowSize)
)

// writeTestWatermark makes the ToC of the tenant and window in bucket hold the
// watermark of "test-topic" partition 0.
func writeTestWatermark(t *testing.T, bucket objstore.Bucket, tenant string, window time.Time, offset int64) {
	t.Helper()
	reg := prometheus.NewRegistry()
	writer := metastore.NewTableOfContentsWriter(bucket, metastore.DefaultTocWriterBackoffConfig, metastore.DefaultTocBuilderConfig, log.NewNopLogger(), metastore.NewTocWriterMetrics(reg), indexobj.NewBuilderMetrics(reg))
	entry := metastore.TableOfContentsEntry{
		Path:      fmt.Sprintf("index/%s-%d", tenant, offset),
		StartTime: window.Add(time.Minute),
		EndTime:   window.Add(time.Minute),
	}
	current, err := metastore.ReadWatermark(t.Context(), bucket, tenant, window, "test-topic", 0)
	require.NoError(t, err)
	require.NoError(t, writer.WriteEntryWithWatermark(t.Context(), tenant, entry, metastore.WatermarkUpdate{Topic: "test-topic", Partition: 0, EndOffset: offset, Expected: current}))
}

func ptr[T any](v T) *T { return &v }

// failingBucket fails every read.
type failingBucket struct {
	objstore.Bucket
	err error
}

func (b failingBucket) Attributes(context.Context, string) (objstore.ObjectAttributes, error) {
	return objstore.ObjectAttributes{}, b.err
}

func (b failingBucket) Get(context.Context, string) (io.ReadCloser, error) { return nil, b.err }

func (b failingBucket) GetRange(context.Context, string, int64, int64) (io.ReadCloser, error) {
	return nil, b.err
}

func newBucketWatermarks(bucket objstore.BucketReader) *watermarkCache {
	return newWatermarkCache("test-topic", 0, bucket, NewWatermarkMetrics(prometheus.NewRegistry()))
}

func lookups(w *watermarkCache, result string) float64 {
	return testutil.ToFloat64(w.metrics.cacheLookups.WithLabelValues(result))
}

func readSamples(t *testing.T, w *watermarkCache, result string) uint64 {
	t.Helper()
	var m dto.Metric
	require.NoError(t, w.metrics.readDuration.WithLabelValues(result).(prometheus.Metric).Write(&m))
	return m.GetHistogram().GetSampleCount()
}

func TestWatermarkCache_Get(t *testing.T) {
	scope := builderScope{tenant: "a", window: testWindow1}

	t.Run("returns nil and reads once when the ToC has no row", func(t *testing.T) {
		w := newBucketWatermarks(objstore.NewInMemBucket())

		for range 3 {
			value, err := w.get(t.Context(), scope)
			require.NoError(t, err)
			require.Nil(t, value)
		}
		require.Equal(t, uint64(1), readSamples(t, w, resultOK))
	})

	t.Run("returns the watermark of the ToC, reads each scope once and counts the cache lookups", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		writeTestWatermark(t, bucket, "a", testWindow1, 10)
		w := newBucketWatermarks(bucket)

		for range 3 {
			value, err := w.get(t.Context(), scope)
			require.NoError(t, err)
			require.Equal(t, int64(10), *value)
		}

		require.Equal(t, uint64(1), readSamples(t, w, resultOK))
		require.Equal(t, 1.0, lookups(w, lookupMiss))
		require.Equal(t, 2.0, lookups(w, lookupHit))
		require.Equal(t, 1.0, testutil.ToFloat64(w.metrics.cacheEntries))
	})

	t.Run("reads the tenants and windows separately", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		writeTestWatermark(t, bucket, "a", testWindow1, 10)
		w := newBucketWatermarks(bucket)

		for _, tc := range []struct {
			scope builderScope
			want  *int64
		}{
			{scope: scope, want: ptr(int64(10))},
			{scope: builderScope{tenant: "a", window: testWindow2}},
			{scope: builderScope{tenant: "b", window: testWindow1}},
		} {
			value, err := w.get(t.Context(), tc.scope)
			require.NoError(t, err)
			require.Equal(t, tc.want, value, tc.scope.String())
		}
	})

	t.Run("returns the read error, counts it, and reads again on the next call", func(t *testing.T) {
		readErr := errors.New("bucket down")
		w := newBucketWatermarks(failingBucket{Bucket: objstore.NewInMemBucket(), err: readErr})

		for range 2 {
			value, err := w.get(t.Context(), scope)
			require.ErrorIs(t, err, readErr)
			require.Nil(t, value)
		}

		require.Equal(t, uint64(2), readSamples(t, w, resultError))
		require.Equal(t, 2.0, lookups(w, lookupMiss))
		require.Equal(t, 0.0, testutil.ToFloat64(w.metrics.cacheEntries))
	})

	t.Run("evicts the least recently used entry when the cache is full", func(t *testing.T) {
		w := newBucketWatermarks(objstore.NewInMemBucket())
		w.entries = newWatermarkEntries(2)
		scopeOf := func(tenant string) builderScope { return builderScope{tenant: tenant, window: testWindow1} }

		for _, tenant := range []string{"a", "b", "a", "c"} {
			_, err := w.get(t.Context(), scopeOf(tenant))
			require.NoError(t, err)
		}

		require.True(t, w.entries.Contains(scopeOf("a")))
		require.False(t, w.entries.Contains(scopeOf("b")))
		require.True(t, w.entries.Contains(scopeOf("c")))
		require.Equal(t, 2.0, testutil.ToFloat64(w.metrics.cacheEntries))
	})
}

func TestWatermarkCache_Set(t *testing.T) {
	scope := builderScope{tenant: "a", window: testWindow1}

	t.Run("serves the offset without a read of the ToC", func(t *testing.T) {
		w := newBucketWatermarks(objstore.NewInMemBucket())

		w.set(scope, 7)

		value, err := w.get(t.Context(), scope)
		require.NoError(t, err)
		require.Equal(t, int64(7), *value)
		require.Equal(t, uint64(0), readSamples(t, w, resultOK))
	})

	t.Run("keeps the higher value when it gets a lower offset", func(t *testing.T) {
		w := newBucketWatermarks(objstore.NewInMemBucket())

		w.set(scope, 9)
		w.set(scope, 5)

		value, err := w.get(t.Context(), scope)
		require.NoError(t, err)
		require.Equal(t, int64(9), *value)
	})

	t.Run("raises the value that the cache read, or the missing row", func(t *testing.T) {
		withRow := objstore.NewInMemBucket()
		writeTestWatermark(t, withRow, "a", testWindow1, 10)
		for name, bucket := range map[string]objstore.Bucket{"row": withRow, "no row": objstore.NewInMemBucket()} {
			w := newBucketWatermarks(bucket)
			_, err := w.get(t.Context(), scope)
			require.NoError(t, err, name)

			w.set(scope, 20)

			value, err := w.get(t.Context(), scope)
			require.NoError(t, err, name)
			require.Equal(t, int64(20), *value, name)
		}
	})
}

func TestProcessor_Watermarks(t *testing.T) {
	// newTestProcessor returns a processor on a multi-builder whose ToCs hold
	// the given watermarks.
	newTestProcessor := func(t *testing.T, watermarks map[builderScope]int64) (*processor, *mockMultiBuilder) {
		bucket := objstore.NewInMemBucket()
		for scope, offset := range watermarks {
			writeTestWatermark(t, bucket, scope.tenant, scope.window, offset)
		}
		m := &mockMultiBuilder{MultiObjectBuilder: NewMultiObjectBuilder(newTestBuilderFactory(), int(testBuilderCfg.TargetObjectSize), newBucketWatermarks(bucket))}
		return newProcessor(m, nil, &mockFlushCommitter{}, time.Hour, time.Hour, log.NewNopLogger(), prometheus.NewRegistry()), m
	}
	scopeA1 := builderScope{tenant: "a", window: testWindow1}
	scopeA2 := builderScope{tenant: "a", window: testWindow2}

	t.Run("drops a record at or below the watermark, keeps the next one, and moves lastOffset", func(t *testing.T) {
		proc, m := newTestProcessor(t, map[builderScope]int64{scopeA1: 10})
		process := func(offset int64) {
			rec := newTestRecord(t, "a", testWindow1.Add(time.Minute))
			rec.Offset = offset
			require.NoError(t, proc.processRecord(t.Context(), rec))
		}

		process(10)
		require.Empty(t, m.getBuilders())
		require.Equal(t, int64(10), proc.lastOffset)
		require.False(t, proc.firstRecordProcessedAt.IsZero())
		require.False(t, proc.lastRecordProcessedAt.IsZero())
		require.Equal(t, 1.0, testutil.ToFloat64(proc.metrics.droppedRecords))

		process(11)
		require.Len(t, m.getBuilders(), 1)
		require.Equal(t, int64(11), proc.lastOffset)
		require.Equal(t, 1.0, testutil.ToFloat64(proc.metrics.droppedRecords))
	})

	t.Run("commits the offset of dropped records when the builder stays empty for the maximum age", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			bucket := objstore.NewInMemBucket()
			writeTestWatermark(t, bucket, "a", testWindow1, 20)
			flushCommitter := &mockFlushCommitter{}
			m := &mockMultiBuilder{MultiObjectBuilder: NewMultiObjectBuilder(newTestBuilderFactory(), int(testBuilderCfg.TargetObjectSize), newBucketWatermarks(bucket))}
			proc := newProcessor(m, nil, flushCommitter, 5*time.Minute, 30*time.Minute, log.NewNopLogger(), prometheus.NewRegistry())
			startTestFlushLoop(t.Context(), t, proc)
			process := func(offset int64) {
				rec := newTestRecord(t, "a", testWindow1.Add(time.Minute))
				rec.Offset = offset
				require.NoError(t, proc.processRecord(t.Context(), rec))
			}

			process(0)
			time.Sleep(29 * time.Minute)
			process(1)
			synctest.Wait()
			require.Equal(t, 0, flushCommitter.flushes)

			time.Sleep(2 * time.Minute)
			process(2)
			synctest.Wait()

			require.Equal(t, 1, flushCommitter.flushes)
			require.Equal(t, 0, flushCommitter.lastBuilderCount)
			require.Equal(t, int64(1), flushCommitter.lastOffset)
			require.Equal(t, flushReasonMaxAge, flushCommitter.lastReason)
		})
	})

	t.Run("commits the offset of dropped records when Run sees the idle timeout", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			bucket := objstore.NewInMemBucket()
			writeTestWatermark(t, bucket, "a", testWindow1, 20)
			flushCommitter := &mockFlushCommitter{}
			records := make(chan *kgo.Record)
			m := &mockMultiBuilder{MultiObjectBuilder: NewMultiObjectBuilder(newTestBuilderFactory(), int(testBuilderCfg.TargetObjectSize), newBucketWatermarks(bucket))}
			proc := newProcessor(m, records, flushCommitter, 5*time.Minute, 30*time.Minute, log.NewNopLogger(), prometheus.NewRegistry())
			ctx, cancel := context.WithCancel(t.Context())
			ran := make(chan error, 1)
			go func() { ran <- proc.Run(ctx) }()

			for offset := range int64(5) {
				rec := newTestRecord(t, "a", testWindow1.Add(time.Minute))
				rec.Offset = offset
				records <- rec
			}
			time.Sleep(11 * time.Minute)
			synctest.Wait()
			cancel()

			require.NoError(t, <-ran)
			require.Equal(t, 1, flushCommitter.flushes)
			require.Equal(t, 0, flushCommitter.lastBuilderCount)
			require.Equal(t, int64(4), flushCommitter.lastOffset)
		})
	})

	t.Run("drops only the windows of a multi-window record that the ToC holds", func(t *testing.T) {
		proc, m := newTestProcessor(t, map[builderScope]int64{scopeA1: 10, scopeA2: 9})

		rec := newTestMultiWindowRecord(t, "a", []time.Time{testWindow1, testWindow2})
		rec.Offset = 10
		require.NoError(t, proc.processRecord(t.Context(), rec))

		builders := m.getBuilders()
		require.Len(t, builders, 1)
		ranges := builders[0].TimeRanges()
		require.Len(t, ranges, 1)
		require.Equal(t, testWindow2, ranges[0].MinTime.UTC().Truncate(metastore.MetastoreWindowSize))
		require.Equal(t, 1.0, testutil.ToFloat64(proc.metrics.droppedRecords))
		require.Equal(t, int64(10), proc.lastOffset)
	})

	t.Run("fails the flush when another process raised the ToC watermark after the first record", func(t *testing.T) {
		stack := newRealStack(t)
		m := &fixedCountMultiBuilder{
			MultiObjectBuilder: NewMultiObjectBuilder(stack.factory, stack.targetSize, stack.watermarks),
			fullAfter:          2,
		}
		proc := newProcessor(m, nil, stack.flushCommitter, time.Hour, 0, log.NewNopLogger(), prometheus.NewRegistry())
		stop := startTestFlushLoop(t.Context(), t, proc)

		for offset := range int64(2) {
			rec := newTestRecord(t, "a", testWindow1.Add(time.Minute))
			rec.Offset = offset
			require.NoError(t, proc.processRecord(t.Context(), rec))
		}
		writeTestWatermark(t, objstore.NewPrefixedBucket(stack.bucket, "index/"), "a", testWindow1, 50)
		trailer := newTestRecord(t, "a", testWindow1.Add(time.Minute))
		trailer.Offset = 2
		require.NoError(t, proc.processRecord(t.Context(), trailer))

		err := stop()
		require.ErrorIs(t, err, metastore.ErrWatermarkMismatch)
		require.Empty(t, stack.committer.offsets)
		held, err := metastore.ReadWatermark(t.Context(), objstore.NewPrefixedBucket(stack.bucket, "index/"), "a", testWindow1, "test-topic", 0)
		require.NoError(t, err)
		require.Equal(t, int64(50), *held)
	})

	t.Run("stops without an error when the context is canceled during a watermark read", func(t *testing.T) {
		bucket := &blockingBucket{Bucket: objstore.NewInMemBucket(), started: make(chan struct{}), release: make(chan struct{})}
		flushCommitter := &mockFlushCommitter{}
		records := make(chan *kgo.Record, 1)
		m := &mockMultiBuilder{MultiObjectBuilder: NewMultiObjectBuilder(newTestBuilderFactory(), int(testBuilderCfg.TargetObjectSize), newBucketWatermarks(bucket))}
		proc := newProcessor(m, records, flushCommitter, time.Hour, time.Hour, log.NewNopLogger(), prometheus.NewRegistry())
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		ran := make(chan error, 1)
		go func() { ran <- proc.Run(ctx) }()

		records <- newTestRecord(t, "a", testWindow1.Add(time.Minute))
		<-bucket.started
		cancel()

		require.NoError(t, <-ran)
		require.Equal(t, 0, flushCommitter.flushes)
		require.Equal(t, 0.0, testutil.ToFloat64(proc.metrics.recordFailures))
	})

	t.Run("fails the run when it cannot read a watermark, and commits nothing past the record", func(t *testing.T) {
		readErr := errors.New("bucket down")
		w := newBucketWatermarks(failingBucket{Bucket: objstore.NewInMemBucket(), err: readErr})
		flushCommitter := &mockFlushCommitter{}
		records := make(chan *kgo.Record, 2)
		m := &mockMultiBuilder{MultiObjectBuilder: NewMultiObjectBuilder(newTestBuilderFactory(), int(testBuilderCfg.TargetObjectSize), w)}
		proc := newProcessor(m, records, flushCommitter, time.Hour, time.Hour, log.NewNopLogger(), prometheus.NewRegistry())

		for offset := int64(4); offset < 6; offset++ {
			rec := newTestRecord(t, "a", testWindow1.Add(time.Minute))
			rec.Offset = offset
			records <- rec
		}

		err := proc.Run(t.Context())
		require.ErrorIs(t, err, readErr)
		require.ErrorIs(t, err, errUnrecoverable)
		require.Equal(t, 0, flushCommitter.flushes)
		require.Equal(t, 1.0, testutil.ToFloat64(proc.metrics.records))
		require.Equal(t, 0.0, testutil.ToFloat64(proc.metrics.recordFailures))
	})
}

func TestFlushCommitter_Watermarks(t *testing.T) {
	newCommitter := func(tocWriter *mockTOCWriter, committer *mockCommitter, reg *prometheus.Registry) *flushCommitterImpl {
		return newFlushCommitter(&mockFlusher{obj: &dataobj.Object{}}, committer, &mockIndexer{}, tocWriter, "test-topic", 3, 1, log.NewNopLogger(), reg)
	}
	newBuilderWithWatermark := func(t *testing.T, reg prometheus.Registerer, watermark *int64) builder {
		return WatermarkedBuilder{logsBuilder: newTestFlushBuilder(t, reg), initialWatermark: watermark}
	}

	t.Run("commits the offset and writes no ToC when it has no builders", func(t *testing.T) {
		var (
			reg       = prometheus.NewRegistry()
			tocWriter = &mockTOCWriter{}
			committer = &mockCommitter{}
		)
		fc := newCommitter(tocWriter, committer, reg)

		require.NoError(t, fc.Flush(t.Context(), nil, flushReasonIdle, 7))

		require.Equal(t, []int64{7}, committer.offsets)
		require.Empty(t, tocWriter.updates)
		require.Empty(t, tocWriter.paths)
	})

	t.Run("expects the initial watermark of each builder", func(t *testing.T) {
		var (
			reg       = prometheus.NewRegistry()
			tocWriter = &mockTOCWriter{}
			five      = int64(5)
		)
		fc := newCommitter(tocWriter, &mockCommitter{}, reg)

		require.NoError(t, fc.Flush(t.Context(), []builder{newBuilderWithWatermark(t, reg, &five)}, "test", 12))

		require.Equal(t, []metastore.WatermarkUpdate{
			{Topic: "test-topic", Partition: 3, EndOffset: 12, Expected: &five},
		}, tocWriter.updates)
	})
}

// blockingBucket blocks every read until release is closed or ctx is done.
// It closes started when the first read begins.
type blockingBucket struct {
	objstore.Bucket
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (b *blockingBucket) wait(ctx context.Context) error {
	b.once.Do(func() { close(b.started) })
	select {
	case <-b.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (b *blockingBucket) Attributes(ctx context.Context, name string) (objstore.ObjectAttributes, error) {
	if err := b.wait(ctx); err != nil {
		return objstore.ObjectAttributes{}, err
	}
	return b.Bucket.Attributes(ctx, name)
}

func (b *blockingBucket) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	if err := b.wait(ctx); err != nil {
		return nil, err
	}
	return b.Bucket.Get(ctx, name)
}

func (b *blockingBucket) GetRange(ctx context.Context, name string, off, length int64) (io.ReadCloser, error) {
	if err := b.wait(ctx); err != nil {
		return nil, err
	}
	return b.Bucket.GetRange(ctx, name, off, length)
}

func TestWatermarkCache_Concurrency(t *testing.T) {
	scope := builderScope{tenant: "a", window: testWindow1}

	t.Run("keeps the offset that landed while the read of the ToC ran", func(t *testing.T) {
		bucket := &blockingBucket{Bucket: objstore.NewInMemBucket(), started: make(chan struct{}), release: make(chan struct{})}
		w := newBucketWatermarks(bucket)

		type result struct {
			value *int64
			err   error
		}
		done := make(chan result, 1)
		go func() {
			value, err := w.get(t.Context(), scope)
			done <- result{value, err}
		}()
		<-bucket.started
		w.set(scope, 50)
		close(bucket.release)

		res := <-done
		require.NoError(t, res.err)
		require.Equal(t, int64(50), *res.value)
	})
}
