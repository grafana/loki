package storage

import (
	"context"
	"testing"
	"time"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
)

func Test_newBatchChunkIterator(t *testing.T) {

	tests := map[string]struct {
		chunks     []*chunkenc.LazyChunk
		expected   []*logproto.Stream
		matchers   string
		start, end time.Time
		direction  logproto.Direction
	}{
		"forward with overlap": {
			[]*chunkenc.LazyChunk{
				newLazyChunk(logproto.Stream{
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
					},
				}),
				newLazyChunk(logproto.Stream{
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
					},
				}),
				newLazyChunk(logproto.Stream{
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
					},
				}),
				newLazyChunk(logproto.Stream{
					Labels: fooLabels,
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
			[]*logproto.Stream{
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
			fooLabels,
			from, from.Add(3 * time.Millisecond),
			logproto.FORWARD,
		},
		"backward with overlap": {
			[]*chunkenc.LazyChunk{
				newLazyChunk(logproto.Stream{
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
					},
				}),
				newLazyChunk(logproto.Stream{
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
					},
				}),
				newLazyChunk(logproto.Stream{
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
					},
				}),
				newLazyChunk(logproto.Stream{
					Labels: fooLabels,
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
			[]*logproto.Stream{
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
			fooLabels,
			from, from.Add(3 * time.Millisecond),
			logproto.BACKWARD,
		},
		"forward without overlap": {
			[]*chunkenc.LazyChunk{
				newLazyChunk(logproto.Stream{
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
					},
				}),
				newLazyChunk(logproto.Stream{
					Labels: fooLabels,
					Entries: []logproto.Entry{
						{
							Timestamp: from.Add(2 * time.Millisecond),
							Line:      "3",
						},
					},
				}),
				newLazyChunk(logproto.Stream{
					Labels: fooLabels,
					Entries: []logproto.Entry{
						{
							Timestamp: from.Add(3 * time.Millisecond),
							Line:      "4",
						},
					},
				}),
			},
			[]*logproto.Stream{
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
			fooLabels,
			from, from.Add(3 * time.Millisecond),
			logproto.FORWARD,
		},
		"backward without overlap": {
			[]*chunkenc.LazyChunk{
				newLazyChunk(logproto.Stream{
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
					},
				}),
				newLazyChunk(logproto.Stream{
					Labels: fooLabels,
					Entries: []logproto.Entry{
						{
							Timestamp: from.Add(2 * time.Millisecond),
							Line:      "3",
						},
					},
				}),
				newLazyChunk(logproto.Stream{
					Labels: fooLabels,
					Entries: []logproto.Entry{
						{
							Timestamp: from.Add(3 * time.Millisecond),
							Line:      "4",
						},
					},
				}),
			},
			[]*logproto.Stream{
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
			fooLabels,
			from, from.Add(3 * time.Millisecond),
			logproto.BACKWARD,
		},
	}

	for name, tt := range tests {
		tt := tt
		t.Run(name, func(t *testing.T) {
			it := newBatchChunkIterator(context.Background(), tt.chunks, 2, newMatchers(tt.matchers), nil, newQuery("", tt.start, tt.end, tt.direction))
			streams, _, err := iter.ReadBatch(it, 1000)
			_ = it.Close()
			if err != nil {
				t.Fatalf("error reading batch %s", err)
			}

			assertStream(t, tt.expected, streams.Streams)

		})
	}

}
