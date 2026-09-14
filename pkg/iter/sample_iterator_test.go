package iter

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"testing"
	"time"

	"github.com/cespare/xxhash/v2"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/util"
)

func TestNewPeekingSampleIterator(t *testing.T) {
	iter := NewPeekingSampleIterator(NewSeriesIterator(logproto.Series{
		Samples: []logproto.Sample{
			{
				Timestamp: time.Unix(0, 1).UnixNano(),
			},
			{
				Timestamp: time.Unix(0, 2).UnixNano(),
			},
			{
				Timestamp: time.Unix(0, 3).UnixNano(),
			},
		},
	}))
	_, peek, ok := iter.Peek()
	if peek.Timestamp != 1 {
		t.Fatal("wrong peeked time.")
	}
	if !ok {
		t.Fatal("should be ok.")
	}
	hasNext := iter.Next()
	if !hasNext {
		t.Fatal("should have next.")
	}
	if iter.At().Timestamp != 1 {
		t.Fatal("wrong peeked time.")
	}

	_, peek, ok = iter.Peek()
	if peek.Timestamp != 2 {
		t.Fatal("wrong peeked time.")
	}
	if !ok {
		t.Fatal("should be ok.")
	}
	hasNext = iter.Next()
	if !hasNext {
		t.Fatal("should have next.")
	}
	if iter.At().Timestamp != 2 {
		t.Fatal("wrong peeked time.")
	}
	_, peek, ok = iter.Peek()
	if peek.Timestamp != 3 {
		t.Fatal("wrong peeked time.")
	}
	if !ok {
		t.Fatal("should be ok.")
	}
	hasNext = iter.Next()
	if !hasNext {
		t.Fatal("should have next.")
	}
	if iter.At().Timestamp != 3 {
		t.Fatal("wrong peeked time.")
	}
	_, _, ok = iter.Peek()
	if ok {
		t.Fatal("should not be ok.")
	}
	require.NoError(t, iter.Close())
	require.NoError(t, iter.Err())
}

func sample(i int) logproto.Sample {
	return logproto.Sample{
		Timestamp: int64(i),
		Hash:      uint64(i),
		Value:     float64(1),
	}
}

var varSeries = logproto.Series{
	Labels:     `{foo="var"}`,
	StreamHash: hashLabels(`{foo="var"}`),
	Samples: []logproto.Sample{
		sample(1), sample(2), sample(3),
	},
}

var carSeries = logproto.Series{
	Labels:     `{foo="car"}`,
	StreamHash: hashLabels(`{foo="car"}`),
	Samples: []logproto.Sample{
		sample(1), sample(2), sample(3),
	},
}

func TestNewTimestampFirstMergeSampleIterator(t *testing.T) {
	t.Run("with labels", func(t *testing.T) {
		it := NewTimestampFirstMergeSampleIterator(context.Background(),
			[]SampleIterator{
				NewSeriesIterator(varSeries),
				NewSeriesIterator(carSeries),
				NewSeriesIterator(carSeries),
				NewSeriesIterator(varSeries),
				NewSeriesIterator(carSeries),
				NewSeriesIterator(varSeries),
				NewSeriesIterator(carSeries),
			})

		for i := 1; i < 4; i++ {
			require.True(t, it.Next(), i)
			require.Equal(t, `{foo="car"}`, it.Labels(), i)
			require.Equal(t, sample(i), it.At(), i)
			require.True(t, it.Next(), i)
			require.Equal(t, `{foo="var"}`, it.Labels(), i)
			require.Equal(t, sample(i), it.At(), i)
		}
		require.False(t, it.Next())
		require.NoError(t, it.Err())
		require.NoError(t, it.Close())
	})
	t.Run("no labels", func(t *testing.T) {
		it := NewTimestampFirstMergeSampleIterator(context.Background(),
			[]SampleIterator{
				NewSeriesIterator(logproto.Series{
					Labels:     ``,
					StreamHash: carSeries.StreamHash,
					Samples:    carSeries.Samples,
				}),
				NewSeriesIterator(logproto.Series{
					Labels:     ``,
					StreamHash: varSeries.StreamHash,
					Samples:    varSeries.Samples,
				}), NewSeriesIterator(logproto.Series{
					Labels:     ``,
					StreamHash: carSeries.StreamHash,
					Samples:    carSeries.Samples,
				}),
				NewSeriesIterator(logproto.Series{
					Labels:     ``,
					StreamHash: varSeries.StreamHash,
					Samples:    varSeries.Samples,
				}),
				NewSeriesIterator(logproto.Series{
					Labels:     ``,
					StreamHash: carSeries.StreamHash,
					Samples:    carSeries.Samples,
				}),
				NewSeriesIterator(logproto.Series{
					Labels:     ``,
					StreamHash: varSeries.StreamHash,
					Samples:    varSeries.Samples,
				}),
			})

		for i := 1; i < 4; i++ {
			require.True(t, it.Next(), i)
			require.Equal(t, ``, it.Labels(), i)
			require.Equal(t, sample(i), it.At(), i)
			require.True(t, it.Next(), i)
			require.Equal(t, ``, it.Labels(), i)
			require.Equal(t, sample(i), it.At(), i)
		}
		require.False(t, it.Next())
		require.NoError(t, it.Err())
		require.NoError(t, it.Close())
	})
}

