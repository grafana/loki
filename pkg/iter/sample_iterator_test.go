package iter

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logproto"
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
	if iter.Sample().Timestamp != 1 {
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
	if iter.Sample().Timestamp != 2 {
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
	if iter.Sample().Timestamp != 3 {
		t.Fatal("wrong peeked time.")
	}
	_, _, ok = iter.Peek()
	if ok {
		t.Fatal("should not be ok.")
	}
	require.NoError(t, iter.Close())
	require.NoError(t, iter.Error())
}

func sample(i int) logproto.Sample {
	return logproto.Sample{
		Timestamp: int64(i),
		Hash:      uint64(i),
		Value:     float64(1),
	}
}

var varSeries = logproto.Series{
	Labels: `{foo="var"}`,
	Samples: []logproto.Sample{
		sample(1), sample(2), sample(3),
	},
}
var carSeries = logproto.Series{
	Labels: `{foo="car"}`,
	Samples: []logproto.Sample{
		sample(1), sample(2), sample(3),
	},
}

func TestNewHeapSampleIterator(t *testing.T) {
	it := NewHeapSampleIterator(context.Background(),
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
		require.Equal(t, sample(i), it.Sample(), i)
		require.True(t, it.Next(), i)
		require.Equal(t, `{foo="var"}`, it.Labels(), i)
		require.Equal(t, sample(i), it.Sample(), i)
	}
	require.False(t, it.Next())
	require.NoError(t, it.Error())
	require.NoError(t, it.Close())
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
		require.Equal(t, sample(i), it.Sample(), i)
	}
	for i := 1; i < 4; i++ {
		require.True(t, it.Next(), i)
		require.Equal(t, `{foo="car"}`, it.Labels(), i)
		require.Equal(t, sample(i), it.Sample(), i)
	}
	require.False(t, it.Next())
	require.NoError(t, it.Error())
	require.NoError(t, it.Close())
}

func TestNewNonOverlappingSampleIterator(t *testing.T) {
	it := NewNonOverlappingSampleIterator([]SampleIterator{
		NewSeriesIterator(varSeries),
		NewSeriesIterator(logproto.Series{
			Labels:  varSeries.Labels,
			Samples: []logproto.Sample{sample(4), sample(5)},
		}),
	}, varSeries.Labels)

	for i := 1; i < 6; i++ {
		require.True(t, it.Next(), i)
		require.Equal(t, `{foo="var"}`, it.Labels(), i)
		require.Equal(t, sample(i), it.Sample(), i)
	}
	require.False(t, it.Next())
	require.NoError(t, it.Error())
	require.NoError(t, it.Close())
}

func TestReadSampleBatch(t *testing.T) {
	res, size, err := ReadSampleBatch(NewSeriesIterator(carSeries), 1)
	require.Equal(t, &logproto.SampleQueryResponse{Series: []logproto.Series{{Labels: carSeries.Labels, Samples: []logproto.Sample{sample(1)}}}}, res)
	require.Equal(t, uint32(1), size)
	require.NoError(t, err)

	res, size, err = ReadSampleBatch(NewMultiSeriesIterator(context.Background(), []logproto.Series{carSeries, varSeries}), 100)
	require.ElementsMatch(t, []logproto.Series{carSeries, varSeries}, res.Series)
	require.Equal(t, uint32(6), size)
	require.NoError(t, err)
}
