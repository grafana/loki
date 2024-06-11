package iter

import (
	"hash/fnv"
	"testing"

	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/stretchr/testify/require"
)

func TestNewSumMergeSampleIterator(t *testing.T) {
	t.Run("with labels -- no merge", func(t *testing.T) {
		it := NewSumMergeSampleIterator(
			[]iter.SampleIterator{
				iter.NewSeriesIterator(varSeries),
				iter.NewSeriesIterator(carSeries),
			})

		for i := 1; i < 4; i++ {
			require.True(t, it.Next(), i)
			require.Equal(t, `{foo="car"}`, it.Labels(), i)
			require.Equal(t, sample(int64(i), float64(i)), it.Sample(), i)
			require.True(t, it.Next(), i)
			require.Equal(t, `{foo="var"}`, it.Labels(), i)
			require.Equal(t, sample(int64(i), float64(i)), it.Sample(), i)
		}
		require.False(t, it.Next())
		require.NoError(t, it.Error())
		require.NoError(t, it.Close())
	})

	t.Run("with labels -- merge", func(t *testing.T) {
		it := NewSumMergeSampleIterator(
			[]iter.SampleIterator{
				iter.NewSeriesIterator(varSeries),
				iter.NewSeriesIterator(carSeries),
				iter.NewSeriesIterator(varSeries),
				iter.NewSeriesIterator(carSeries),
				iter.NewSeriesIterator(varSeries),
				iter.NewSeriesIterator(carSeries),
			})

		for i := 1; i < 4; i++ {
			require.True(t, it.Next(), i)
			require.Equal(t, `{foo="car"}`, it.Labels(), i)
			require.Equal(t, sample(int64(i), float64(i*3)), it.Sample(), i)
			require.True(t, it.Next(), i)
			require.Equal(t, `{foo="var"}`, it.Labels(), i)
			require.Equal(t, sample(int64(i), float64(i*3)), it.Sample(), i)
		}
		require.False(t, it.Next())
		require.NoError(t, it.Error())
		require.NoError(t, it.Close())
	})

	t.Run("no labels", func(t *testing.T) {
		it := NewSumMergeSampleIterator(
			[]iter.SampleIterator{
				iter.NewSeriesIterator(logproto.Series{
					Labels:     ``,
					StreamHash: carSeries.StreamHash,
					Samples:    carSeries.Samples,
				}),
				iter.NewSeriesIterator(logproto.Series{
					Labels:     ``,
					StreamHash: varSeries.StreamHash,
					Samples:    varSeries.Samples,
				}),
			})

		for i := 1; i < 4; i++ {
			require.True(t, it.Next(), i)
			require.Equal(t, ``, it.Labels(), i)
			require.Equal(t, sample(int64(i), float64(i)), it.Sample(), i)
			require.True(t, it.Next(), i)
			require.Equal(t, ``, it.Labels(), i)
			require.Equal(t, sample(int64(i), float64(i)), it.Sample(), i)
		}
		require.False(t, it.Next())
		require.NoError(t, it.Error())
		require.NoError(t, it.Close())
	})

	t.Run("no labels -- merge", func(t *testing.T) {
		it := NewSumMergeSampleIterator(
			[]iter.SampleIterator{
				iter.NewSeriesIterator(logproto.Series{
					Labels:     ``,
					StreamHash: carSeries.StreamHash,
					Samples:    carSeries.Samples,
				}),
				iter.NewSeriesIterator(logproto.Series{
					Labels:     ``,
					StreamHash: varSeries.StreamHash,
					Samples:    varSeries.Samples,
				}),
				iter.NewSeriesIterator(logproto.Series{
					Labels:     ``,
					StreamHash: carSeries.StreamHash,
					Samples:    carSeries.Samples,
				}),
				iter.NewSeriesIterator(logproto.Series{
					Labels:     ``,
					StreamHash: varSeries.StreamHash,
					Samples:    varSeries.Samples,
				}),
				iter.NewSeriesIterator(logproto.Series{
					Labels:     ``,
					StreamHash: carSeries.StreamHash,
					Samples:    carSeries.Samples,
				}),
				iter.NewSeriesIterator(logproto.Series{
					Labels:     ``,
					StreamHash: varSeries.StreamHash,
					Samples:    varSeries.Samples,
				}),
			})

		for i := 1; i < 4; i++ {
			require.True(t, it.Next(), i)
			require.Equal(t, ``, it.Labels(), i)
			require.Equal(t, sample(int64(i), float64(i*3)), it.Sample(), i)
			require.True(t, it.Next(), i)
			require.Equal(t, ``, it.Labels(), i)
			require.Equal(t, sample(int64(i), float64(i*3)), it.Sample(), i)
		}
		require.False(t, it.Next())
		require.NoError(t, it.Error())
		require.NoError(t, it.Close())
	})
	t.Run("it sums the values from two identical points", func(t *testing.T) {
		series := logproto.Series{
			Labels:     `{foo="bar"}`,
			StreamHash: hashLabels(`{foo="bar"}`),
			Samples: []logproto.Sample{
				sample(1, 1), sample(2, 2), sample(3, 3),
			},
		}
		it := NewSumMergeSampleIterator(
			[]iter.SampleIterator{
				iter.NewSeriesIterator(series),
				iter.NewSeriesIterator(series),
			})

		require.True(t, it.Next())
		require.Equal(t, `{foo="bar"}`, it.Labels())
		require.Equal(t, sample(1, 2), it.Sample())

		require.True(t, it.Next())
		require.Equal(t, `{foo="bar"}`, it.Labels())
		require.Equal(t, sample(2, 4), it.Sample())

		require.True(t, it.Next())
		require.Equal(t, `{foo="bar"}`, it.Labels())
		require.Equal(t, sample(3, 6), it.Sample())

		require.False(t, it.Next())
	})
	t.Run("it sums the values from two streams with different data points", func(t *testing.T) {
		series1 := logproto.Series{
			Labels:     `{foo="bar"}`,
			StreamHash: hashLabels(`{foo="bar"}`),
			Samples: []logproto.Sample{
				sample(1, 1), sample(2, 2), sample(3, 3),
			},
		}
		series2 := logproto.Series{
			Labels:     `{foo="baz"}`,
			StreamHash: hashLabels(`{foo="baz"}`),
			Samples: []logproto.Sample{
				sample(1, 1), sample(2, 2), sample(3, 4),
			},
		}
		series3 := logproto.Series{
			Labels:     `{foo="bar"}`,
			StreamHash: hashLabels(`{foo="bar"}`),
			Samples: []logproto.Sample{
				sample(2, 2), sample(4, 4),
			},
		}
		it := NewSumMergeSampleIterator(
			[]iter.SampleIterator{
				iter.NewSeriesIterator(series1),
				iter.NewSeriesIterator(series2),
				iter.NewSeriesIterator(series3),
			})

		require.True(t, it.Next())
		require.Equal(t, `{foo="baz"}`, it.Labels())
		require.Equal(t, sample(1, 1), it.Sample())

		require.True(t, it.Next())
		require.Equal(t, `{foo="bar"}`, it.Labels())
		require.Equal(t, sample(1, 1), it.Sample()) // first only

		require.True(t, it.Next())
		require.Equal(t, `{foo="baz"}`, it.Labels())
		require.Equal(t, sample(2, 2), it.Sample())

		require.True(t, it.Next())
		require.Equal(t, `{foo="bar"}`, it.Labels())
		require.Equal(t, sample(2, 4), it.Sample()) // merged

		require.True(t, it.Next())
		require.Equal(t, `{foo="baz"}`, it.Labels())
		require.Equal(t, sample(3, 4), it.Sample())

		require.True(t, it.Next())
		require.Equal(t, `{foo="bar"}`, it.Labels())
		require.Equal(t, sample(3, 3), it.Sample())

		require.True(t, it.Next())
		require.Equal(t, `{foo="bar"}`, it.Labels())
		require.Equal(t, sample(4, 4), it.Sample()) // second only

		require.False(t, it.Next())
	})
}

var varSeries = logproto.Series{
	Labels:     `{foo="var"}`,
	StreamHash: hashLabels(`{foo="var"}`),
	Samples: []logproto.Sample{
		sample(1, 1), sample(2, 2), sample(3, 3),
	},
}

var carSeries = logproto.Series{
	Labels:     `{foo="car"}`,
	StreamHash: hashLabels(`{foo="car"}`),
	Samples: []logproto.Sample{
		sample(1, 1), sample(2, 2), sample(3, 3),
	},
}

func sample(t int64, v float64) logproto.Sample {
	// The main difference between this MergeSampleIterator and the one from
	// v3/pkg/iter is that this one does not care about the Sample's hash
	// since it is not coming from a log line.
	return logproto.Sample{
		Timestamp: t,
		Hash:      uint64(42),
		Value:     v,
	}
}

func hashLabels(lbs string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(lbs))
	return h.Sum64()
}