type fakeSampleClient struct {
	series [][]logproto.Series
	curr   int
}

func (f *fakeSampleClient) Recv() (*logproto.SampleQueryResponse, error) {
	if f.curr >= len(f.series) {
		return nil, io.EOF
	}
	res := &logproto.SampleQueryResponse{
		Series: f.series[f.curr],
	}
	f.curr++
	return res, nil
}

func (fakeSampleClient) Context() context.Context { return context.Background() }
func (fakeSampleClient) CloseSend() error         { return nil }
func TestNewTimestampFirstSampleQueryClientIterator(t *testing.T) {
	it := NewTimestampFirstSampleQueryClientIterator(&fakeSampleClient{
		series: [][]logproto.Series{
			{varSeries},
			{carSeries},
		},
	})
	for i := 1; i < 4; i++ {
		require.True(t, it.Next(), i)
		require.Equal(t, `{foo="var"}`, it.Labels(), i)
		require.Equal(t, sample(i), it.At(), i)
	}
	for i := 1; i < 4; i++ {
		require.True(t, it.Next(), i)
		require.Equal(t, `{foo="car"}`, it.Labels(), i)
		require.Equal(t, sample(i), it.At(), i)
	}
	require.False(t, it.Next())
	require.NoError(t, it.Err())
	require.NoError(t, it.Close())
}

func TestNewNonOverlappingSampleIterator(t *testing.T) {
	it := NewNonOverlappingSampleIterator([]SampleIterator{
		NewSeriesIterator(varSeries),
		NewSeriesIterator(logproto.Series{
			Labels:  varSeries.Labels,
			Samples: []logproto.Sample{sample(4), sample(5)},
		}),
	})

	for i := 1; i < 6; i++ {
		require.True(t, it.Next(), i)
		require.Equal(t, `{foo="var"}`, it.Labels(), i)
		require.Equal(t, sample(i), it.At(), i)
	}
	require.False(t, it.Next())
	require.NoError(t, it.Err())
	require.NoError(t, it.Close())
}

func TestReadSampleBatch(t *testing.T) {
	res, size, err := ReadSampleBatch(NewSeriesIterator(carSeries), 1)
	require.Equal(t, &logproto.SampleQueryResponse{Series: []logproto.Series{{Labels: carSeries.Labels, StreamHash: carSeries.StreamHash, Samples: []logproto.Sample{sample(1)}}}}, res)
	require.Equal(t, uint32(1), size)
	require.NoError(t, err)

	res, size, err = ReadSampleBatch(NewMultiSeriesIterator([]logproto.Series{carSeries, varSeries}), 100)
	require.ElementsMatch(t, []logproto.Series{carSeries, varSeries}, res.Series)
	require.Equal(t, uint32(6), size)
	require.NoError(t, err)
}

type CloseTestingSmplIterator struct {
	closed atomic.Bool
	s      logproto.Sample
}

func (i *CloseTestingSmplIterator) Next() bool          { return true }
func (i *CloseTestingSmplIterator) At() logproto.Sample { return i.s }
func (i *CloseTestingSmplIterator) StreamHash() uint64  { return 0 }
func (i *CloseTestingSmplIterator) Labels() string      { return "" }
func (i *CloseTestingSmplIterator) Err() error          { return nil }
func (i *CloseTestingSmplIterator) Close() error {
	i.closed.Store(true)
	return nil
}

func TestNonOverlappingSampleClose(t *testing.T) {
	a, b := &CloseTestingSmplIterator{}, &CloseTestingSmplIterator{}
	itr := NewNonOverlappingSampleIterator([]SampleIterator{a, b})

	// Ensure both itr.cur and itr.iterators are non nil
	itr.Next()

	require.NotNil(t, itr.(*nonOverlappingSampleIterator).curr)

	itr.Close()

	require.Equal(t, true, a.closed.Load())
	require.Equal(t, true, b.closed.Load())
}

