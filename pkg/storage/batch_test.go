package storage

import (
	"context"
	"fmt"
	"testing"
	"time"
	"unsafe"

	"github.com/grafana/dskit/user"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/chunkenc"
	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/querier/testutil"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/util"
)

var NilMetrics = NewChunkMetrics(nil, 0)

func Test_batchIterSafeStart(t *testing.T) {
	stream := logproto.Stream{
		Labels: fooLabelsWithName.String(),
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
	periodConfig := config.PeriodConfig{
		From:   config.DayTime{Time: 0},
		Schema: "v11",
	}

	chunkfmt, headfmt, err := periodConfig.ChunkFormat()
	require.NoError(t, err)

	chks := []*LazyChunk{
		newLazyChunk(chunkfmt, headfmt, stream),
	}

	s := config.SchemaConfig{
		Configs: []config.PeriodConfig{
			periodConfig,
		},
	}

	batch := newBatchChunkIterator(context.Background(), s, chks, 1, logproto.FORWARD, from, from.Add(4*time.Millisecond), NilMetrics, []*labels.Matcher{}, nil)

	// if it was started already, we should see a panic before this
	time.Sleep(time.Millisecond)

	// ensure idempotency
	batch.Start()
	batch.Start()

	require.NotNil(t, batch.Next())
}

func Test_newLogBatchChunkIterator(t *testing.T) {
	periodConfigs := []config.PeriodConfig{
		{
			From:   config.DayTime{Time: 0},
			Schema: "v11",
		},
		{
			From:   config.DayTime{Time: 0},
			Schema: "v12",
		},
		{
			From:   config.DayTime{Time: 0},
			Schema: "v13",
		},
	}

	type testCase struct {
		chunks     []*LazyChunk
		expected   []logproto.Stream
		matchers   string
		start, end time.Time
		direction  logproto.Direction
		batchSize  int
	}

	var tests map[string]testCase

	for _, periodConfig := range periodConfigs {
		chunkfmt, headfmt, err := periodConfig.ChunkFormat()
		require.NoError(t, err)

		tests = map[string]testCase{
			"forward with overlap": {
				[]*LazyChunk{
					newLazyChunk(chunkfmt, headfmt, logproto.Stream{
						Labels: fooLabelsWithName.String(),
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
					newLazyChunk(chunkfmt, headfmt, logproto.Stream{
						Labels: fooLabelsWithName.String(),
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
					newLazyChunk(chunkfmt, headfmt, logproto.Stream{
						Labels: fooLabelsWithName.String(),
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
					newLazyChunk(chunkfmt, headfmt, logproto.Stream{
						Labels: fooLabelsWithName.String(),
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
					newLazyChunk(chunkfmt, headfmt, logproto.Stream{
						Labels: fooLabelsWithName.String(),
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
					newLazyChunk(chunkfmt, headfmt, logproto.Stream{
						Labels: fooLabelsWithName.String(),
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
						Labels: fooLabels.String(),
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
				fooLabelsWithName.String(),
				from, from.Add(4 * time.Millisecond),
				logproto.FORWARD,
				2,
			},
			"forward all overlap and all chunks have a from time less than query from time": {
				[]*LazyChunk{
					newLazyChunk(chunkfmt, headfmt, logproto.Stream{
						Labels: fooLabelsWithName.String(),
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
					newLazyChunk(chunkfmt, headfmt, logproto.Stream{
						Labels: fooLabelsWithName.String(),
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
					newLazyChunk(chunkfmt, headfmt, logproto.Stream{
						Labels: fooLabelsWithName.String(),
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
					newLazyChunk(chunkfmt, headfmt, logproto.Stream{
						Labels: fooLabelsWithName.String(),
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
					newLazyChunk(chunkfmt, headfmt, logproto.Stream{
						Labels: fooLabelsWithName.String(),
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
					newLazyChunk(chunkfmt, headfmt, logproto.Stream{
						Labels: fooLabelsWithName.String(),
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
						Labels: fooLabels.String(),
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
				fooLabelsWithName.String(),
				from.Add(1 * time.Millisecond), from.Add(5 * time.Millisecond),
				logproto.FORWARD,
				2,
			},
			"forward with overlapping non-continuous entries": {
				[]*LazyChunk{
					newLazyChunk(chunkfmt, headfmt, logproto.Stream{
						Labels: fooLabelsWithName.String(),
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
					newLazyChunk(chunkfmt, headfmt, logproto.Stream{
						Labels: fooLabelsWithName.String(),
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
					newLazyChunk(chunkfmt, headfmt, logproto.Stream{
						Labels: fooLabelsWithName.String(),
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
					newLazyChunk(chunkfmt, headfmt, logproto.Stream{
						Labels: fooLabelsWithName.String(),
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
						Labels: fooLabels.String(),
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
				fooLabelsWithName.String(),
				from, from.Add(3 * time.Millisecond),
				logproto.FORWARD,
				2,
			},
			"backward with overlap": {
				[]*LazyChunk{
					newLazyChunk(chunkfmt, headfmt, logproto.Stream{
						Labels: fooLabelsWithName.String(),
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
					newLazyChunk(chunkfmt, headfmt, logproto.Stream{
						Labels: fooLabelsWithName.String(),
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
					newLazyChunk(chunkfmt, headfmt, logproto.Stream{
						Labels: fooLabelsWithName.String(),
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
					newLazyChunk(chunkfmt, headfmt, logproto.Stream{
						Labels: fooLabelsWithName.String(),
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
					newLazyChunk(chunkfmt, headfmt, logproto.Stream{
						Labels: fooLabelsWithName.String(),
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
					newLazyChunk(chunkfmt, headfmt, logproto.Stream{
						Labels: fooLabelsWithName.String(),
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
						Labels: fooLabels.String(),
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
				fooLabelsWithName.String(),
				from, from.Add(4 * time.Millisecond),
				logproto.BACKWARD,
				2,
			},
			"backward all overlap and all chunks have a through time greater than query through time": {
				[]*LazyChunk{
					newLazyChunk(chunkfmt, headfmt, logproto.Stream{
						Labels: fooLabelsWithName.String(),
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
					newLazyChunk(chunkfmt, headfmt, logproto.Stream{
						Labels: fooLabelsWithName.String(),
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
					newLazyChunk(chunkfmt, headfmt, logproto.Stream{
						Labels: fooLabelsWithName.String(),
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
					newLazyChunk(chunkfmt, headfmt, logproto.Stream{
						Labels: fooLabelsWithName.String(),
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
					newLazyChunk(chunkfmt, headfmt, logproto.Stream{
						Labels: fooLabelsWithName.String(),
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
					newLazyChunk(chunkfmt, headfmt, logproto.Stream{
						Labels: fooLabelsWithName.String(),
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
						Labels: fooLabels.String(),
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
				fooLabelsWithName.String(),
				from, from.Add(4 * time.Millisecond),
				logproto.BACKWARD,
				2,
			},
			"backward with overlapping non-continuous entries": {
				[]*LazyChunk{
					newLazyChunk(chunkfmt, headfmt, logproto.Stream{
						Labels: fooLabelsWithName.String(),
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
					newLazyChunk(chunkfmt, headfmt, logproto.Stream{
						Labels: fooLabelsWithName.String(),
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
					newLazyChunk(chunkfmt, headfmt, logproto.Stream{
						Labels: fooLabelsWithName.String(),
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
					newLazyChunk(chunkfmt, headfmt, logproto.Stream{
						Labels: fooLabelsWithName.String(),
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
						Labels: fooLabels.String(),
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
				fooLabelsWithName.String(),
				from, from.Add(8 * time.Millisecond),
				logproto.BACKWARD,
				2,
			},
			"forward without overlap": {
				[]*LazyChunk{
					newLazyChunk(chunkfmt, headfmt, logproto.Stream{
						Labels: fooLabelsWithName.String(),
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
					newLazyChunk(chunkfmt, headfmt, logproto.Stream{
						Labels: fooLabelsWithName.String(),
						Entries: []logproto.Entry{
							{
								Timestamp: from.Add(2 * time.Millisecond),
								Line:      "3",
							},
						},
					}),
					newLazyChunk(chunkfmt, headfmt, logproto.Stream{
						Labels: fooLabelsWithName.String(),
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
						Labels: fooLabels.String(),
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
				fooLabelsWithName.String(),
				from, from.Add(3 * time.Millisecond),
				logproto.FORWARD,
				2,
			},
			"backward without overlap": {
				[]*LazyChunk{
					newLazyChunk(chunkfmt, headfmt, logproto.Stream{
						Labels: fooLabelsWithName.String(),
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
					newLazyChunk(chunkfmt, headfmt, logproto.Stream{
						Labels: fooLabelsWithName.String(),
						Entries: []logproto.Entry{
							{
								Timestamp: from.Add(2 * time.Millisecond),
								Line:      "3",
							},
						},
					}),
					newLazyChunk(chunkfmt, headfmt, logproto.Stream{
						Labels: fooLabelsWithName.String(),
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
						Labels: fooLabels.String(),
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
				fooLabelsWithName.String(),
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
					newLazyChunk(chunkfmt, headfmt, logproto.Stream{
						Labels: fooLabelsWithName.String(),
						Entries: []logproto.Entry{
							{
								Timestamp: from,
								Line:      "1",
							},
						},
					}),
					newLazyChunk(chunkfmt, headfmt, logproto.Stream{
						Labels: fooLabelsWithName.String(),
						Entries: []logproto.Entry{
							{
								Timestamp: from,
								Line:      "1",
							},
						},
					}),
					newLazyChunk(chunkfmt, headfmt, logproto.Stream{
						Labels: fooLabelsWithName.String(),
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
					newLazyChunk(chunkfmt, headfmt, logproto.Stream{
						Labels: fooLabelsWithName.String(),
						Entries: []logproto.Entry{
							{
								Timestamp: from.Add(time.Millisecond),
								Line:      "2",
							},
						},
					}),
					newLazyChunk(chunkfmt, headfmt, logproto.Stream{
						Labels: fooLabelsWithName.String(),
						Entries: []logproto.Entry{
							{
								Timestamp: from.Add(time.Millisecond),
								Line:      "2",
							},
						},
					}),
					newLazyChunk(chunkfmt, headfmt, logproto.Stream{
						Labels: fooLabelsWithName.String(),
						Entries: []logproto.Entry{
							{
								Timestamp: from.Add(time.Millisecond),
								Line:      "2",
							},
						},
					}),
					newLazyChunk(chunkfmt, headfmt, logproto.Stream{
						Labels: fooLabelsWithName.String(),
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
						Labels: fooLabels.String(),
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
				fooLabelsWithName.String(),
				from, from.Add(4 * time.Millisecond),
				logproto.FORWARD,
				1,
			},
		}

	}

	schemaConfigs := []config.SchemaConfig{
		{
			Configs: []config.PeriodConfig{periodConfigs[0]},
		},
		{
			Configs: []config.PeriodConfig{periodConfigs[1]},
		},
		{
			Configs: []config.PeriodConfig{periodConfigs[2]},
		},
	}

	for _, schemaConfig := range schemaConfigs {
		s := schemaConfig
		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				it, err := newLogBatchIterator(context.Background(), s, NilMetrics, tt.chunks, tt.batchSize, newMatchers(tt.matchers), log.NewNoopPipeline(), tt.direction, tt.start, tt.end, nil)
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
}

func TestNewTimestampFirstSampleBatchIterator(t *testing.T) {
	periodConfig := config.PeriodConfig{
		From:   config.DayTime{Time: 0},
		Schema: "v11",
	}

	chunkfmt, headfmt, err := periodConfig.ChunkFormat()
	require.NoError(t, err)

	tests := map[string]struct {
		chunks     []*LazyChunk
		expected   []logproto.Series
		matchers   string
		start, end time.Time
		batchSize  int
	}{
		"forward with overlap": {
			[]*LazyChunk{
				newLazyChunk(chunkfmt, headfmt, logproto.Stream{
					Labels: fooLabelsWithName.String(),
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
				newLazyChunk(chunkfmt, headfmt, logproto.Stream{
					Labels: fooLabelsWithName.String(),
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
				newLazyChunk(chunkfmt, headfmt, logproto.Stream{
					Labels: fooLabelsWithName.String(),
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
				newLazyChunk(chunkfmt, headfmt, logproto.Stream{
					Labels: fooLabelsWithName.String(),
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
				newLazyChunk(chunkfmt, headfmt, logproto.Stream{
					Labels: fooLabelsWithName.String(),
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
				newLazyChunk(chunkfmt, headfmt, logproto.Stream{
					Labels: fooLabelsWithName.String(),
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
					Labels: fooLabels.String(),
					Samples: []logproto.Sample{
						{
							Timestamp: from.UnixNano(),
							Hash:      util.UniqueSampleHash(fooLabels.String(), unsafeGetBytes("1")),
							Value:     1.,
						},
						{
							Timestamp: from.Add(time.Millisecond).UnixNano(),
							Hash:      util.UniqueSampleHash(fooLabels.String(), unsafeGetBytes("2")),
							Value:     1.,
						},
						{
							Timestamp: from.Add(2 * time.Millisecond).UnixNano(),
							Hash:      util.UniqueSampleHash(fooLabels.String(), unsafeGetBytes("3")),
							Value:     1.,
						},
						{
							Timestamp: from.Add(3 * time.Millisecond).UnixNano(),
							Hash:      util.UniqueSampleHash(fooLabels.String(), unsafeGetBytes("4")),
							Value:     1.,
						},
					},
				},
			},
			fooLabelsWithName.String(),
			from, from.Add(4 * time.Millisecond),
			2,
		},
		"forward with overlapping non-continuous entries": {
			[]*LazyChunk{
				newLazyChunk(chunkfmt, headfmt, logproto.Stream{
					Labels: fooLabelsWithName.String(),
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
				newLazyChunk(chunkfmt, headfmt, logproto.Stream{
					Labels: fooLabelsWithName.String(),
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
				newLazyChunk(chunkfmt, headfmt, logproto.Stream{
					Labels: fooLabelsWithName.String(),
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
				newLazyChunk(chunkfmt, headfmt, logproto.Stream{
					Labels: fooLabelsWithName.String(),
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
					Labels: fooLabels.String(),
					Samples: []logproto.Sample{
						{
							Timestamp: from.UnixNano(),
							Hash:      util.UniqueSampleHash(fooLabels.String(), unsafeGetBytes("1")),
							Value:     1.,
						},
						{
							Timestamp: from.Add(time.Millisecond).UnixNano(),
							Hash:      util.UniqueSampleHash(fooLabels.String(), unsafeGetBytes("2")),
							Value:     1.,
						},
						{
							Timestamp: from.Add(2 * time.Millisecond).UnixNano(),
							Hash:      util.UniqueSampleHash(fooLabels.String(), unsafeGetBytes("3")),
							Value:     1.,
						},
					},
				},
			},
			fooLabelsWithName.String(),
			from, from.Add(3 * time.Millisecond),
			2,
		},
		"forward last chunk boundaries equal to end": {
			[]*LazyChunk{
				newLazyChunk(chunkfmt, headfmt, logproto.Stream{
					Labels: fooLabelsWithName.String(),
					Entries: []logproto.Entry{
						{
							Timestamp: time.Unix(1, 0),
							Line:      "1",
						},
						{
							Timestamp: time.Unix(2, 0),
							Line:      "2",
						},
					},
				}),
				newLazyChunk(chunkfmt, headfmt, logproto.Stream{
					Labels: fooLabelsWithName.String(),
					Entries: []logproto.Entry{
						{
							Timestamp: time.Unix(2, 0),
							Line:      "2",
						},
						{
							Timestamp: time.Unix(3, 0),
							Line:      "3",
						},
					},
				}),
				newLazyChunk(chunkfmt, headfmt, logproto.Stream{
					Labels: fooLabelsWithName.String(),
					Entries: []logproto.Entry{
						{
							Timestamp: time.Unix(3, 0),
							Line:      "3",
						},
						{
							Timestamp: time.Unix(4, 0),
							Line:      "4",
						},
					},
				}),
			},
			[]logproto.Series{
				{
					Labels: fooLabels.String(),
					Samples: []logproto.Sample{
						{
							Timestamp: time.Unix(1, 0).UnixNano(),
							Hash:      util.UniqueSampleHash(fooLabels.String(), unsafeGetBytes("1")),
							Value:     1.,
						},
						{
							Timestamp: time.Unix(2, 0).UnixNano(),
							Hash:      util.UniqueSampleHash(fooLabels.String(), unsafeGetBytes("2")),
							Value:     1.,
						},
					},
				},
			},
			fooLabelsWithName.String(),
			time.Unix(1, 0), time.Unix(3, 0),
			2,
		},
		"forward last chunk boundaries equal to end and start": {
			[]*LazyChunk{
				newLazyChunk(chunkfmt, headfmt, logproto.Stream{
					Labels: fooLabelsWithName.String(),
					Entries: []logproto.Entry{
						{
							Timestamp: time.Unix(1, 0),
							Line:      "1",
						},
						{
							Timestamp: time.Unix(1, 0),
							Line:      "2",
						},
					},
				}),
				newLazyChunk(chunkfmt, headfmt, logproto.Stream{
					Labels: fooLabelsWithName.String(),
					Entries: []logproto.Entry{
						{
							Timestamp: time.Unix(1, 0),
							Line:      "2",
						},
						{
							Timestamp: time.Unix(2, 0),
							Line:      "3",
						},
					},
				}),
			},
			[]logproto.Series{
				{
					Labels: fooLabels.String(),
					Samples: []logproto.Sample{
						{
							Timestamp: time.Unix(1, 0).UnixNano(),
							Hash:      util.UniqueSampleHash(fooLabels.String(), unsafeGetBytes("1")),
							Value:     1.,
						},
						{
							Timestamp: time.Unix(1, 0).UnixNano(),
							Hash:      util.UniqueSampleHash(fooLabels.String(), unsafeGetBytes("2")),
							Value:     1.,
						},
					},
				},
			},
			fooLabelsWithName.String(),
			time.Unix(1, 0), time.Unix(1, 0),
			2,
		},
		"forward without overlap": {
			[]*LazyChunk{
				newLazyChunk(chunkfmt, headfmt, logproto.Stream{
					Labels: fooLabelsWithName.String(),
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
				newLazyChunk(chunkfmt, headfmt, logproto.Stream{
					Labels: fooLabelsWithName.String(),
					Entries: []logproto.Entry{
						{
							Timestamp: from.Add(2 * time.Millisecond),
							Line:      "3",
						},
					},
				}),
				newLazyChunk(chunkfmt, headfmt, logproto.Stream{
					Labels: fooLabelsWithName.String(),
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
					Labels: fooLabels.String(),
					Samples: []logproto.Sample{
						{
							Timestamp: from.UnixNano(),
							Hash:      util.UniqueSampleHash(fooLabels.String(), unsafeGetBytes("1")),
							Value:     1.,
						},
						{
							Timestamp: from.Add(time.Millisecond).UnixNano(),
							Hash:      util.UniqueSampleHash(fooLabels.String(), unsafeGetBytes("2")),
							Value:     1.,
						},
						{
							Timestamp: from.Add(2 * time.Millisecond).UnixNano(),
							Hash:      util.UniqueSampleHash(fooLabels.String(), unsafeGetBytes("3")),
							Value:     1.,
						},
					},
				},
			},
			fooLabelsWithName.String(),
			from, from.Add(3 * time.Millisecond),
			2,
		},
	}

	s := config.SchemaConfig{
		Configs: []config.PeriodConfig{
			{
				From:   config.DayTime{Time: 0},
				Schema: "v11",
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ex, err := log.NewLineSampleExtractor(log.CountExtractor, nil, nil, false, false)
			require.NoError(t, err)

			it, err := newTimestampFirstSampleBatchIterator(
				context.Background(),
				s,
				NilMetrics,
				tt.chunks,
				tt.batchSize,
				newMatchers(tt.matchers),
				tt.start,
				tt.end,
				nil,
				ex,
			)
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

func TestNewTimestampFirstSampleBatchIterator_ErrorHandling(t *testing.T) {
	t.Run("At() stays valued after a clean terminal exhaustion", func(t *testing.T) {
		last := logproto.Sample{Timestamp: from.UnixNano(), Value: 1}
		chunks := []*LazyChunk{newFakeLazyChunk(1, from, from.Add(time.Minute), &fakeSampleIterator{samples: []logproto.Sample{last}})}
		it := newFakeSampleBatchIterator(t, 10, chunks...)

		require.True(t, it.Next())
		require.False(t, it.Next(), "no chunks remain, so the batch iterator is exhausted after the one sample")
		require.Equal(t, last, it.At(), "curr is left for Close, not closed or cleared, so At still returns its last value")
		require.NoError(t, it.Close())
	})

	t.Run("close error on a cleanly drained iterator surfaces through Close, not Err", func(t *testing.T) {
		closeBoom := errors.New("close boom")
		curr := &fakeSampleIterator{closeErr: closeBoom}
		chunks := []*LazyChunk{newFakeLazyChunk(1, from, from.Add(time.Minute), curr)}
		it := newFakeSampleBatchIterator(t, 10, chunks...)

		require.False(t, it.Next(), "no samples, so the batch iterator is exhausted immediately")
		require.NoError(t, it.Err(), "a close error is not a read error")
		require.ErrorIs(t, it.Close(), closeBoom)
		require.Equal(t, 1, curr.closed, "curr must not be closed a second time by the later top-level Close")
	})

	t.Run("read error on iterator surfaces through Err and stops iteration", func(t *testing.T) {
		wantErr := errors.New("read boom")
		curr := &fakeSampleIterator{err: wantErr}
		chunks := []*LazyChunk{newFakeLazyChunk(1, from, from.Add(time.Minute), curr)}
		it := newFakeSampleBatchIterator(t, 10, chunks...)

		require.False(t, it.Next())
		require.ErrorIs(t, it.Err(), wantErr)
		require.NoError(t, it.Close(), "a read error must not surface a second time as a close error")
		require.Equal(t, 1, curr.closed)
	})

	t.Run("read error on iterator stops iteration, without pulling in a later batch", func(t *testing.T) {
		wantErr := errors.New("read boom")
		errored := &fakeSampleIterator{err: wantErr}
		healthy := &fakeSampleIterator{samples: []logproto.Sample{{Timestamp: from.Add(time.Hour).UnixNano(), Value: 2}}}
		chunks := []*LazyChunk{
			newFakeLazyChunk(1, from, from.Add(time.Minute), errored),
			newFakeLazyChunk(1, from.Add(time.Hour), from.Add(time.Hour+time.Minute), healthy),
		}
		it := newFakeSampleBatchIterator(t, 1, chunks...) // batchSize 1: one chunk per batch

		require.False(t, it.Next(), "must stop immediately rather than advance into the next batch")
		require.ErrorIs(t, it.Err(), wantErr)
		require.NoError(t, it.Close())
		require.Equal(t, 1, errored.closed)
		require.Zero(t, healthy.i, "the next batch's chunk must never be decoded")
		require.Zero(t, healthy.closed, "the next batch's chunk must never be touched")
	})

	t.Run("read and close errors on iterator surface separately, through Err and Close", func(t *testing.T) {
		readErr := errors.New("read boom")
		closeErr := errors.New("close boom")
		curr := &fakeSampleIterator{err: readErr, closeErr: closeErr}
		chunks := []*LazyChunk{newFakeLazyChunk(1, from, from.Add(time.Minute), curr)}
		it := newFakeSampleBatchIterator(t, 10, chunks...)

		require.False(t, it.Next())
		require.ErrorIs(t, it.Err(), readErr)
		require.ErrorIs(t, it.Close(), closeErr)
		require.Equal(t, 1, curr.closed)
	})

	t.Run("Close before any Next call is a no-op", func(t *testing.T) {
		curr := &fakeSampleIterator{samples: []logproto.Sample{{Timestamp: from.UnixNano(), Value: 1}}}
		chunks := []*LazyChunk{newFakeLazyChunk(1, from, from.Add(time.Minute), curr)}
		it := newFakeSampleBatchIterator(t, 10, chunks...)

		require.NoError(t, it.Close())
		require.Zero(t, curr.closed, "curr is only built by Next, so Close before any Next call has nothing to close")
	})

	t.Run("a close error from a iterator Next rotated past survives to the final Close", func(t *testing.T) {
		closeBoom := errors.New("close boom")
		first := &fakeSampleIterator{closeErr: closeBoom} // no samples: exhausts cleanly right away
		second := &fakeSampleIterator{samples: []logproto.Sample{{Timestamp: from.Add(time.Hour).UnixNano(), Value: 2}}}
		chunks := []*LazyChunk{
			newFakeLazyChunk(1, from, from.Add(time.Minute), first),
			newFakeLazyChunk(1, from.Add(time.Hour), from.Add(time.Hour+time.Minute), second),
		}
		it := newFakeSampleBatchIterator(t, 1, chunks...)

		require.True(t, it.Next(), "must rotate past the first curr into the second chunk's data")
		require.ErrorIs(t, it.Close(), closeBoom)
	})
}

type fakeSampleIterator struct {
	samples  []logproto.Sample
	i        int
	err      error
	closeErr error
	closed   int
}

func (it *fakeSampleIterator) Next() bool {
	if it.i < len(it.samples) {
		it.i++
		return true
	}
	return false
}

func (it *fakeSampleIterator) At() logproto.Sample {
	if it.i == 0 {
		return logproto.Sample{}
	}
	return it.samples[it.i-1]
}

func (it *fakeSampleIterator) Err() error {
	if it.i >= len(it.samples) {
		return it.err
	}
	return nil
}

func (it *fakeSampleIterator) Close() error {
	it.closed++
	return it.closeErr
}

func (it *fakeSampleIterator) Labels() string     { return "" }
func (it *fakeSampleIterator) StreamHash() uint64 { return 0 }

type fakeChunk struct {
	chunkenc.Chunk
	block *fakeBlock
}

func (c *fakeChunk) Blocks(time.Time, time.Time) []chunkenc.Block {
	return []chunkenc.Block{c.block}
}

func newFakeLazyChunk(fp model.Fingerprint, from, through time.Time, it iter.SampleIterator) *LazyChunk {
	block := &fakeBlock{mint: from.UnixNano(), maxt: through.UnixNano(), it: it}
	data := chunkenc.NewFacade(&fakeChunk{Chunk: chunkenc.NewDumbChunk(), block: block}, 0, 0)
	c := chunk.NewChunk("fake", fp, fooLabelsWithName, data, model.TimeFromUnixNano(from.UnixNano()), model.TimeFromUnixNano(through.UnixNano()))
	return &LazyChunk{IsValid: true, Chunk: c}
}

func newFakeSampleBatchIterator(t *testing.T, batchSize int, chunks ...*LazyChunk) iter.SampleIterator {
	ex, err := log.NewLineSampleExtractor(log.CountExtractor, nil, nil, false, false)
	require.NoError(t, err)

	it, err := newTimestampFirstSampleBatchIterator(
		context.Background(),
		config.SchemaConfig{},
		NilMetrics,
		chunks,
		batchSize,
		nil,
		from.Add(-time.Hour), from.Add(3*time.Hour),
		nil,
		ex,
	)
	require.NoError(t, err)
	return it
}

func TestPartitionOverlappingchunks(t *testing.T) {
	periodConfig := config.PeriodConfig{
		From:   config.DayTime{Time: 0},
		Schema: "v11",
	}

	chunkfmt, headfmt, err := periodConfig.ChunkFormat()
	require.NoError(t, err)

	var (
		oneThroughFour = newLazyChunk(chunkfmt, headfmt, logproto.Stream{
			Labels: fooLabelsWithName.String(),
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
		two = newLazyChunk(chunkfmt, headfmt, logproto.Stream{
			Labels: fooLabelsWithName.String(),
			Entries: []logproto.Entry{
				{
					Timestamp: from.Add(1 * time.Millisecond),
					Line:      "2",
				},
			},
		})
		three = newLazyChunk(chunkfmt, headfmt, logproto.Stream{
			Labels: fooLabelsWithName.String(),
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

	periodConfig := config.PeriodConfig{
		From:   config.DayTime{Time: 0},
		Schema: "v11",
	}

	chunkfmt, headfmt, err := periodConfig.ChunkFormat()
	require.NoError(t, err)

	var (
		firstChunk = newLazyChunk(chunkfmt, headfmt, logproto.Stream{
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
		secondChunk = newLazyInvalidChunk(chunkfmt, headfmt, logproto.Stream{
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
		thirdChunk = newLazyChunk(chunkfmt, headfmt, logproto.Stream{
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
				ctx:      ctx,
				pipeline: log.NewNoopPipeline(),
			}
			it, err := b.buildMergeIterator(tc.input, from, from.Add(6*time.Millisecond), b.pipeline.ForStream(labels.New(labels.Label{Name: "foo", Value: "bar"})), nil)
			if err != nil {
				t.Errorf("buildMergeIterator error = %v", err)
				return
			}
			req := newQuery("{foo=\"bar\"}", from, from.Add(6*time.Millisecond), nil, nil)
			streams, _, err := iter.ReadBatch(it, req.Limit)
			_ = it.Close()
			if err != nil {
				t.Fatalf("error reading batch %s", err)
			}
			assertStream(t, tc.expected, streams.Streams)
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
			promql.ErrStorage{Err: chunkenc.ErrInvalidChecksum},
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

func TestBatchCancel(t *testing.T) {
	periodConfig := config.PeriodConfig{
		From:   config.DayTime{Time: 0},
		Schema: "v11",
	}

	chunkfmt, headfmt, err := periodConfig.ChunkFormat()
	require.NoError(t, err)

	createChunk := func(from time.Time) *LazyChunk {
		return newLazyChunk(chunkfmt, headfmt, logproto.Stream{
			Labels: fooLabelsWithName.String(),
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
		})
	}
	chunks := []*LazyChunk{
		createChunk(from), createChunk(from.Add(10 * time.Millisecond)), createChunk(from.Add(30 * time.Millisecond)),
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s := config.SchemaConfig{
		Configs: []config.PeriodConfig{
			{
				From:   config.DayTime{Time: 0},
				Schema: "v11",
			},
		},
	}

	it, err := newLogBatchIterator(ctx, s, NilMetrics, chunks, 1, newMatchers(fooLabels.String()), log.NewNoopPipeline(), logproto.FORWARD, from, time.Now(), nil)
	require.NoError(t, err)
	defer require.NoError(t, it.Close())
	//nolint:revive
	for it.Next() {
	}
	require.Equal(t, context.Canceled, it.Err())
}

var entry logproto.Entry

func Benchmark_store_OverlappingChunks(b *testing.B) {
	periodConfig := config.PeriodConfig{
		From:   config.DayTime{Time: 0},
		Schema: "v11",
	}

	chunkfmt, headfmt, err := periodConfig.ChunkFormat()
	require.NoError(b, err)

	b.ReportAllocs()
	st := &LokiStore{
		chunkMetrics: NilMetrics,
		cfg: Config{
			MaxChunkBatchSize: 50,
		},
		Store: newMockChunkStore(chunkfmt, headfmt, newOverlappingStreams(200, 200)),
	}
	b.ResetTimer()
	statsCtx, ctx := stats.NewContext(user.InjectOrgID(context.Background(), "fake"))
	start := time.Now()
	for i := 0; i < b.N; i++ {
		it, err := st.SelectLogs(ctx, logql.SelectLogParams{QueryRequest: &logproto.QueryRequest{
			Selector:  `{foo="bar"}`,
			Direction: logproto.BACKWARD,
			Limit:     0,
			Shards:    nil,
			Start:     time.Unix(0, 1),
			End:       time.Unix(0, time.Now().UnixNano()),
			Plan:      testutil.MustPlan(`{foo="bar"}`),
		}})
		if err != nil {
			b.Fatal(err)
		}
		for it.Next() {
			entry = it.At()
		}
		if err := it.Close(); err != nil {
			b.Fatal(err)
		}
	}
	r := statsCtx.Result(time.Since(start), 0, 0)
	b.Log("Total chunks:" + fmt.Sprintf("%d", r.TotalChunksRef()))
	b.Log("Total bytes decompressed:" + fmt.Sprintf("%d", r.TotalDecompressedBytes()))
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

func unsafeGetBytes(s string) []byte {
	return unsafe.Slice(unsafe.StringData(s), len(s)) // #nosec G103 -- we know the string is not mutated -- nosemgrep: use-of-unsafe-block
}
