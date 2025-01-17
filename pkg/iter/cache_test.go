package iter

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
)

func Test_CachedIterator(t *testing.T) {
	stream := logproto.Stream{
		Labels: `{foo="bar"}`,
		Entries: []logproto.Entry{
			{Timestamp: time.Unix(0, 1), Line: "1"},
			{Timestamp: time.Unix(0, 2), Line: "2"},
			{Timestamp: time.Unix(0, 3), Line: "3"},
		},
	}
	c := NewCachedIterator(NewStreamIterator(stream), 3)

	assert := func() {
		require.Equal(t, "", c.Labels())
		require.Equal(t, logproto.Entry{}, c.At())
		require.Equal(t, true, c.Next())
		require.Equal(t, stream.Entries[0], c.At())
		require.Equal(t, true, c.Next())
		require.Equal(t, stream.Entries[1], c.At())
		require.Equal(t, true, c.Next())
		require.Equal(t, stream.Entries[2], c.At())
		require.Equal(t, false, c.Next())
		require.NoError(t, c.Err())
		require.Equal(t, stream.Entries[2], c.At())
		require.Equal(t, false, c.Next())
	}

	assert()

	// Close the iterator reset it to the beginning.
	require.Equal(t, nil, c.Close())

	assert()
}

func Test_EmptyCachedIterator(t *testing.T) {
	c := NewCachedIterator(NoopEntryIterator, 0)

	require.Equal(t, "", c.Labels())
	require.Equal(t, logproto.Entry{}, c.At())
	require.Equal(t, false, c.Next())
	require.Equal(t, "", c.Labels())
	require.Equal(t, logproto.Entry{}, c.At())

	require.Equal(t, nil, c.Close())

	require.Equal(t, "", c.Labels())
	require.Equal(t, logproto.Entry{}, c.At())
	require.Equal(t, false, c.Next())
	require.Equal(t, "", c.Labels())
	require.Equal(t, logproto.Entry{}, c.At())
}

func Test_ErrorCachedIterator(t *testing.T) {
	c := NewCachedIterator(ErrorEntryIterator, 0)

	require.Equal(t, false, c.Next())
	require.Equal(t, "", c.Labels())
	require.Equal(t, logproto.Entry{}, c.At())
	require.Equal(t, errors.New("error"), c.Err())
	require.Equal(t, errors.New("close"), c.Close())
}

func Test_CachedIteratorResetNotExhausted(t *testing.T) {
	stream := logproto.Stream{
		Labels: `{foo="bar"}`,
		Entries: []logproto.Entry{
			{Timestamp: time.Unix(0, 1), Line: "1"},
			{Timestamp: time.Unix(0, 2), Line: "2"},
			{Timestamp: time.Unix(0, 3), Line: "3"},
		},
	}
	c := NewCachedIterator(NewStreamIterator(stream), 3)

	require.Equal(t, true, c.Next())
	require.Equal(t, stream.Entries[0], c.At())
	require.Equal(t, true, c.Next())
	require.Equal(t, stream.Entries[1], c.At())
	c.Reset()
	require.Equal(t, true, c.Next())
	require.Equal(t, stream.Entries[0], c.At())
	require.Equal(t, true, c.Next())
	require.Equal(t, stream.Entries[1], c.At())
	require.Equal(t, true, c.Next())
	require.Equal(t, stream.Entries[2], c.At())
	require.Equal(t, false, c.Next())
	require.NoError(t, c.Err())
	require.Equal(t, stream.Entries[2], c.At())
	require.Equal(t, false, c.Next())

	// Close the iterator reset it to the beginning.
	require.Equal(t, nil, c.Close())
}

func Test_CachedIteratorResetExhausted(t *testing.T) {
	stream := logproto.Stream{
		Labels: `{foo="bar"}`,
		Entries: []logproto.Entry{
			{Timestamp: time.Unix(0, 1), Line: "1"},
			{Timestamp: time.Unix(0, 2), Line: "2"},
		},
	}
	c := NewCachedIterator(NewStreamIterator(stream), 3)

	require.Equal(t, true, c.Next())
	require.Equal(t, stream.Entries[0], c.At())
	require.Equal(t, true, c.Next())
	require.Equal(t, stream.Entries[1], c.At())
	c.Reset()
	require.Equal(t, true, c.Next())
	require.Equal(t, stream.Entries[0], c.At())
	require.Equal(t, true, c.Next())
	require.Equal(t, stream.Entries[1], c.At())
	require.Equal(t, false, c.Next())

	// Close the iterator reset it to the beginning.
	require.Equal(t, nil, c.Close())
}