// TestMergeSampleIterator_ShouldCloseEverySource checks the merge closes every
// input exactly once: drained during Next, empty in requeue, left on the heap
// for Close, or never prefetched.
func TestMergeSampleIterator_ShouldCloseEverySource(t *testing.T) {
	ctx := context.Background()

	t.Run("fully drained closes every source once", func(t *testing.T) {
		// Staggered timestamps drain the early sources through the merge loop and
		// the last remaining source through the single-iterator shortcut.
		a := &erroringSampleIterator{samples: []logproto.Sample{sample(1), sample(4)}, labels: `{s="a"}`}
		b := &erroringSampleIterator{samples: []logproto.Sample{sample(2), sample(5)}, labels: `{s="b"}`}
		c := &erroringSampleIterator{samples: []logproto.Sample{sample(3)}, labels: `{s="c"}`}

		it := NewTimestampFirstMergeSampleIterator(ctx, []SampleIterator{a, b, c})
		var got int
		for it.Next() {
			got++
		}
		require.NoError(t, it.Err())
		require.Equal(t, 5, got)
		require.NoError(t, it.Close())

		require.Equal(t, 1, a.closed, "a drains through the merge loop and is closed once")
		require.Equal(t, 1, b.closed, "b drains last through the shortcut and is closed once")
		require.Equal(t, 1, c.closed, "c drains through the merge loop and is closed once")
	})

	t.Run("single source drained through the shortcut is closed once", func(t *testing.T) {
		a := &erroringSampleIterator{samples: []logproto.Sample{sample(1), sample(2)}, labels: `{s="a"}`}

		it := NewTimestampFirstMergeSampleIterator(ctx, []SampleIterator{a})
		for it.Next() { //nolint:revive
		}
		require.NoError(t, it.Close())

		require.Equal(t, 1, a.closed)
	})

	t.Run("empty sources are closed once", func(t *testing.T) {
		empty := &erroringSampleIterator{labels: `{s="empty"}`}
		a := &erroringSampleIterator{samples: []logproto.Sample{sample(1)}, labels: `{s="a"}`}

		it := NewTimestampFirstMergeSampleIterator(ctx, []SampleIterator{empty, a})
		for it.Next() { //nolint:revive
		}
		require.NoError(t, it.Close())

		require.Equal(t, 1, empty.closed, "an empty source is closed once when requeued")
		require.Equal(t, 1, a.closed)
	})

	t.Run("close before full drain closes each source once", func(t *testing.T) {
		a := &erroringSampleIterator{samples: []logproto.Sample{sample(1)}, labels: `{s="a"}`}
		b := &erroringSampleIterator{samples: []logproto.Sample{sample(2)}, labels: `{s="b"}`}

		it := NewTimestampFirstMergeSampleIterator(ctx, []SampleIterator{a, b})
		require.True(t, it.Next()) // drains a; b stays on the heap
		require.NoError(t, it.Close())

		require.Equal(t, 1, a.closed, "a drained during Next is not closed again by Close")
		require.Equal(t, 1, b.closed, "b left on the heap is closed by Close")
	})

	t.Run("Close closes every heap source even when one Close fails", func(t *testing.T) {
		// Two data runs so a single Next leaves both sources on the heap. Both
		// Close calls fail, so a first-error return would leak whichever is second.
		a := &erroringSampleIterator{samples: []logproto.Sample{sample(1), sample(3)}, labels: `{s="a"}`, closeErr: errors.New("close a")}
		b := &erroringSampleIterator{samples: []logproto.Sample{sample(2), sample(4)}, labels: `{s="b"}`, closeErr: errors.New("close b")}

		it := NewTimestampFirstMergeSampleIterator(ctx, []SampleIterator{a, b})
		require.True(t, it.Next()) // prefetch both onto the heap
		it.Close()

		require.Equal(t, 1, a.closed, "a is closed once")
		require.Equal(t, 1, b.closed, "a failing Close must not leak b")
	})

	t.Run("Close before any iteration closes every source", func(t *testing.T) {
		// Never iterated, so the sources are still queued and not yet on the heap.
		a := &erroringSampleIterator{samples: []logproto.Sample{sample(1)}, labels: `{s="a"}`}
		b := &erroringSampleIterator{samples: []logproto.Sample{sample(2)}, labels: `{s="b"}`}

		it := NewTimestampFirstMergeSampleIterator(ctx, []SampleIterator{a, b})
		require.NoError(t, it.Close())

		require.Equal(t, 1, a.closed, "an un-prefetched source is closed by Close")
		require.Equal(t, 1, b.closed, "an un-prefetched source is closed by Close")
	})
}

