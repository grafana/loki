package storage

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/cespare/xxhash/v2"
	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/user"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logql/stats"
)

var NilMetrics = NewChunkMetrics(nil, 0)

func Test_batchIterSafeStart(t *testing.T) {
	stream := logproto.Stream{
		Labels: fooLabelsWithName,
		Entries: []logproto.Entry{
			{
				Timestamp: from,
				Line:      "1",
			},
			{
				Timestamp: from.Add(time.Millisecond),
				Line:      "2",
			},
		},
	}
	chks := []*LazyChunk{
		newLazyChunk(stream),
	}

	var ok bool

	batch := newBatchChunkIterator(context.Background(), chks, 1, logproto.FORWARD, from, from.Add(4*time.Millisecond), func(chunks []*LazyChunk, from, through time.Time, nextChunk *LazyChunk) (genericIterator, error) {
		if !ok {
			panic("unexpected")
		}

		// we don't care about the actual data for this test, just give it an iterator.
		return iter.NewStreamIterator(stream), nil
	})

	// if it was started already, we should see a panic before this
	time.Sleep(time.Millisecond)
	ok = true

	// ensure idempotency
	batch.Start()
	batch.Start()

	ok = batch.Next()
	require.Equal(t, true, ok)

}

func Test_newLogBatchChunkIterator(t *testing.T) {

	tests := map[string]struct {
		chunks     []*LazyChunk
		expected   []logproto.Stream
		matchers   string
		start, end time.Time
		direction  logproto.Direction
		batchSize  int
	}{
		"forward with overlap": {
			[]*LazyChunk{
				newLazyChunk(logproto.Stream{
					Labels: fooLabelsWithName,
					Entries: []logproto.Entry{
						{
							Timestamp: from,
							Line:      "1",
						},
						{
							Timestamp: from.Add(time.Millisecond),
							Line:      "2",
						},
					},
				}),
				newLazyChunk(logproto.Stream{
					Labels: fooLabelsWithName,
					Entries: []logproto.Entry{
						{
							Timestamp: from.Add(time.Millisecond),
							Line:      "2",
						},
						{
							Timestamp: from.Add(2 * time.Millisecond),
							Line:      "3",
						},
					},
				}),
				newLazyChunk(logproto.Stream{
					Labels: fooLabelsWithName,
					Entries: []logproto.Entry{
						{
							Timestamp: from.Add(time.Millisecond),
							Line:      "2",
						},
						{
							Timestamp: from.Add(2 * time.Millisecond),
							Line:      "3",
						},
					},
				}),
				newLazyChunk(logproto.Stream{
					Labels: fooLabelsWithName,
					Entries: []logproto.Entry{
						{
							Timestamp: from.Add(2 * time.Millisecond),
							Line:      "3",
						},
						{
							Timestamp: from.Add(3 * time.Millisecond),
							Line:      "4",
						},
					},
				}),
				newLazyChunk(logproto.Stream{
					Labels: fooLabelsWithName,
					Entries: []logproto.Entry{
						{
							Timestamp: from.Add(2 * time.Millisecond),
							Line:      "3",
						},
						{
							Timestamp: from.Add(3 * time.Millisecond),
							Line:      "4",
						},
					},
				}),
				newLazyChunk(logproto.Stream{
					Labels: fooLabelsWithName,
					Entries: []logproto.Entry{
						{
							Timestamp: from.Add(3 * time.Millisecond),
							Line:      "4",
						},
						{
							Timestamp: from.Add(4 * time.Millisecond),
							Line:      "5",
						},
					},
				}),
			},
			[]logproto.Stream{
				{
					Labels: fooLabels,
					Entries: []logproto.Entry{
						{
							Timestamp: from,
							Line:      "1",
						},
						{
							Timestamp: from.Add(time.Millisecond),
							Line:      "2",
						},
						{
							Timestamp: from.Add(2 * time.Millisecond),
							Line:      "3",
						},
						{
							Timestamp: from.Add(3 * time.Millisecond),
							Line:      "4",
						},
					},
				},
			},
			fooLabelsWithName,
			from, from.Add(4 * time.Millisecond),
			logproto.FORWARD,
			2,
		},
		"forward all overlap and all chunks have a from time less than query from time": {
			[]*LazyChunk{
				newLazyChunk(logproto.Stream{
					Labels: fooLabelsWithName,
					Entries: []logproto.Entry{
						{
							Timestamp: from,
							Line:      "1",
						},
						{
							Timestamp: from.Add(time.Millisecond),
							Line:      "2",
						},
					},
				}),
				newLazyChunk(logproto.Stream{
					Labels: fooLabelsWithName,
					Entries: []logproto.Entry{
						{
							Timestamp: from,
							Line:      "1",
						},
						{
							Timestamp: from.Add(time.Millisecond),
							Line:      "2",
						},
						{
							Timestamp: from.Add(2 * time.Millisecond),
							Line:      "3",
						},
					},
				}),
				newLazyChunk(logproto.Stream{
					Labels: fooLabelsWithName,
					Entries: []logproto.Entry{
						{
							Timestamp: from,
							Line:      "1",
						},
						{
							Timestamp: from.Add(time.Millisecond),
							Line:      "2",
						},
						{
							Timestamp: from.Add(2 * time.Millisecond),
							Line:      "3",
						},
					},
				}),
				newLazyChunk(logproto.Stream{
					Labels: fooLabelsWithName,
					Entries: []logproto.Entry{
						{
							Timestamp: from,
							Line:      "1",
						},
						{
							Timestamp: from.Add(2 * time.Millisecond),
							Line:      "3",
						},
						{
							Timestamp: from.Add(3 * time.Millisecond),
							Line:      "4",
						},
					},
				}),
				newLazyChunk(logproto.Stream{
					Labels: fooLabelsWithName,
					Entries: []logproto.Entry{
						{
							Timestamp: from,
							Line:      "1",
						},
						{
							Timestamp: from.Add(2 * time.Millisecond),
							Line:      "3",
						},
						{
							Timestamp: from.Add(3 * time.Millisecond),
							Line:      "4",
						},
					},
				}),
				newLazyChunk(logproto.Stream{
					Labels: fooLabelsWithName,
					Entries: []logproto.Entry{
						{
							Timestamp: from,
							Line:      "1",
						},
						{
							Timestamp: from.Add(3 * time.Millisecond),
							Line:      "4",
						},
						{
							Timestamp: from.Add(4 * time.Millisecond),
							Line:      "5",
						},
					},
				}),
			},
			[]logproto.Stream{
				{
					Labels: fooLabels,
					Entries: []logproto.Entry{
						{
							Timestamp: from.Add(time.Millisecond),
							Line:      "2",
						},
						{
							Timestamp: from.Add(2 * time.Millisecond),
							Line:      "3",
						},
						{
							Timestamp: from.Add(3 * time.Millisecond),
							Line:      "4",
						},
						{
							Timestamp: from.Add(4 * time.Millisecond),
							Line:      "5",
						},
					},
				},
			},
			fooLabelsWithName,
			from.Add(1 * time.Millisecond), from.Add(5 * time.Millisecond),
			logproto.FORWARD,
			2,
		},
		"forward with overlapping non-continuous entries": {
			[]*LazyChunk{
				newLazyChunk(logproto.Stream{
					Labels: fooLabelsWithName,
					Entries: []logproto.Entry{
						{
							Timestamp: from,
							Line:      "1",
						},
						{
							Timestamp: from.Add(time.Millisecond),
							Line:      "2",
						},
						{
							Timestamp: from.Add(3 * time.Millisecond),
							Line:      "4",
						},
					},
				}),
				newLazyChunk(logproto.Stream{
					Labels: fooLabelsWithName,
					Entries: []logproto.Entry{
						{
							Timestamp: from.Add(time.Millisecond),
							Line:      "2",
						},
						{
							Timestamp: from.Add(2 * time.Millisecond),
							Line:      "3",
						},
					},
				}),
				newLazyChunk(logproto.Stream{
					Labels: fooLabelsWithName,
					Entries: []logproto.Entry{
						{
							Timestamp: from.Add(time.Millisecond),
							Line:      "2",
						},
						{
							Timestamp: from.Add(3 * time.Millisecond),
							Line:      "4",
						},
					},
				}),
				newLazyChunk(logproto.Stream{
					Labels: fooLabelsWithName,
					Entries: []logproto.Entry{
						{
							Timestamp: from.Add(2 * time.Millisecond),
							Line:      "3",
						},
						{
							Timestamp: from.Add(3 * time.Millisecond),
							Line:      "4",
						},
					},
				}),
			},
			[]logproto.Stream{
				{
					Labels: fooLabels,
					Entries: []logproto.Entry{
						{
							Timestamp: from,
							Line:      "1",
						},
						{
							Timestamp: from.Add(time.Millisecond),
							Line:      "2",
						},
						{
							Timestamp: from.Add(2 * time.Millisecond),
							Line:      "3",
						},
					},
				},
			},
			fooLabelsWithName,
			from, from.Add(3 * time.Millisecond),
			logproto.FORWARD,
			2,
		},
		"backward with overlap": {
			[]*LazyChunk{
				newLazyChunk(logproto.Stream{
					Labels: fooLabelsWithName,
					Entries: []logproto.Entry{
						{
							Timestamp: from,
							Line:      "1",
						},
						{
							Timestamp: from.Add(time.Millisecond),
							Line:      "2",
						},
					},
				}),
				newLazyChunk(logproto.Stream{
					Labels: fooLabelsWithName,
					Entries: []logproto.Entry{
						{
							Timestamp: from.Add(time.Millisecond),
							Line:      "2",
						},
						{
							Timestamp: from.Add(2 * time.Millisecond),
							Line:      "3",
						},
					},
				}),
				newLazyChunk(logproto.Stream{
					Labels: fooLabelsWithName,
					Entries: []logproto.Entry{
						{
							Timestamp: from.Add(time.Millisecond),
							Line:      "2",
						},
						{
							Timestamp: from.Add(2 * time.Millisecond),
							Line:      "3",
						},
					},
				}),
				newLazyChunk(logproto.Stream{
					Labels: fooLabelsWithName,
					Entries: []logproto.Entry{
						{
							Timestamp: from.Add(2 * time.Millisecond),
							Line:      "3",
						},
						{
							Timestamp: from.Add(3 * time.Millisecond),
							Line:      "4",
						},
					},
				}),
				newLazyChunk(logproto.Stream{
					Labels: fooLabelsWithName,
					Entries: []logproto.Entry{
						{
							Timestamp: from.Add(2 * time.Millisecond),
							Line:      "3",
						},
						{
							Timestamp: from.Add(3 * time.Millisecond),
							Line:      "4",
						},
					},
				}),
				newLazyChunk(logproto.Stream{
					Labels: fooLabelsWithName,
					Entries: []logproto.Entry{
						{
							Timestamp: from.Add(3 * time.Millisecond),
							Line:      "4",
						},
						{
							Timestamp: from.Add(4 * time.Millisecond),
							Line:      "5",
						},
					},
				}),
			},
			[]logproto.Stream{
				{
					Labels: fooLabels,
					Entries: []logproto.Entry{
						{
							Timestamp: from.Add(3 * time.Millisecond),
							Line:      "4",
						},
						{
							Timestamp: from.Add(2 * time.Millisecond),
							Line:      "3",
						},
						{
							Timestamp: from.Add(time.Millisecond),
							Line:      "2",
						},
						{
							Timestamp: from,
							Line:      "1",
						},
					},
				},
			},
			fooLabelsWithName,
			from, from.Add(4 * time.Millisecond),
			logproto.BACKWARD,
			2,
		},
		"backward all overlap and all chunks have a through time greater than query through time": {
			[]*LazyChunk{
				newLazyChunk(logproto.Stream{
					Labels: fooLabelsWithName,
					Entries: []logproto.Entry{
						{
							Timestamp: from,
							Line:      "1",
						},
						{
							Timestamp: from.Add(time.Millisecond),
							Line:      "2",
						},
						{
							Timestamp: from.Add(4 * time.Millisecond),
							Line:      "5",
						},
					},
				}),
				newLazyChunk(logproto.Stream{
					Labels: fooLabelsWithName,
					Entries: []logproto.Entry{
						{
							Timestamp: from.Add(time.Millisecond),
							Line:      "2",
						},
						{
							Timestamp: from.Add(2 * time.Millisecond),
							Line:      "3",
						},
						{
							Timestamp: from.Add(4 * time.Millisecond),
							Line:      "5",
						},
					},
				}),
				newLazyChunk(logproto.Stream{
					Labels: fooLabelsWithName,
					Entries: []logproto.Entry{
						{
							Timestamp: from.Add(time.Millisecond),
							Line:      "2",
						},
						{
							Timestamp: from.Add(2 * time.Millisecond),
							Line:      "3",
						},
						{
							Timestamp: from.Add(4 * time.Millisecond),
							Line:      "5",
						},
					},
				}),
				newLazyChunk(logproto.Stream{
					Labels: fooLabelsWithName,
					Entries: []logproto.Entry{
						{
							Timestamp: from.Add(2 * time.Millisecond),
							Line:      "3",
						},
						{
							Timestamp: from.Add(3 * time.Millisecond),
							Line:      "4",
						},
						{
							Timestamp: from.Add(4 * time.Millisecond),
							Line:      "5",
						},
					},
				}),
				newLazyChunk(logproto.Stream{
					Labels: fooLabelsWithName,
					Entries: []logproto.Entry{
						{
							Timestamp: from.Add(2 * time.Millisecond),
							Line:      "3",
						},
						{
							Timestamp: from.Add(3 * time.Millisecond),
							Line:      "4",
						},
						{
							Timestamp: from.Add(4 * time.Millisecond),
							Line:      "5",
						},
					},
				}),
				newLazyChunk(logproto.Stream{
					Labels: fooLabelsWithName,
					Entries: []logproto.Entry{
						{
							Timestamp: from.Add(3 * time.Millisecond),
							Line:      "4",
						},
						{
							Timestamp: from.Add(4 * time.Millisecond),
							Line:      "5",
						},
					},
				}),
			},
			[]logproto.Stream{
				{
					Labels: fooLabels,
					Entries: []logproto.Entry{
						{
							Timestamp: from.Add(3 * time.Millisecond),
							Line:      "4",
						},
						{
							Timestamp: from.Add(2 * time.Millisecond),
							Line:      "3",
						},
						{
							Timestamp: from.Add(time.Millisecond),
							Line:      "2",
						},
						{
							Timestamp: from,
							Line:      "1",
						},
					},
				},
			},
			fooLabelsWithName,
			from, from.Add(4 * time.Millisecond),
			logproto.BACKWARD,
			2,
		},
		"backward with overlapping non-continuous entries": {
			[]*LazyChunk{
				newLazyChunk(logproto.Stream{
					Labels: fooLabelsWithName,
					Entries: []logproto.Entry{
						{
							Timestamp: from.Add(0 * time.Millisecond),
							Line:      "0",
						},
						{
							Timestamp: from.Add(3 * time.Millisecond),
							Line:      "3",
						},
					},
				}),
				newLazyChunk(logproto.Stream{
					Labels: fooLabelsWithName,
					Entries: []logproto.Entry{
						{
							Timestamp: from.Add(1 * time.Millisecond),
							Line:      "1",
						},
						{
							Timestamp: from.Add(6 * time.Millisecond),
							Line:      "6",
						},
					},
				}),
				newLazyChunk(logproto.Stream{
					Labels: fooLabelsWithName,
					Entries: []logproto.Entry{
						{
							Timestamp: from.Add(2 * time.Millisecond),
							Line:      "2",
						},
						{
							Timestamp: from.Add(5 * time.Millisecond),
							Line:      "5",
						},
					},
				}),
				newLazyChunk(logproto.Stream{
					Labels: fooLabelsWithName,
					Entries: []logproto.Entry{
						{
							Timestamp: from.Add(4 * time.Millisecond),
							Line:      "4",
						},
						{
							Timestamp: from.Add(7 * time.Millisecond),
							Line:      "7",
						},
					},
				}),
			},
			[]logproto.Stream{
				{
					Labels: fooLabels,
					Entries: []logproto.Entry{
						{
							Timestamp: from.Add(7 * time.Millisecond),
							Line:      "7",
						},
						{
							Timestamp: from.Add(6 * time.Millisecond),
							Line:      "6",
						},
						{
							Timestamp: from.Add(5 * time.Millisecond),
							Line:      "5",
						},
						{
							Timestamp: from.Add(4 * time.Millisecond),
							Line:      "4",
						},
						{
							Timestamp: from.Add(3 * time.Millisecond),
							Line:      "3",
						},
						{
							Timestamp: from.Add(2 * time.Millisecond),
							Line:      "2",
						},
						{
							Timestamp: from.Add(1 * time.Millisecond),
							Line:      "1",
						},
						{
							Timestamp: from.Add(0 * time.Millisecond),
							Line:      "0",
						},
					},
				},
			},
			fooLabelsWithName,
			from, from.Add(8 * time.Millisecond),
			logproto.BACKWARD,
			2,
		},
		"forward without overlap": {
			[]*LazyChunk{
				newLazyChunk(logproto.Stream{
					Labels: fooLabelsWithName,
					Entries: []logproto.Entry{
						{
							Timestamp: from,
							Line:      "1",
						},
						{
							Timestamp: from.Add(time.Millisecond),
							Line:      "2",
						},
					},
				}),
				newLazyChunk(logproto.Stream{
					Labels: fooLabelsWithName,
					Entries: []logproto.Entry{
						{
							Timestamp: from.Add(2 * time.Millisecond),
							Line:      "3",
						},
					},
				}),
				newLazyChunk(logproto.Stream{
					Labels: fooLabelsWithName,
					Entries: []logproto.Entry{
						{
							Timestamp: from.Add(3 * time.Millisecond),
							Line:      "4",
						},
					},
				}),
			},
			[]logproto.Stream{
				{
					Labels: fooLabels,
					Entries: []logproto.Entry{
						{
							Timestamp: from,
							Line:      "1",
						},
						{
							Timestamp: from.Add(time.Millisecond),
							Line:      "2",
						},
						{
							Timestamp: from.Add(2 * time.Millisecond),
							Line:      "3",
						},
					},
				},
			},
			fooLabelsWithName,
			from, from.Add(3 * time.Millisecond),
			logproto.FORWARD,
			2,
		},
		"backward without overlap": {
			[]*LazyChunk{
				newLazyChunk(logproto.Stream{
					Labels: fooLabelsWithName,
					Entries: []logproto.Entry{
						{
							Timestamp: from,
							Line:      "1",
						},
						{
							Timestamp: from.Add(time.Millisecond),
							Line:      "2",
						},
					},
				}),
				newLazyChunk(logproto.Stream{
					Labels: fooLabelsWithName,
					Entries: []logproto.Entry{
						{
							Timestamp: from.Add(2 * time.Millisecond),
							Line:      "3",
						},
					},
				}),
				newLazyChunk(logproto.Stream{
					Labels: fooLabelsWithName,
					Entries: []logproto.Entry{
						{
							Timestamp: from.Add(3 * time.Millisecond),
							Line:      "4",
						},
					},
				}),
			},
			[]logproto.Stream{
				{
					Labels: fooLabels,
					Entries: []logproto.Entry{
						{
							Timestamp: from.Add(2 * time.Millisecond),
							Line:      "3",
						},
						{
							Timestamp: from.Add(time.Millisecond),
							Line:      "2",
						},
						{
							Timestamp: from,
							Line:      "1",
						},
					},
				},
			},
			fooLabelsWithName,
			from, from.Add(3 * time.Millisecond),
			logproto.BACKWARD,
			2,
		},
		// This test is rather complex under the hood.
		// It should cause three sub batches in the iterator.
		// The first batch has no overlap -- it cannot as the first. It has bounds [1,2)
		// The second batch has one chunk overlap, but it includes no entries in the overlap.
		// It has bounds [2,4).
		// The third batch finally consumes the overlap, with bounds [4,max).
		// Notably it also ends up testing the code paths for increasing batch sizes past
		// the default due to nextChunks with the same start timestamp.
		"forward identicals": {
			[]*LazyChunk{
				newLazyChunk(logproto.Stream{
					Labels: fooLabelsWithName,
					Entries: []logproto.Entry{
						{
							Timestamp: from,
							Line:      "1",
						},
					},
				}),
				newLazyChunk(logproto.Stream{
					Labels: fooLabelsWithName,
					Entries: []logproto.Entry{
						{
							Timestamp: from,
							Line:      "1",
						},
					},
				}),
				newLazyChunk(logproto.Stream{
					Labels: fooLabelsWithName,
					Entries: []logproto.Entry{
						{
							Timestamp: from,
							Line:      "1",
						},
						{
							Timestamp: from.Add(3 * time.Millisecond),
							Line:      "4",
						},
					},
				}),
				newLazyChunk(logproto.Stream{
					Labels: fooLabelsWithName,
					Entries: []logproto.Entry{
						{
							Timestamp: from.Add(time.Millisecond),
							Line:      "2",
						},
					},
				}),
				newLazyChunk(logproto.Stream{
					Labels: fooLabelsWithName,
					Entries: []logproto.Entry{
						{
							Timestamp: from.Add(time.Millisecond),
							Line:      "2",
						},
					},
				}),
				newLazyChunk(logproto.Stream{
					Labels: fooLabelsWithName,
					Entries: []logproto.Entry{
						{
							Timestamp: from.Add(time.Millisecond),
							Line:      "2",
						},
					},
				}),
				newLazyChunk(logproto.Stream{
					Labels: fooLabelsWithName,
					Entries: []logproto.Entry{
						{
							Timestamp: from.Add(3 * time.Millisecond),
							Line:      "4",
						},
					},
				}),
			},
			[]logproto.Stream{
				{
					Labels: fooLabels,
					Entries: []logproto.Entry{
						{
							Timestamp: from,
							Line:      "1",
						},
						{
							Timestamp: from.Add(time.Millisecond),
							Line:      "2",
						},
						{
							Timestamp: from.Add(3 * time.Millisecond),
							Line:      "4",
						},
					},
				},
			},
			fooLabelsWithName,
			from, from.Add(4 * time.Millisecond),
			logproto.FORWARD,
			1,
		},
	}

	for name, tt := range tests {
		tt := tt
		t.Run(name, func(t *testing.T) {
			it, err := newLogBatchIterator(context.Background(), NilMetrics, tt.chunks, tt.batchSize, newMatchers(tt.matchers), nil, tt.direction, tt.start, tt.end)
			require.NoError(t, err)
			streams, _, err := iter.ReadBatch(it, 1000)
			_ = it.Close()
			if err != nil {
				t.Fatalf("error reading batch %s", err)
			}

			assertStream(t, tt.expected, streams.Streams)

		})
	}
}