func Test_CachedSampleIterator(t *testing.T) {
	series := logproto.Series{
		Labels: `{foo="bar"}`,
		Samples: []logproto.Sample{
			{Timestamp: time.Unix(0, 1).UnixNano(), Hash: 1, Value: 1.},
			{Timestamp: time.Unix(0, 2).UnixNano(), Hash: 2, Value: 2.},
			{Timestamp: time.Unix(0, 3).UnixNano(), Hash: 3, Value: 3.},
		},
	}
	c := NewCachedSampleIterator(NewSeriesIterator(series), 3)

	assert := func() {
		require.Equal(t, "", c.Labels())
		require.Equal(t, logproto.Sample{}, c.At())
		require.Equal(t, true, c.Next())
		require.Equal(t, series.Samples[0], c.At())
		require.Equal(t, true, c.Next())
		require.Equal(t, series.Samples[1], c.At())
		require.Equal(t, true, c.Next())
		require.Equal(t, series.Samples[2], c.At())
		require.Equal(t, false, c.Next())
		require.NoError(t, c.Err())
		require.Equal(t, series.Samples[2], c.At())
		require.Equal(t, false, c.Next())
	}

	assert()

	// Close the iterator reset it to the beginning.
	require.Equal(t, nil, c.Close())

	assert()
}

func Test_CachedSampleIteratorResetNotExhausted(t *testing.T) {
	series := logproto.Series{
		Labels: `{foo="bar"}`,
		Samples: []logproto.Sample{
			{Timestamp: time.Unix(0, 1).UnixNano(), Hash: 1, Value: 1.},
			{Timestamp: time.Unix(0, 2).UnixNano(), Hash: 2, Value: 2.},
			{Timestamp: time.Unix(0, 3).UnixNano(), Hash: 3, Value: 3.},
		},
	}
	c := NewCachedSampleIterator(NewSeriesIterator(series), 3)

	require.Equal(t, true, c.Next())
	require.Equal(t, series.Samples[0], c.At())
	require.Equal(t, true, c.Next())
	require.Equal(t, series.Samples[1], c.At())
	c.Reset()
	require.Equal(t, true, c.Next())
	require.Equal(t, series.Samples[0], c.At())
	require.Equal(t, true, c.Next())
	require.Equal(t, series.Samples[1], c.At())
	require.Equal(t, true, c.Next())
	require.Equal(t, series.Samples[2], c.At())
	require.Equal(t, false, c.Next())
	require.NoError(t, c.Err())
	require.Equal(t, series.Samples[2], c.At())
	require.Equal(t, false, c.Next())

	// Close the iterator reset it to the beginning.
	require.Equal(t, nil, c.Close())
}

func Test_CachedSampleIteratorResetExhausted(t *testing.T) {
	series := logproto.Series{
		Labels: `{foo="bar"}`,
		Samples: []logproto.Sample{
			{Timestamp: time.Unix(0, 1).UnixNano(), Hash: 1, Value: 1.},
			{Timestamp: time.Unix(0, 2).UnixNano(), Hash: 2, Value: 2.},
		},
	}
	c := NewCachedSampleIterator(NewSeriesIterator(series), 3)

	require.Equal(t, true, c.Next())
	require.Equal(t, series.Samples[0], c.At())
	require.Equal(t, true, c.Next())
	require.Equal(t, series.Samples[1], c.At())
	c.Reset()
	require.Equal(t, true, c.Next())
	require.Equal(t, series.Samples[0], c.At())
	require.Equal(t, true, c.Next())
	require.Equal(t, series.Samples[1], c.At())
	require.Equal(t, false, c.Next())

	// Close the iterator reset it to the beginning.
	require.Equal(t, nil, c.Close())
}

func Test_EmptyCachedSampleIterator(t *testing.T) {
	c := NewCachedSampleIterator(NoopSampleIterator, 0)

	require.Equal(t, "", c.Labels())
	require.Equal(t, logproto.Sample{}, c.At())
	require.Equal(t, false, c.Next())
	require.Equal(t, "", c.Labels())
	require.Equal(t, logproto.Sample{}, c.At())

	require.Equal(t, nil, c.Close())

	require.Equal(t, "", c.Labels())
	require.Equal(t, logproto.Sample{}, c.At())
	require.Equal(t, false, c.Next())
	require.Equal(t, "", c.Labels())
	require.Equal(t, logproto.Sample{}, c.At())
}

func Test_ErrorCachedSampleIterator(t *testing.T) {
	c := NewCachedSampleIterator(ErrorSampleIterator, 0)

	require.Equal(t, false, c.Next())
	require.Equal(t, "", c.Labels())
	require.Equal(t, logproto.Sample{}, c.At())
	require.Equal(t, errors.New("error"), c.Err())
	require.Equal(t, errors.New("close"), c.Close())
}