// TestMergeSampleIterator_ShouldSurfaceDrainError checks a source's read error
// reaches Err when it fails at EOF while draining through Next.
func TestMergeSampleIterator_ShouldSurfaceDrainError(t *testing.T) {
	ctx := context.Background()
	wantErr := errors.New("boom")

	t.Run("error draining through the merge loop is surfaced", func(t *testing.T) {
		// errored yields ts1,ts2 then fails at EOF; healthy at ts3 keeps the merge
		// going, so errored drains through the merge loop rather than the shortcut.
		errored := &erroringSampleIterator{samples: []logproto.Sample{sample(1), sample(2)}, labels: `{s="a"}`, err: wantErr}
		healthy := &erroringSampleIterator{samples: []logproto.Sample{sample(3)}, labels: `{s="b"}`}

		it := NewTimestampFirstMergeSampleIterator(ctx, []SampleIterator{errored, healthy})
		for it.Next() { //nolint:revive
		}
		require.ErrorIs(t, it.Err(), wantErr)
		require.Equal(t, 1, errored.closed)
		require.Equal(t, 1, healthy.closed)
		it.Close()
	})

	t.Run("error draining through the single-iterator shortcut is surfaced", func(t *testing.T) {
		// A lone source that fails at EOF drains through the heap.Len()==1 shortcut.
		errored := &erroringSampleIterator{samples: []logproto.Sample{sample(1), sample(2)}, labels: `{s="a"}`, err: wantErr}

		it := NewTimestampFirstMergeSampleIterator(ctx, []SampleIterator{errored})
		for it.Next() { //nolint:revive
		}
		require.ErrorIs(t, it.Err(), wantErr)
		require.Equal(t, 1, errored.closed)
		it.Close()
	})
}

// TestSortSampleIterator_ShouldCloseEverySource checks the sort closes every
// input exactly once: drained during Next, empty at init, left on the heap for
// Close, or never prefetched.
func TestSortSampleIterator_ShouldCloseEverySource(t *testing.T) {
	t.Run("fully drained closes every source once", func(t *testing.T) {
		// Distinct timestamps interleave the sources so each drains through Next.
		// The sort does not dedupe, so all five samples are returned.
		a := &erroringSampleIterator{samples: []logproto.Sample{sample(1), sample(4)}, labels: `{s="a"}`}
		b := &erroringSampleIterator{samples: []logproto.Sample{sample(2), sample(5)}, labels: `{s="b"}`}
		c := &erroringSampleIterator{samples: []logproto.Sample{sample(3)}, labels: `{s="c"}`}

		it := NewSortSampleIterator([]SampleIterator{a, b, c})
		var got int
		for it.Next() {
			got++
		}
		require.NoError(t, it.Err())
		require.Equal(t, 5, got)
		require.NoError(t, it.Close())

		require.Equal(t, 1, a.closed)
		require.Equal(t, 1, b.closed)
		require.Equal(t, 1, c.closed)
	})

	t.Run("empty sources are closed once", func(t *testing.T) {
		empty := &erroringSampleIterator{labels: `{s="empty"}`}
		a := &erroringSampleIterator{samples: []logproto.Sample{sample(1)}, labels: `{s="a"}`}

		it := NewSortSampleIterator([]SampleIterator{empty, a})
		for it.Next() { //nolint:revive
		}
		require.NoError(t, it.Close())

		require.Equal(t, 1, empty.closed, "an empty source is closed once in init")
		require.Equal(t, 1, a.closed)
	})

	t.Run("close before full drain closes each source once", func(t *testing.T) {
		a := &erroringSampleIterator{samples: []logproto.Sample{sample(1)}, labels: `{s="a"}`}
		b := &erroringSampleIterator{samples: []logproto.Sample{sample(2)}, labels: `{s="b"}`}

		it := NewSortSampleIterator([]SampleIterator{a, b})
		require.True(t, it.Next()) // drains a; b stays on the heap
		require.NoError(t, it.Close())

		require.Equal(t, 1, a.closed, "a drained during Next is not closed again by Close")
		require.Equal(t, 1, b.closed, "b left on the heap is closed by Close")
	})

	t.Run("Close closes every heap source even when one Close fails", func(t *testing.T) {
		// Two data runs so a single Next leaves both sources on the heap. Both
		// Close calls fail, so a first-error return would leak whichever is second.
		a := &erroringSampleIterator{samples: []logproto.Sample{sample(1), sample(3)}, labels: `{s="a"}`, closeErr: errors.New("close a")}
		b := &erroringSampleIterator{samples: []logproto.Sample{sample(2), sample(4)}, labels: `{s="b"}`, closeErr: errors.New("close b")}

		it := NewSortSampleIterator([]SampleIterator{a, b})
		require.True(t, it.Next()) // both stay on the heap
		it.Close()

		require.Equal(t, 1, a.closed, "a is closed once")
		require.Equal(t, 1, b.closed, "a failing Close must not leak b")
	})

	t.Run("Close before any iteration closes every source", func(t *testing.T) {
		// Never iterated, so the sources are still queued and not yet on the heap.
		a := &erroringSampleIterator{samples: []logproto.Sample{sample(1)}, labels: `{s="a"}`}
		b := &erroringSampleIterator{samples: []logproto.Sample{sample(2)}, labels: `{s="b"}`}

		it := NewSortSampleIterator([]SampleIterator{a, b})
		require.NoError(t, it.Close())

		require.Equal(t, 1, a.closed, "an un-prefetched source is closed by Close")
		require.Equal(t, 1, b.closed, "an un-prefetched source is closed by Close")
	})
}

