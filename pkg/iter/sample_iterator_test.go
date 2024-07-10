package iter

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"testing"
	"time"

	"github.com/cespare/xxhash"
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

func TestNewMergeSampleIterator(t *testing.T) {
	t.Run("with labels", func(t *testing.T) {
		it := NewMergeSampleIterator(context.Background(),
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
		it := NewMergeSampleIterator(context.Background(),
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
func TestNewSampleQueryClientIterator(t *testing.T) {
	it := NewSampleQueryClientIterator(&fakeSampleClient{
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
			it := NewMergeSampleIterator(ctx, itrs)
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
	it := NewMergeSampleIterator(context.Background(),
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