func Test_newSampleBatchChunkIterator(t *testing.T) {

	tests := map[string]struct {
		chunks     []*LazyChunk
		expected   []logproto.Series
		matchers   string
		start, end time.Time
		batchSize  int
	}{
		"forward with overlap": {
			[]*LazyChunk{
				newLazyChunk(logproto.Stream{
					Labels: fooLabelsWithName,
					Entries: []logproto.Entry{
						{
							Timestamp: from,
							Line:      "1",
						},
						{
							Timestamp: from.Add(time.Millisecond),
							Line:      "2",
						},
					},
				}),
				newLazyChunk(logproto.Stream{
					Labels: fooLabelsWithName,
					Entries: []logproto.Entry{
						{
							Timestamp: from.Add(time.Millisecond),
							Line:      "2",
						},
						{
							Timestamp: from.Add(2 * time.Millisecond),
							Line:      "3",
						},
					},
				}),
				newLazyChunk(logproto.Stream{
					Labels: fooLabelsWithName,
					Entries: []logproto.Entry{
						{
							Timestamp: from.Add(time.Millisecond),
							Line:      "2",
						},
						{
							Timestamp: from.Add(2 * time.Millisecond),
							Line:      "3",
						},
					},
				}),
				newLazyChunk(logproto.Stream{
					Labels: fooLabelsWithName,
					Entries: []logproto.Entry{
						{
							Timestamp: from.Add(2 * time.Millisecond),
							Line:      "3",
						},
						{
							Timestamp: from.Add(3 * time.Millisecond),
							Line:      "4",
						},
					},
				}),
				newLazyChunk(logproto.Stream{
					Labels: fooLabelsWithName,
					Entries: []logproto.Entry{
						{
							Timestamp: from.Add(2 * time.Millisecond),
							Line:      "3",
						},
						{
							Timestamp: from.Add(3 * time.Millisecond),
							Line:      "4",
						},
					},
				}),
				newLazyChunk(logproto.Stream{
					Labels: fooLabelsWithName,
					Entries: []logproto.Entry{
						{
							Timestamp: from.Add(3 * time.Millisecond),
							Line:      "4",
						},
						{
							Timestamp: from.Add(4 * time.Millisecond),
							Line:      "5",
						},
					},
				}),
			},
			[]logproto.Series{
				{
					Labels: fooLabels,
					Samples: []logproto.Sample{
						{
							Timestamp: from.UnixNano(),
							Hash:      xxhash.Sum64String("1"),
							Value:     1.,
						},
						{
							Timestamp: from.Add(time.Millisecond).UnixNano(),
							Hash:      xxhash.Sum64String("2"),
							Value:     1.,
						},
						{
							Timestamp: from.Add(2 * time.Millisecond).UnixNano(),
							Hash:      xxhash.Sum64String("3"),
							Value:     1.,
						},
						{
							Timestamp: from.Add(3 * time.Millisecond).UnixNano(),
							Hash:      xxhash.Sum64String("4"),
							Value:     1.,
						},
					},
				},
			},
			fooLabelsWithName,
			from, from.Add(4 * time.Millisecond),
			2,
		},
		"forward with overlapping non-continuous entries": {
			[]*LazyChunk{
				newLazyChunk(logproto.Stream{
					Labels: fooLabelsWithName,
					Entries: []logproto.Entry{
						{
							Timestamp: from,
							Line:      "1",
						},
						{
							Timestamp: from.Add(time.Millisecond),
							Line:      "2",
						},
						{
							Timestamp: from.Add(3 * time.Millisecond),
							Line:      "4",
						},
					},
				}),
				newLazyChunk(logproto.Stream{
					Labels: fooLabelsWithName,
					Entries: []logproto.Entry{
						{
							Timestamp: from.Add(time.Millisecond),
							Line:      "2",
						},
						{
							Timestamp: from.Add(2 * time.Millisecond),
							Line:      "3",
						},
					},
				}),
				newLazyChunk(logproto.Stream{
					Labels: fooLabelsWithName,
					Entries: []logproto.Entry{
						{
							Timestamp: from.Add(time.Millisecond),
							Line:      "2",
						},
						{
							Timestamp: from.Add(3 * time.Millisecond),
							Line:      "4",
						},
					},
				}),
				newLazyChunk(logproto.Stream{
					Labels: fooLabelsWithName,
					Entries: []logproto.Entry{
						{
							Timestamp: from.Add(2 * time.Millisecond),
							Line:      "3",
						},
						{
							Timestamp: from.Add(3 * time.Millisecond),
							Line:      "4",
						},
					},
				}),
			},
			[]logproto.Series{
				{
					Labels: fooLabels,
					Samples: []logproto.Sample{
						{
							Timestamp: from.UnixNano(),
							Hash:      xxhash.Sum64String("1"),
							Value:     1.,
						},
						{
							Timestamp: from.Add(time.Millisecond).UnixNano(),
							Hash:      xxhash.Sum64String("2"),
							Value:     1.,
						},
						{
							Timestamp: from.Add(2 * time.Millisecond).UnixNano(),
							Hash:      xxhash.Sum64String("3"),
							Value:     1.,
						},
					},
				},
			},
			fooLabelsWithName,
			from, from.Add(3 * time.Millisecond),
			2,
		},
		"forward without overlap": {
			[]*LazyChunk{
				newLazyChunk(logproto.Stream{
					Labels: fooLabelsWithName,
					Entries: []logproto.Entry{
						{
							Timestamp: from,
							Line:      "1",
						},
						{
							Timestamp: from.Add(time.Millisecond),
							Line:      "2",
						},
					},
				}),
				newLazyChunk(logproto.Stream{
					Labels: fooLabelsWithName,
					Entries: []logproto.Entry{
						{
							Timestamp: from.Add(2 * time.Millisecond),
							Line:      "3",
						},
					},
				}),
				newLazyChunk(logproto.Stream{
					Labels: fooLabelsWithName,
					Entries: []logproto.Entry{
						{
							Timestamp: from.Add(3 * time.Millisecond),
							Line:      "4",
						},
					},
				}),
			},
			[]logproto.Series{
				{
					Labels: fooLabels,
					Samples: []logproto.Sample{
						{
							Timestamp: from.UnixNano(),
							Hash:      xxhash.Sum64String("1"),
							Value:     1.,
						},
						{
							Timestamp: from.Add(time.Millisecond).UnixNano(),
							Hash:      xxhash.Sum64String("2"),
							Value:     1.,
						},
						{
							Timestamp: from.Add(2 * time.Millisecond).UnixNano(),
							Hash:      xxhash.Sum64String("3"),
							Value:     1.,
						},
					},
				},
			},
			fooLabelsWithName,
			from, from.Add(3 * time.Millisecond),
			2,
		},
	}

	for name, tt := range tests {
		tt := tt
		t.Run(name, func(t *testing.T) {
			it, err := newSampleBatchIterator(context.Background(), NilMetrics, tt.chunks, tt.batchSize, newMatchers(tt.matchers), nil, logql.ExtractCount, tt.start, tt.end)
			require.NoError(t, err)
			series, _, err := iter.ReadSampleBatch(it, 1000)
			_ = it.Close()
			if err != nil {
				t.Fatalf("error reading batch %s", err)
			}

			assertSeries(t, tt.expected, series.Series)

		})
	}
}