// TestSortSampleIterator_ShouldSurfaceDrainError checks a source's read error
// reaches Err whether it fails at EOF during Next or fails immediately in init.
func TestSortSampleIterator_ShouldSurfaceDrainError(t *testing.T) {
	wantErr := errors.New("boom")

	t.Run("error draining through Next is surfaced", func(t *testing.T) {
		errored := &erroringSampleIterator{samples: []logproto.Sample{sample(1), sample(2)}, labels: `{s="a"}`, err: wantErr}
		healthy := &erroringSampleIterator{samples: []logproto.Sample{sample(3)}, labels: `{s="b"}`}

		it := NewSortSampleIterator([]SampleIterator{errored, healthy})
		for it.Next() { //nolint:revive
		}
		require.ErrorIs(t, it.Err(), wantErr)
		require.Equal(t, 1, errored.closed)
		require.Equal(t, 1, healthy.closed)
		it.Close()
	})

	t.Run("error from an empty source in init is surfaced", func(t *testing.T) {
		errored := &erroringSampleIterator{labels: `{s="a"}`, err: wantErr} // no samples: fails immediately
		healthy := &erroringSampleIterator{samples: []logproto.Sample{sample(1)}, labels: `{s="b"}`}

		it := NewSortSampleIterator([]SampleIterator{errored, healthy})
		for it.Next() { //nolint:revive
		}
		require.ErrorIs(t, it.Err(), wantErr)
		require.Equal(t, 1, errored.closed)
		require.Equal(t, 1, healthy.closed)
		it.Close()
	})
}

func TestSampleIteratorWithClose_CloseIdempotent(t *testing.T) {
	c := 0
	closeFn := func() error {
		c++
		return nil
	}
	it := SampleIteratorWithClose(NoopSampleIterator, closeFn)
	// Multiple calls to close should result in c only ever having been incremented one time from 0 to 1
	err := it.Close()
	assert.NoError(t, err)
	assert.EqualValues(t, 1, c)
	err = it.Close()
	assert.NoError(t, err)
	assert.EqualValues(t, 1, c)
	err = it.Close()
	assert.NoError(t, err)
	assert.EqualValues(t, 1, c)
}

func TestSampleIteratorWithClose_ReturnsError(t *testing.T) {
	closeFn := func() error {
		return errors.New("i broke")
	}
	it := SampleIteratorWithClose(ErrorSampleIterator, closeFn)
	err := it.Close()
	// Verify that a proper multi error is returned when both the iterator and the close function return errors
	if me, ok := err.(util.MultiError); ok {
		assert.True(t, len(me) == 2, "Expected 2 errors, one from the iterator and one from the close function")
		assert.EqualError(t, me[0], "close")
		assert.EqualError(t, me[1], "i broke")
	} else {
		t.Error("Expected returned error to be of type util.MultiError")
	}
	// A second call to Close should return the same error
	err2 := it.Close()
	assert.Equal(t, err, err2)
}

func BenchmarkSortSampleIterator(b *testing.B) {
	var (
		ctx          = context.Background()
		series       []logproto.Series
		entriesCount = 10000
		seriesCount  = 100
	)
	for i := 0; i < seriesCount; i++ {
		series = append(series, logproto.Series{
			Labels: fmt.Sprintf(`{i="%d"}`, i),
		})
	}
	for i := 0; i < entriesCount; i++ {
		series[i%seriesCount].Samples = append(series[i%seriesCount].Samples, logproto.Sample{
			Timestamp: int64(seriesCount - i),
			Value:     float64(i),
		})
	}
	rand.Shuffle(len(series), func(i, j int) {
		series[i], series[j] = series[j], series[i]
	})

	b.Run("merge", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			var itrs []SampleIterator
			for i := 0; i < seriesCount; i++ {
				itrs = append(itrs, NewSeriesIterator(series[i]))
			}
			b.StartTimer()
			it := NewTimestampFirstMergeSampleIterator(ctx, itrs)
			for it.Next() {
				it.At()
			}
			it.Close()
		}
	})
	b.Run("sort", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			var itrs []SampleIterator
			for i := 0; i < seriesCount; i++ {
				itrs = append(itrs, NewSeriesIterator(series[i]))
			}
			b.StartTimer()
			it := NewSortSampleIterator(itrs)
			for it.Next() {
				it.At()
			}
			it.Close()
		}
	})
}

