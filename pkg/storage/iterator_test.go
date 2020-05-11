package storage

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/user"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
)

func Test_newBatchChunkIterator(t *testing.T) {

	tests := map[string]struct {
		chunks     []*chunkenc.LazyChunk
		expected   []logproto.Stream
		matchers   string
		start, end time.Time
		direction  logproto.Direction
		batchSize  int
	}{
		"forward with overlap": {
			[]*chunkenc.LazyChunk{
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
		"forward with overlapping non-continuous entries": {
			[]*chunkenc.LazyChunk{
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
			[]*chunkenc.LazyChunk{
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
		"backward with overlapping non-continuous entries": {
			[]*chunkenc.LazyChunk{
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
			[]*chunkenc.LazyChunk{
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
			[]*chunkenc.LazyChunk{
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
	}

	for name, tt := range tests {
		tt := tt
		t.Run(name, func(t *testing.T) {
			it := newBatchChunkIterator(context.Background(), tt.chunks, tt.batchSize, newMatchers(tt.matchers), nil, newQuery("", tt.start, tt.end, tt.direction))
			streams, _, err := iter.ReadBatch(it, 1000)
			_ = it.Close()
			if err != nil {
				t.Fatalf("error reading batch %s", err)
			}

			assertStream(t, tt.expected, streams.Streams)

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
		input    []*chunkenc.LazyChunk
		expected [][]*chunkenc.LazyChunk
	}{
		{
			input: []*chunkenc.LazyChunk{
				oneThroughFour,
				two,
				three,
			},
			expected: [][]*chunkenc.LazyChunk{
				{oneThroughFour},
				{two, three},
			},
		},
		{
			input: []*chunkenc.LazyChunk{
				two,
				oneThroughFour,
				three,
			},
			expected: [][]*chunkenc.LazyChunk{
				{oneThroughFour},
				{two, three},
			},
		},
		{
			input: []*chunkenc.LazyChunk{
				two,
				two,
				three,
				three,
			},
			expected: [][]*chunkenc.LazyChunk{
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
		input    [][]*chunkenc.LazyChunk
		expected []logproto.Stream
	}{
		{
			[][]*chunkenc.LazyChunk{
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
			[][]*chunkenc.LazyChunk{
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
			it, err := buildHeapIterator(ctx, tc.input, nil, logproto.FORWARD, from, from.Add(6*time.Millisecond))
			if err != nil {
				t.Errorf("buildHeapIterator error = %v", err)
				return
			}
			req := newQuery("{foo=\"bar\"}", from, from.Add(6*time.Millisecond), logproto.FORWARD)
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
