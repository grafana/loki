package logql

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
)

func Test_seriesIterator_Peek(t *testing.T) {
	type expectation struct {
		ok     bool
		sample Sample
	}
	for _, test := range []struct {
		name         string
		it           SeriesIterator
		expectations []expectation
	}{
		{
			"count",
			newSeriesIterator(iter.NewStreamIterator(newStream(5, identity, `{app="foo"}`)), extractCount),
			[]expectation{
				{true, Sample{Labels: `{app="foo"}`, TimestampNano: 0, Value: 1}},
				{true, Sample{Labels: `{app="foo"}`, TimestampNano: time.Unix(1, 0).UnixNano(), Value: 1}},
				{true, Sample{Labels: `{app="foo"}`, TimestampNano: time.Unix(2, 0).UnixNano(), Value: 1}},
				{true, Sample{Labels: `{app="foo"}`, TimestampNano: time.Unix(3, 0).UnixNano(), Value: 1}},
				{true, Sample{Labels: `{app="foo"}`, TimestampNano: time.Unix(4, 0).UnixNano(), Value: 1}},
				{false, Sample{}},
			},
		},
		{
			"bytes empty",
			newSeriesIterator(
				iter.NewStreamIterator(newStream(3,
					func(i int64) logproto.Entry {
						return logproto.Entry{
							Timestamp: time.Unix(i, 0),
						}
					},
					`{app="foo"}`)),
				extractBytes,
			),
			[]expectation{
				{true, Sample{Labels: `{app="foo"}`, TimestampNano: 0, Value: 0}},
				{true, Sample{Labels: `{app="foo"}`, TimestampNano: time.Unix(1, 0).UnixNano(), Value: 0}},
				{true, Sample{Labels: `{app="foo"}`, TimestampNano: time.Unix(2, 0).UnixNano(), Value: 0}},
				{false, Sample{}},
			},
		},
		{
			"bytes",
			newSeriesIterator(
				iter.NewStreamIterator(newStream(3,
					func(i int64) logproto.Entry {
						return logproto.Entry{
							Timestamp: time.Unix(i, 0),
							Line:      "foo",
						}
					},
					`{app="foo"}`)),
				extractBytes,
			),
			[]expectation{
				{true, Sample{Labels: `{app="foo"}`, TimestampNano: 0, Value: 3}},
				{true, Sample{Labels: `{app="foo"}`, TimestampNano: time.Unix(1, 0).UnixNano(), Value: 3}},
				{true, Sample{Labels: `{app="foo"}`, TimestampNano: time.Unix(2, 0).UnixNano(), Value: 3}},
				{false, Sample{}},
			},
		},
		{
			"bytes backward",
			newSeriesIterator(
				iter.NewStreamsIterator(context.Background(),
					[]logproto.Stream{
						newStream(3,
							func(i int64) logproto.Entry {
								return logproto.Entry{
									Timestamp: time.Unix(i, 0),
									Line:      "foo",
								}
							},
							`{app="foo"}`), newStream(3,
							func(i int64) logproto.Entry {
								return logproto.Entry{
									Timestamp: time.Unix(i, 0),
									Line:      "barr",
								}
							},
							`{app="barr"}`),
					},
					logproto.BACKWARD,
				),
				extractBytes,
			),
			[]expectation{
				{true, Sample{Labels: `{app="foo"}`, TimestampNano: 0, Value: 3}},
				{true, Sample{Labels: `{app="foo"}`, TimestampNano: time.Unix(1, 0).UnixNano(), Value: 3}},
				{true, Sample{Labels: `{app="foo"}`, TimestampNano: time.Unix(2, 0).UnixNano(), Value: 3}},
				{true, Sample{Labels: `{app="barr"}`, TimestampNano: 0, Value: 4}},
				{true, Sample{Labels: `{app="barr"}`, TimestampNano: time.Unix(1, 0).UnixNano(), Value: 4}},
				{true, Sample{Labels: `{app="barr"}`, TimestampNano: time.Unix(2, 0).UnixNano(), Value: 4}},
				{false, Sample{}},
			},
		},
		{
			"skip first",
			newSeriesIterator(iter.NewStreamIterator(newStream(2, identity, `{app="foo"}`)), fakeSampler{}),
			[]expectation{
				{true, Sample{Labels: `{app="foo"}`, TimestampNano: time.Unix(1, 0).UnixNano(), Value: 10}},
				{false, Sample{}},
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, e := range test.expectations {
				sample, ok := test.it.Peek()
				require.Equal(t, e.ok, ok)
				if !e.ok {
					continue
				}
				require.Equal(t, e.sample, sample)
				test.it.Next()
			}
			require.NoError(t, test.it.Close())
		})
	}
}

// fakeSampler is a Sampler that returns no value for 0 timestamp otherwise always 10
type fakeSampler struct{}

func (fakeSampler) From(lbs string, entry logproto.Entry) (Sample, bool) {
	if entry.Timestamp.UnixNano() == 0 {
		return Sample{}, false
	}
	return Sample{
		Labels:        lbs,
		TimestampNano: entry.Timestamp.UnixNano(),
		Value:         10,
	}, true
}