func Test_SampleSortIterator(t *testing.T) {
	t.Run("forward", func(t *testing.T) {
		t.Parallel()
		it := NewSortSampleIterator(
			[]SampleIterator{
				NewSeriesIterator(logproto.Series{
					Samples: []logproto.Sample{
						{Timestamp: 0},
						{Timestamp: 3},
						{Timestamp: 5},
					},
					Labels: `{foo="bar"}`,
				}),
				NewSeriesIterator(logproto.Series{
					Samples: []logproto.Sample{
						{Timestamp: 1},
						{Timestamp: 2},
						{Timestamp: 4},
					},
					Labels: `{foo="bar"}`,
				}),
			})
		var i int64
		defer it.Close()
		for it.Next() {
			require.Equal(t, i, it.At().Timestamp)
			i++
		}
	})
	t.Run("forward sort by stream", func(t *testing.T) {
		t.Parallel()
		it := NewSortSampleIterator(
			[]SampleIterator{
				NewSeriesIterator(logproto.Series{
					Samples: []logproto.Sample{
						{Timestamp: 0},
						{Timestamp: 3},
						{Timestamp: 5},
					},
					Labels: `b`,
				}),
				NewSeriesIterator(logproto.Series{
					Samples: []logproto.Sample{
						{Timestamp: 0},
						{Timestamp: 1},
						{Timestamp: 2},
						{Timestamp: 4},
					},
					Labels: `a`,
				}),
			})

		// The first entry appears in both so we expect it to be sorted by Labels.
		require.True(t, it.Next())
		require.Equal(t, int64(0), it.At().Timestamp)
		require.Equal(t, `a`, it.Labels())

		var i int64
		defer it.Close()
		for it.Next() {
			require.Equal(t, i, it.At().Timestamp)
			i++
		}
	})
}

func TestDedupeMergeSampleIterator(t *testing.T) {
	it := NewTimestampFirstMergeSampleIterator(context.Background(),
		[]SampleIterator{
			NewSeriesIterator(logproto.Series{
				Labels: ``,
				Samples: []logproto.Sample{
					{
						Timestamp: time.Unix(1, 0).UnixNano(),
						Value:     1.,
						Hash:      xxhash.Sum64String("1"),
					},
					{
						Timestamp: time.Unix(1, 0).UnixNano(),
						Value:     1.,
						Hash:      xxhash.Sum64String("2"),
					},
				},
				StreamHash: 0,
			}),
			NewSeriesIterator(logproto.Series{
				Labels: ``,
				Samples: []logproto.Sample{
					{
						Timestamp: time.Unix(1, 0).UnixNano(),
						Value:     1.,
						Hash:      xxhash.Sum64String("2"),
					},
					{
						Timestamp: time.Unix(2, 0).UnixNano(),
						Value:     1.,
						Hash:      xxhash.Sum64String("3"),
					},
				},
				StreamHash: 0,
			}),
		})

	require.True(t, it.Next())
	require.Equal(t, time.Unix(1, 0).UnixNano(), it.At().Timestamp)
	require.Equal(t, 1., it.At().Value)
	require.Equal(t, xxhash.Sum64String("1"), it.At().Hash)
	require.True(t, it.Next())
	require.Equal(t, time.Unix(1, 0).UnixNano(), it.At().Timestamp)
	require.Equal(t, 1., it.At().Value)
	require.Equal(t, xxhash.Sum64String("2"), it.At().Hash)
	require.True(t, it.Next())
	require.Equal(t, time.Unix(2, 0).UnixNano(), it.At().Timestamp)
	require.Equal(t, 1., it.At().Value)
	require.Equal(t, xxhash.Sum64String("3"), it.At().Hash)
}