func TestPartitionOverlappingchunks(t *testing.T) {
	var (
		oneThroughFour = newLazyChunk(logproto.Stream{
			Labels: fooLabelsWithName,
			Entries: []logproto.Entry{
				{
					Timestamp: from,
					Line:      "1",
				},
				{
					Timestamp: from.Add(3 * time.Millisecond),
					Line:      "4",
				},
			},
		})
		two = newLazyChunk(logproto.Stream{
			Labels: fooLabelsWithName,
			Entries: []logproto.Entry{
				{
					Timestamp: from.Add(1 * time.Millisecond),
					Line:      "2",
				},
			},
		})
		three = newLazyChunk(logproto.Stream{
			Labels: fooLabelsWithName,
			Entries: []logproto.Entry{
				{
					Timestamp: from.Add(2 * time.Millisecond),
					Line:      "3",
				},
			},
		})
	)

	for i, tc := range []struct {
		input    []*LazyChunk
		expected [][]*LazyChunk
	}{
		{
			input: []*LazyChunk{
				oneThroughFour,
				two,
				three,
			},
			expected: [][]*LazyChunk{
				{oneThroughFour},
				{two, three},
			},
		},
		{
			input: []*LazyChunk{
				two,
				oneThroughFour,
				three,
			},
			expected: [][]*LazyChunk{
				{oneThroughFour},
				{two, three},
			},
		},
		{
			input: []*LazyChunk{
				two,
				two,
				three,
				three,
			},
			expected: [][]*LazyChunk{
				{two, three},
				{two, three},
			},
		},
	} {
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			out := partitionOverlappingChunks(tc.input)
			require.Equal(t, tc.expected, out)
		})
	}
}

