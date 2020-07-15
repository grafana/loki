package storage

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
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
	c := newCachedIterator(iter.NewStreamIterator(stream), 3)

	assert := func() {
		// we should crash for call of entry without next although that's not expected.
		require.Equal(t, stream.Labels, c.Labels())
		require.Equal(t, stream.Entries[0], c.Entry())
		require.Equal(t, true, c.Next())
		require.Equal(t, stream.Entries[0], c.Entry())
		require.Equal(t, true, c.Next())
		require.Equal(t, stream.Entries[1], c.Entry())
		require.Equal(t, true, c.Next())
		require.Equal(t, stream.Entries[2], c.Entry())
		require.Equal(t, false, c.Next())
		require.Equal(t, nil, c.Error())
		require.Equal(t, stream.Entries[2], c.Entry())
		require.Equal(t, false, c.Next())
	}

	assert()

	// Close the iterator reset it to the beginning.
	require.Equal(t, nil, c.Close())

	assert()
}

func Test_EmptyCachedIterator(t *testing.T) {

	c := newCachedIterator(iter.NoopIterator, 0)

	require.Equal(t, "", c.Labels())
	require.Equal(t, logproto.Entry{}, c.Entry())
	require.Equal(t, false, c.Next())
	require.Equal(t, "", c.Labels())
	require.Equal(t, logproto.Entry{}, c.Entry())

	require.Equal(t, nil, c.Close())

	require.Equal(t, "", c.Labels())
	require.Equal(t, logproto.Entry{}, c.Entry())
	require.Equal(t, false, c.Next())
	require.Equal(t, "", c.Labels())
	require.Equal(t, logproto.Entry{}, c.Entry())

}

func Test_ErrorCachedIterator(t *testing.T) {

	c := newCachedIterator(&errorIter{}, 0)

	require.Equal(t, false, c.Next())
	require.Equal(t, "", c.Labels())
	require.Equal(t, logproto.Entry{}, c.Entry())
	require.Equal(t, errors.New("error"), c.Error())
	require.Equal(t, errors.New("close"), c.Close())
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
	c := newCachedSampleIterator(iter.NewSeriesIterator(series), 3)

	assert := func() {
		// we should crash for call of entry without next although that's not expected.
		require.Equal(t, series.Labels, c.Labels())
		require.Equal(t, series.Samples[0], c.Sample())
		require.Equal(t, true, c.Next())
		require.Equal(t, series.Samples[0], c.Sample())
		require.Equal(t, true, c.Next())
		require.Equal(t, series.Samples[1], c.Sample())
		require.Equal(t, true, c.Next())
		require.Equal(t, series.Samples[2], c.Sample())
		require.Equal(t, false, c.Next())
		require.Equal(t, nil, c.Error())
		require.Equal(t, series.Samples[2], c.Sample())
		require.Equal(t, false, c.Next())
	}

	assert()

	// Close the iterator reset it to the beginning.
	require.Equal(t, nil, c.Close())

	assert()
}

func Test_EmptyCachedSampleIterator(t *testing.T) {

	c := newCachedSampleIterator(iter.NoopIterator, 0)

	require.Equal(t, "", c.Labels())
	require.Equal(t, logproto.Sample{}, c.Sample())
	require.Equal(t, false, c.Next())
	require.Equal(t, "", c.Labels())
	require.Equal(t, logproto.Sample{}, c.Sample())

	require.Equal(t, nil, c.Close())

	require.Equal(t, "", c.Labels())
	require.Equal(t, logproto.Sample{}, c.Sample())
	require.Equal(t, false, c.Next())
	require.Equal(t, "", c.Labels())
	require.Equal(t, logproto.Sample{}, c.Sample())

}

func Test_ErrorCachedSampleIterator(t *testing.T) {

	c := newCachedSampleIterator(&errorIter{}, 0)

	require.Equal(t, false, c.Next())
	require.Equal(t, "", c.Labels())
	require.Equal(t, logproto.Sample{}, c.Sample())
	require.Equal(t, errors.New("error"), c.Error())
	require.Equal(t, errors.New("close"), c.Close())
}

type errorIter struct{}

func (errorIter) Next() bool              { return false }
func (errorIter) Error() error            { return errors.New("error") }
func (errorIter) Labels() string          { return "" }
func (errorIter) Entry() logproto.Entry   { return logproto.Entry{} }
func (errorIter) Sample() logproto.Sample { return logproto.Sample{} }
func (errorIter) Close() error            { return errors.New("close") }