func TestMergeSampleIteratorZeroHash(t *testing.T) {
	// Create series with samples that have zero hashes but same timestamps
	series1 := logproto.Series{
		Labels:     `{foo="bar"}`,
		StreamHash: hashLabels(`{foo="bar"}`),
		Samples: []logproto.Sample{
			{Timestamp: 1, Value: 1.0, Hash: 0},  // Zero hash
			{Timestamp: 1, Value: 2.0, Hash: 0},  // Zero hash, same timestamp
			{Timestamp: 2, Value: 3.0, Hash: 42}, // Non-zero hash
		},
	}

	series2 := logproto.Series{
		Labels:     `{foo="bar"}`,
		StreamHash: hashLabels(`{foo="bar"}`),
		Samples: []logproto.Sample{
			{Timestamp: 1, Value: 4.0, Hash: 0},  // Zero hash, same timestamp
			{Timestamp: 2, Value: 3.0, Hash: 42}, // Non-zero hash, should be deduplicated
		},
	}

	it := NewTimestampFirstMergeSampleIterator(context.Background(), []SampleIterator{
		NewSeriesIterator(series1),
		NewSeriesIterator(series2),
	})

	// Should get all samples with zero hash at timestamp 1
	require.True(t, it.Next())
	require.Equal(t, `{foo="bar"}`, it.Labels())
	require.Equal(t, logproto.Sample{Timestamp: 1, Value: 1.0, Hash: 0}, it.At())

	require.True(t, it.Next())
	require.Equal(t, `{foo="bar"}`, it.Labels())
	require.Equal(t, logproto.Sample{Timestamp: 1, Value: 2.0, Hash: 0}, it.At())

	require.True(t, it.Next())
	require.Equal(t, `{foo="bar"}`, it.Labels())
	require.Equal(t, logproto.Sample{Timestamp: 1, Value: 4.0, Hash: 0}, it.At())

	// Should get only one sample with non-zero hash at timestamp 2 (deduplicated)
	require.True(t, it.Next())
	require.Equal(t, `{foo="bar"}`, it.Labels())
	require.Equal(t, logproto.Sample{Timestamp: 2, Value: 3.0, Hash: 42}, it.At())

	// No more samples
	require.False(t, it.Next())
	require.NoError(t, it.Err())
	require.NoError(t, it.Close())
}