func TestBuildHeapIterator(t *testing.T) {
	var (
		firstChunk = newLazyChunk(logproto.Stream{
			Labels: "{foo=\"bar\"}",
			Entries: []logproto.Entry{
				{
					Timestamp: from,
					Line:      "1",
				},
				{
					Timestamp: from.Add(time.Millisecond),
					Line:      "2",
				},
				{
					Timestamp: from.Add(2 * time.Millisecond),
					Line:      "3",
				},
			},
		})
		secondChunk = newLazyInvalidChunk(logproto.Stream{
			Labels: "{foo=\"bar\"}",
			Entries: []logproto.Entry{
				{
					Timestamp: from.Add(3 * time.Millisecond),
					Line:      "4",
				},
				{
					Timestamp: from.Add(4 * time.Millisecond),
					Line:      "5",
				},
			},
		})
		thirdChunk = newLazyChunk(logproto.Stream{
			Labels: "{foo=\"bar\"}",
			Entries: []logproto.Entry{
				{
					Timestamp: from.Add(5 * time.Millisecond),
					Line:      "6",
				},
			},
		})
	)

	for i, tc := range []struct {
		input    [][]*LazyChunk
		expected []logproto.Stream
	}{
		{
			[][]*LazyChunk{
				{firstChunk},
				{thirdChunk},
			},
			[]logproto.Stream{
				{
					Labels: "{foo=\"bar\"}",
					Entries: []logproto.Entry{
						{
							Timestamp: from,
							Line:      "1",
						},
						{
							Timestamp: from.Add(time.Millisecond),
							Line:      "2",
						},
						{
							Timestamp: from.Add(2 * time.Millisecond),
							Line:      "3",
						},
						{
							Timestamp: from.Add(5 * time.Millisecond),
							Line:      "6",
						},
					},
				},
			},
		},
		{
			[][]*LazyChunk{
				{secondChunk},
				{firstChunk, thirdChunk},
			},
			[]logproto.Stream{
				{
					Labels: "{foo=\"bar\"}",
					Entries: []logproto.Entry{
						{
							Timestamp: from,
							Line:      "1",
						},
						{
							Timestamp: from.Add(time.Millisecond),
							Line:      "2",
						},
						{
							Timestamp: from.Add(2 * time.Millisecond),
							Line:      "3",
						},
						{
							Timestamp: from.Add(5 * time.Millisecond),
							Line:      "6",
						},
					},
				},
			},
		},
	} {
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			ctx = user.InjectOrgID(context.Background(), "test-user")
			b := &logBatchIterator{
				batchChunkIterator: &batchChunkIterator{
					direction: logproto.FORWARD,
				},
				ctx:    ctx,
				labels: map[model.Fingerprint]string{},
			}
			it, err := b.buildHeapIterator(tc.input, from, from.Add(6*time.Millisecond), nil)
			if err != nil {
				t.Errorf("buildHeapIterator error = %v", err)
				return
			}
			req := newQuery("{foo=\"bar\"}", from, from.Add(6*time.Millisecond), nil)
			streams, _, err := iter.ReadBatch(it, req.Limit)
			_ = it.Close()
			if err != nil {
				t.Fatalf("error reading batch %s", err)
			}
			assertStream(t, tc.expected, streams.Streams)
		})
	}
}

