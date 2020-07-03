package iter

import (
	"context"
	"testing"
	"time"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/stretchr/testify/require"
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

func TestNewHeapSampleIterator(t *testing.T) {
	it := NewHeapSampleIterator(context.Background(),
		[]SampleIterator{
			NewSeriesIterator(logproto.Series{
				Labels: `{foo="var"}`,
				Samples: []logproto.Sample{
					sample(1), sample(2), sample(3),
				},
			}), NewSeriesIterator(logproto.Series{
				Labels: `{foo="car"}`,
				Samples: []logproto.Sample{
					sample(1), sample(2), sample(3),
				},
			}), NewSeriesIterator(logproto.Series{
				Labels: `{foo="car"}`,
				Samples: []logproto.Sample{
					sample(1), sample(2), sample(3),
				},
			}), NewSeriesIterator(logproto.Series{
				Labels: `{foo="var"}`,
				Samples: []logproto.Sample{
					sample(1), sample(2), sample(3),
				},
			}), NewSeriesIterator(logproto.Series{
				Labels: `{foo="car"}`,
				Samples: []logproto.Sample{
					sample(1), sample(2), sample(3),
				},
			}), NewSeriesIterator(logproto.Series{
				Labels: `{foo="var"}`,
				Samples: []logproto.Sample{
					sample(1), sample(2), sample(3),
				},
			}), NewSeriesIterator(logproto.Series{
				Labels: `{foo="car"}`,
				Samples: []logproto.Sample{
					sample(1), sample(2), sample(3),
				},
			}),
		})

	for i := 1; i < 4; i++ {
		require.True(t, it.Next(), i)
		require.Equal(t, `{foo="car"}`, it.Labels(), i)
		require.Equal(t, sample(i), it.Sample(), i)
		require.True(t, it.Next(), i)
		require.Equal(t, `{foo="var"}`, it.Labels(), i)
		require.Equal(t, sample(i), it.Sample(), i)
	}

}