// TestNonOverlappingSampleIterator_ShouldSurfaceErrors verifies the concatenation
// reports a sub-iterator failure through Err instead of treating it as normal
// exhaustion.
func TestNonOverlappingSampleIterator_ShouldSurfaceErrors(t *testing.T) {
	failing := func(ts int, labels string, err error) SampleIterator {
		return &erroringSampleIterator{samples: []logproto.Sample{sample(ts)}, labels: labels, err: err}
	}
	healthy := func(ts int, labels string) SampleIterator {
		return NewSeriesIterator(logproto.Series{Labels: labels, Samples: []logproto.Sample{sample(ts)}})
	}

	t.Run("error stops iteration and is surfaced", func(t *testing.T) {
		wantErr := errors.New("boom")
		it := NewNonOverlappingSampleIterator([]SampleIterator{
			failing(1, `{app="a"}`, wantErr),
			healthy(2, `{app="b"}`),
		})

		var got int
		for it.Next() {
			got++
		}
		require.Equal(t, 1, got, "iteration stops at the failing stream; later streams are not played")
		require.ErrorIs(t, it.Err(), wantErr, "the error must be surfaced, not dropped as normal exhaustion")
	})

	t.Run("error in the last stream is surfaced", func(t *testing.T) {
		wantErr := errors.New("boom")
		it := NewNonOverlappingSampleIterator([]SampleIterator{
			healthy(1, `{app="a"}`),
			failing(2, `{app="b"}`, wantErr),
		})

		for it.Next() { //nolint:revive
		}
		require.ErrorIs(t, it.Err(), wantErr)
	})

	t.Run("no error returns nil", func(t *testing.T) {
		it := NewNonOverlappingSampleIterator([]SampleIterator{
			healthy(1, `{app="a"}`),
			healthy(2, `{app="b"}`),
		})

		var got int
		for it.Next() {
			got++
		}
		require.Equal(t, 2, got)
		require.NoError(t, it.Err())
	})

	t.Run("close error surfaces through Close, not Err", func(t *testing.T) {
		wantErr := errors.New("close boom")
		it := NewNonOverlappingSampleIterator([]SampleIterator{
			&erroringSampleIterator{samples: []logproto.Sample{sample(1)}, labels: `{app="a"}`, closeErr: wantErr},
			healthy(2, `{app="b"}`),
		})

		require.NoError(t, it.Err(), "a close error is not a read error")
		require.ErrorIs(t, it.Close(), wantErr, "Close must surface a sub-iterator close error")
	})

	t.Run("close error while iterating is not a read error", func(t *testing.T) {
		// The first iterator reads cleanly, then its Close fails while iterating.
		// That is a cleanup failure, not a read failure, so iteration continues
		// and Err stays nil.
		it := NewNonOverlappingSampleIterator([]SampleIterator{
			&erroringSampleIterator{samples: []logproto.Sample{sample(1)}, labels: `{app="a"}`, closeErr: errors.New("close boom")},
			healthy(2, `{app="b"}`),
		})

		var got int
		for it.Next() {
			got++
		}
		require.Equal(t, 2, got, "both streams play; a close failure does not stop iteration")
		require.NoError(t, it.Err(), "a close error during iteration must not become a read error")
	})

	t.Run("stream that errors before any sample stops immediately", func(t *testing.T) {
		wantErr := errors.New("open failed")
		it := NewNonOverlappingSampleIterator([]SampleIterator{
			&erroringSampleIterator{labels: `{app="a"}`, err: wantErr}, // no samples
			healthy(2, `{app="b"}`),
		})

		var got int
		for it.Next() {
			got++
		}
		require.Equal(t, 0, got, "no samples are produced")
		require.ErrorIs(t, it.Err(), wantErr)
	})

	t.Run("error in a middle stream stops before later streams", func(t *testing.T) {
		wantErr := errors.New("boom")
		it := NewNonOverlappingSampleIterator([]SampleIterator{
			healthy(1, `{app="a"}`),
			failing(2, `{app="b"}`, wantErr),
			healthy(3, `{app="c"}`),
		})

		var got int
		for it.Next() {
			got++
		}
		require.Equal(t, 2, got, "the stream after the failing one is not played")
		require.ErrorIs(t, it.Err(), wantErr)
	})

	t.Run("fail-fast leaves cleanup to Close", func(t *testing.T) {
		// The fail-fast path does not close the errored current iterator, so Close
		// must close it and the never-started streams, each exactly once.
		errored := &erroringSampleIterator{samples: []logproto.Sample{sample(1)}, labels: `{app="a"}`, err: errors.New("boom")}
		later1 := &erroringSampleIterator{samples: []logproto.Sample{sample(2)}, labels: `{app="b"}`}
		later2 := &erroringSampleIterator{samples: []logproto.Sample{sample(3)}, labels: `{app="c"}`}

		it := NewNonOverlappingSampleIterator([]SampleIterator{errored, later1, later2})
		for it.Next() { //nolint:revive
		}
		require.NoError(t, it.Close())

		require.Equal(t, 1, errored.closed, "the errored current iterator is closed once")
		require.Equal(t, 1, later1.closed, "an un-started iterator is closed once")
		require.Equal(t, 1, later2.closed, "an un-started iterator is closed once")
	})

	t.Run("close error replaying an already-surfaced read error is not reported again", func(t *testing.T) {
		// Some SampleIterator implementations (e.g. the chunk block iterator) return
		// their stored read error from Close too, as a fallback for callers that
		// only check the Close return value. That must not turn into a second,
		// spurious close error once the read error already surfaced through Err.
		wantErr := errors.New("boom")
		errored := &erroringSampleIterator{samples: []logproto.Sample{sample(1)}, labels: `{app="a"}`, err: wantErr, closeErr: wantErr}

		it := NewNonOverlappingSampleIterator([]SampleIterator{errored, healthy(2, `{app="b"}`)})
		for it.Next() { //nolint:revive
		}
		require.ErrorIs(t, it.Err(), wantErr)
		require.NoError(t, it.Close())
		require.Equal(t, 1, errored.closed)
	})

	t.Run("Close surfaces every close error", func(t *testing.T) {
		it := NewNonOverlappingSampleIterator([]SampleIterator{
			&erroringSampleIterator{samples: []logproto.Sample{sample(1)}, closeErr: errors.New("close a")},
			&erroringSampleIterator{samples: []logproto.Sample{sample(2)}, closeErr: errors.New("close b")},
		})

		err := it.Close() // no iteration, so both remain for Close to close
		require.ErrorContains(t, err, "close a")
		require.ErrorContains(t, err, "close b")
	})

	t.Run("Err is stable after a read error", func(t *testing.T) {
		wantErr := errors.New("boom")
		it := NewNonOverlappingSampleIterator([]SampleIterator{failing(1, `{app="a"}`, wantErr)})

		for it.Next() { //nolint:revive
		}
		require.False(t, it.Next(), "further Next calls keep returning false")
		require.ErrorIs(t, it.Err(), wantErr, "Err stays set")
	})
}

// erroringSampleIterator yields its samples, then fails: the Next after the last
// sample returns false with err set, modeling a stream that read some data and
// then failed mid-stream.
type erroringSampleIterator struct {
	samples  []logproto.Sample
	labels   string
	err      error // a read failure, returned by Err once the samples are exhausted
	closeErr error // a cleanup failure, returned by Close; kept separate from err
	closed   int   // number of times Close was called
	i        int
}

func (it *erroringSampleIterator) Next() bool {
	it.i++
	return it.i <= len(it.samples)
}
func (it *erroringSampleIterator) At() logproto.Sample { return it.samples[it.i-1] }
func (it *erroringSampleIterator) Labels() string      { return it.labels }
func (it *erroringSampleIterator) StreamHash() uint64  { return 0 }
func (it *erroringSampleIterator) Close() error        { it.closed++; return it.closeErr }
func (it *erroringSampleIterator) Err() error {
	if it.i > len(it.samples) {
		return it.err
	}
	return nil
}