func TestDropLabels(t *testing.T) {

	for i, tc := range []struct {
		ls       labels.Labels
		drop     []string
		expected labels.Labels
	}{
		{
			ls: labels.Labels{
				labels.Label{
					Name:  "a",
					Value: "1",
				},
				labels.Label{
					Name:  "b",
					Value: "2",
				},
				labels.Label{
					Name:  "c",
					Value: "3",
				},
			},
			drop: []string{"b"},
			expected: labels.Labels{
				labels.Label{
					Name:  "a",
					Value: "1",
				},
				labels.Label{
					Name:  "c",
					Value: "3",
				},
			},
		},
	} {
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			dropped := dropLabels(tc.ls, tc.drop...)
			require.Equal(t, tc.expected, dropped)
		})
	}
}

func Test_IsInvalidChunkError(t *testing.T) {
	tests := []struct {
		name           string
		err            error
		expectedResult bool
	}{
		{
			"invalid chunk cheksum error from cortex",
			promql.ErrStorage{Err: chunk.ErrInvalidChecksum},
			true,
		},
		{
			"invalid chunk cheksum error from loki",
			promql.ErrStorage{Err: chunkenc.ErrInvalidChecksum},
			true,
		},
		{
			"cache error",
			promql.ErrStorage{Err: errors.New("error fetching from cache")},
			false,
		},
		{
			"no error from cortex or loki",
			nil,
			false,
		},
	}
	for _, tc := range tests {
		result := isInvalidChunkError(tc.err)
		require.Equal(t, tc.expectedResult, result)
	}
}

