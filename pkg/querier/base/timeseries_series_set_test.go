package base

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logproto"
)

func TestTimeSeriesSeriesSet(t *testing.T) {

	timeseries := []logproto.TimeSeries{
		{
			Labels: []logproto.LabelAdapter{
				{
					Name:  "label1",
					Value: "value1",
				},
			},
			Samples: []logproto.LegacySample{
				{
					Value:       3.14,
					TimestampMs: 1234,
				},
			},
		},
	}

	ss := newTimeSeriesSeriesSet(timeseries)

	require.True(t, ss.Next())
	series := ss.At()

	require.Equal(t, ss.ts[0].Labels[0].Name, series.Labels()[0].Name)
	require.Equal(t, ss.ts[0].Labels[0].Value, series.Labels()[0].Value)

	it := series.Iterator()
	require.True(t, it.Next())
	ts, v := it.At()
	require.Equal(t, 3.14, v)
	require.Equal(t, int64(1234), ts)
	require.False(t, ss.Next())

	// Append a new sample to seek to
	timeseries[0].Samples = append(timeseries[0].Samples, logproto.LegacySample{
		Value:       1.618,
		TimestampMs: 2345,
	})
	ss = newTimeSeriesSeriesSet(timeseries)

	require.True(t, ss.Next())
	it = ss.At().Iterator()
	require.True(t, it.Seek(2000))
	ts, v = it.At()
	require.Equal(t, 1.618, v)
	require.Equal(t, int64(2345), ts)
}

func TestTimeSeriesIterator(t *testing.T) {
	ts := timeseries{
		series: logproto.TimeSeries{
			Labels: []logproto.LabelAdapter{
				{
					Name:  "label1",
					Value: "value1",
				},
			},
			Samples: []logproto.LegacySample{
				{
					Value:       3.14,
					TimestampMs: 1234,
				},
				{
					Value:       3.14,
					TimestampMs: 1235,
				},
				{
					Value:       3.14,
					TimestampMs: 1236,
				},
			},
		},
	}

	it := ts.Iterator()
	require.True(t, it.Seek(1235)) // Seek to middle
	i, _ := it.At()
	require.EqualValues(t, 1235, i)
	require.True(t, it.Seek(1236)) // Seek to end
	i, _ = it.At()
	require.EqualValues(t, 1236, i)
	require.False(t, it.Seek(1238)) // Seek past end

	it = ts.Iterator()
	require.True(t, it.Next())
	require.True(t, it.Next())
	i, _ = it.At()
	require.EqualValues(t, 1235, i)
	require.True(t, it.Seek(1234)) // Ensure seek doesn't do anything if already past seek target.
	i, _ = it.At()
	require.EqualValues(t, 1235, i)

	it = ts.Iterator()
	for i := 0; it.Next(); {
		j, _ := it.At()
		switch i {
		case 0:
			require.EqualValues(t, 1234, j)
		case 1:
			require.EqualValues(t, 1235, j)
		case 2:
			require.EqualValues(t, 1236, j)
		default:
			t.Fail()
		}
		i++
	}
	it.At() // Ensure an At after a full iteration, doesn't cause a panic
}