var entry logproto.Entry

func Benchmark_store_OverlappingChunks(b *testing.B) {
	b.ReportAllocs()
	st := &store{
		cfg: Config{
			MaxChunkBatchSize: 50,
		},
		Store: newMockChunkStore(newOverlappingStreams(200, 200)),
	}
	b.ResetTimer()
	ctx := user.InjectOrgID(stats.NewContext(context.Background()), "fake")
	start := time.Now()
	for i := 0; i < b.N; i++ {
		it, err := st.SelectLogs(ctx, logql.SelectLogParams{QueryRequest: &logproto.QueryRequest{
			Selector:  `{foo="bar"}`,
			Direction: logproto.BACKWARD,
			Limit:     0,
			Shards:    nil,
			Start:     time.Unix(0, 1),
			End:       time.Unix(0, time.Now().UnixNano()),
		}})
		if err != nil {
			b.Fatal(err)
		}
		for it.Next() {
			entry = it.Entry()
		}
		if err := it.Close(); err != nil {
			b.Fatal(err)
		}
	}
	r := stats.Snapshot(ctx, time.Since(start))
	b.Log("Total chunks:" + fmt.Sprintf("%d", r.Store.TotalChunksRef))
	b.Log("Total bytes decompressed:" + fmt.Sprintf("%d", r.Store.DecompressedBytes))
}

func newOverlappingStreams(streamCount int, entryCount int) []*logproto.Stream {
	streams := make([]*logproto.Stream, streamCount)
	for i := range streams {
		streams[i] = &logproto.Stream{
			Labels:  fmt.Sprintf(`{foo="bar",id="%d"}`, i),
			Entries: make([]logproto.Entry, entryCount),
		}
		for j := range streams[i].Entries {
			streams[i].Entries[j] = logproto.Entry{
				Timestamp: time.Unix(0, int64(1+j)),
				Line:      "a very compressible log line duh",
			}
		}
	}
	return streams
}
