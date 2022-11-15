package storage

import (
	"context"
	_ "net/http/pprof"
	"testing"
	"time"

	"github.com/cespare/xxhash/v2"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/user"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/storage/chunk"
)

func Test_store_PostFilter_SelectLogs(t *testing.T) {
	tests := []struct {
		name     string
		req      *logproto.QueryRequest
		expected []logproto.Stream
	}{
		{
			"all",
			newQuery("{foo=~\"ba.*\"}", from, from.Add(6*time.Millisecond), nil, nil),
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
							Timestamp: from.Add(3 * time.Millisecond),
							Line:      "4",
						},
						{
							Timestamp: from.Add(4 * time.Millisecond),
							Line:      "5",
						},
						{
							Timestamp: from.Add(5 * time.Millisecond),
							Line:      "6",
						},
					},
				},
				{
					Labels: "{foo=\"bazz\"}",
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
						{
							Timestamp: from.Add(4 * time.Millisecond),
							Line:      "5",
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
			"filter regex",
			newQuery("{foo=~\"ba.*\"} |~ \"1|2|3\" !~ \"2|3\"", from, from.Add(6*time.Millisecond), nil, nil),
			[]logproto.Stream{
				{
					Labels: "{foo=\"bar\"}",
					Entries: []logproto.Entry{
						{
							Timestamp: from,
							Line:      "1",
						},
					},
				},
				{
					Labels: "{foo=\"bazz\"}",
					Entries: []logproto.Entry{
						{
							Timestamp: from,
							Line:      "1",
						},
					},
				},
			},
		},
		{
			"filter matcher",
			newQuery("{foo=\"bar\"}", from, from.Add(6*time.Millisecond), nil, nil),
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
							Timestamp: from.Add(3 * time.Millisecond),
							Line:      "4",
						},
						{
							Timestamp: from.Add(4 * time.Millisecond),
							Line:      "5",
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
			"filter time",
			newQuery("{foo=~\"ba.*\"}", from, from.Add(time.Millisecond), nil, nil),
			[]logproto.Stream{
				{
					Labels: "{foo=\"bar\"}",
					Entries: []logproto.Entry{
						{
							Timestamp: from,
							Line:      "1",
						},
					},
				},
				{
					Labels: "{foo=\"bazz\"}",
					Entries: []logproto.Entry{
						{
							Timestamp: from,
							Line:      "1",
						},
					},
				},
			},
		},
		{
			"delete covers whole time range",
			newQuery(
				"{foo=~\"ba.*\"}",
				from,
				from.Add(6*time.Millisecond),
				nil,
				[]*logproto.Delete{
					{
						Selector: `{foo="bar"}`,
						Start:    from.Add(-1 * time.Millisecond).UnixNano(),
						End:      from.Add(7 * time.Millisecond).UnixNano(),
					},
					{
						Selector: `{foo="bazz"} |= "6"`,
						Start:    from.Add(-1 * time.Millisecond).UnixNano(),
						End:      from.Add(7 * time.Millisecond).UnixNano(),
					},
				}),
			[]logproto.Stream{
				{
					Labels: "{foo=\"bazz\"}",
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
						{
							Timestamp: from.Add(4 * time.Millisecond),
							Line:      "5",
						},
					},
				},
			},
		},
		{
			"delete covers partial time range",
			newQuery(
				"{foo=~\"ba.*\"}",
				from,
				from.Add(6*time.Millisecond),
				nil,
				[]*logproto.Delete{
					{
						Selector: `{foo="bar"}`,
						Start:    from.Add(-1 * time.Millisecond).UnixNano(),
						End:      from.Add(3 * time.Millisecond).UnixNano(),
					},
					{
						Selector: `{foo="bazz"} |= "2"`,
						Start:    from.Add(-1 * time.Millisecond).UnixNano(),
						End:      from.Add(3 * time.Millisecond).UnixNano(),
					},
				}),
			[]logproto.Stream{
				{
					Labels: "{foo=\"bar\"}",
					Entries: []logproto.Entry{
						{
							Timestamp: from.Add(4 * time.Millisecond),
							Line:      "5",
						},
						{
							Timestamp: from.Add(5 * time.Millisecond),
							Line:      "6",
						},
					},
				},
				{
					Labels: "{foo=\"bazz\"}",
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
						{
							Timestamp: from.Add(4 * time.Millisecond),
							Line:      "5",
						},
						{
							Timestamp: from.Add(5 * time.Millisecond),
							Line:      "6",
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &store{
				Store: storeFixture,
				cfg: Config{
					MaxChunkBatchSize: 10,
				},
				chunkMetrics: NilMetrics,
			}

			s.SetPostFetcherChunkFilterer(NewRequestPostFetcherChunkFiltererForRequest(10))

			ctx = user.InjectOrgID(context.Background(), "test-user")
			it, err := s.SelectLogs(ctx, logql.SelectLogParams{QueryRequest: tt.req})
			if err != nil {
				t.Errorf("store.LazyQuery() error = %v", err)
				return
			}

			streams, _, err := iter.ReadBatch(it, tt.req.Limit)
			_ = it.Close()
			if err != nil {
				t.Fatalf("error reading batch %s", err)
			}
			assertStream(t, tt.expected, streams.Streams)
		})
	}
}

func Test_store_PostFilter_SelectSample(t *testing.T) {
	tests := []struct {
		name     string
		req      *logproto.SampleQueryRequest
		expected []logproto.Series
	}{
		{
			"all",
			newSampleQuery("count_over_time({foo=~\"ba.*\"}!=\"1234567\" [5m])", from, from.Add(6*time.Millisecond), nil),
			[]logproto.Series{
				{
					Labels: "{foo=\"bar\"}",
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
						{
							Timestamp: from.Add(4 * time.Millisecond).UnixNano(),
							Hash:      xxhash.Sum64String("5"),
							Value:     1.,
						},
						{
							Timestamp: from.Add(5 * time.Millisecond).UnixNano(),
							Hash:      xxhash.Sum64String("6"),
							Value:     1.,
						},
					},
				},
				{
					Labels: "{foo=\"bazz\"}",
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
						{
							Timestamp: from.Add(4 * time.Millisecond).UnixNano(),
							Hash:      xxhash.Sum64String("5"),
							Value:     1.,
						},
						{
							Timestamp: from.Add(5 * time.Millisecond).UnixNano(),
							Hash:      xxhash.Sum64String("6"),
							Value:     1.,
						},
					},
				},
			},
		},
		{
			"filter regex",
			newSampleQuery("rate({foo=~\"ba.*\"} |~ \"1|2|3\" !~ \"2|3\"[1m])", from, from.Add(6*time.Millisecond), nil),
			[]logproto.Series{
				{
					Labels: "{foo=\"bar\"}",
					Samples: []logproto.Sample{
						{
							Timestamp: from.UnixNano(),
							Hash:      xxhash.Sum64String("1"),
							Value:     1.,
						},
					},
				},
				{
					Labels: "{foo=\"bazz\"}",
					Samples: []logproto.Sample{
						{
							Timestamp: from.UnixNano(),
							Hash:      xxhash.Sum64String("1"),
							Value:     1.,
						},
					},
				},
			},
		},
		{
			"filter matcher",
			newSampleQuery("count_over_time({foo=\"bar\"}!=\"1234567\" [10m])", from, from.Add(6*time.Millisecond), nil),
			[]logproto.Series{
				{
					Labels: "{foo=\"bar\"}",
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

						{
							Timestamp: from.Add(4 * time.Millisecond).UnixNano(),
							Hash:      xxhash.Sum64String("5"),
							Value:     1.,
						},
						{
							Timestamp: from.Add(5 * time.Millisecond).UnixNano(),
							Hash:      xxhash.Sum64String("6"),
							Value:     1.,
						},
					},
				},
			},
		},
		{
			"filter time",
			newSampleQuery("count_over_time({foo=~\"ba.*\"}!=\"1234567\" [1s])", from, from.Add(time.Millisecond), nil),
			[]logproto.Series{
				{
					Labels: "{foo=\"bar\"}",
					Samples: []logproto.Sample{
						{
							Timestamp: from.UnixNano(),
							Hash:      xxhash.Sum64String("1"),
							Value:     1.,
						},
					},
				},
				{
					Labels: "{foo=\"bazz\"}",
					Samples: []logproto.Sample{
						{
							Timestamp: from.UnixNano(),
							Hash:      xxhash.Sum64String("1"),
							Value:     1.,
						},
					},
				},
			},
		},
		{
			"delete covers whole time range",
			newSampleQuery(
				"count_over_time({foo=~\"ba.*\"}!=\"1234567\" [5m])",
				from,
				from.Add(6*time.Millisecond),
				[]*logproto.Delete{
					{
						Selector: `{foo="bar"}`,
						Start:    from.Add(-1 * time.Millisecond).UnixNano(),
						End:      from.Add(7 * time.Millisecond).UnixNano(),
					},
					{
						Selector: `{foo="bazz"} |= "6"`,
						Start:    from.Add(-1 * time.Millisecond).UnixNano(),
						End:      from.Add(7 * time.Millisecond).UnixNano(),
					},
				}),
			[]logproto.Series{
				{
					Labels: "{foo=\"bazz\"}",
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

						{
							Timestamp: from.Add(4 * time.Millisecond).UnixNano(),
							Hash:      xxhash.Sum64String("5"),
							Value:     1.,
						},
					},
				},
			},
		},
		{
			"delete covers partial time range",
			newSampleQuery(
				"count_over_time({foo=~\"ba.*\"}!=\"1234567\" [5m])",
				from,
				from.Add(6*time.Millisecond),
				[]*logproto.Delete{
					{
						Selector: `{foo="bar"}`,
						Start:    from.Add(-1 * time.Millisecond).UnixNano(),
						End:      from.Add(3 * time.Millisecond).UnixNano(),
					},
					{
						Selector: `{foo="bazz"} |= "2"`,
						Start:    from.Add(-1 * time.Millisecond).UnixNano(),
						End:      from.Add(3 * time.Millisecond).UnixNano(),
					},
				}),
			[]logproto.Series{
				{
					Labels: "{foo=\"bar\"}",
					Samples: []logproto.Sample{
						{
							Timestamp: from.Add(4 * time.Millisecond).UnixNano(),
							Hash:      xxhash.Sum64String("5"),
							Value:     1.,
						},
						{
							Timestamp: from.Add(5 * time.Millisecond).UnixNano(),
							Hash:      xxhash.Sum64String("6"),
							Value:     1.,
						},
					},
				},
				{
					Labels: "{foo=\"bazz\"}",
					Samples: []logproto.Sample{
						{
							Timestamp: from.UnixNano(),
							Hash:      xxhash.Sum64String("1"),
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
						{
							Timestamp: from.Add(4 * time.Millisecond).UnixNano(),
							Hash:      xxhash.Sum64String("5"),
							Value:     1.,
						},
						{
							Timestamp: from.Add(5 * time.Millisecond).UnixNano(),
							Hash:      xxhash.Sum64String("6"),
							Value:     1.,
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &store{
				Store: storeFixture,
				cfg: Config{
					MaxChunkBatchSize: 10,
				},
				chunkMetrics: NilMetrics,
			}

			s.SetPostFetcherChunkMetricsFilterer(NewRequestPostFetcherChunkFiltererForRequest(10))

			ctx = user.InjectOrgID(context.Background(), "test-user")
			it, err := s.SelectSamples(ctx, logql.SelectSampleParams{SampleQueryRequest: tt.req})
			if err != nil {
				t.Errorf("store.LazyQuery() error = %v", err)
				return
			}

			series, _, err := iter.ReadSampleBatch(it, uint32(100000))
			_ = it.Close()
			if err != nil {
				t.Fatalf("error reading batch %s", err)
			}
			assertSeries(t, tt.expected, series.Series)
		})
	}
}

func Test_store_PostFilter_SelectSample_Filter(t *testing.T) {
	tests := []struct {
		name     string
		req      *logproto.SampleQueryRequest
		expected []logproto.Series
	}{
		{
			"filter regex",
			newSampleQuery("rate({foo=~\"ba.*\"} |~ \"1|2|3\" !~ \"2|3\"[1m])", from, from.Add(6*time.Millisecond), nil),
			[]logproto.Series{
				{
					Labels: "{foo=\"bar\"}",
					Samples: []logproto.Sample{
						{
							Timestamp: from.UnixNano(),
							Hash:      xxhash.Sum64String("1"),
							Value:     1.,
						},
					},
				},
				{
					Labels: "{foo=\"bazz\"}",
					Samples: []logproto.Sample{
						{
							Timestamp: from.UnixNano(),
							Hash:      xxhash.Sum64String("1"),
							Value:     1.,
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &store{
				Store: storeFixture,
				cfg: Config{
					MaxChunkBatchSize: 10,
				},
				chunkMetrics: NilMetrics,
			}

			s.SetPostFetcherChunkMetricsFilterer(NewRequestPostFetcherChunkFiltererForRequest(10))

			ctx = user.InjectOrgID(context.Background(), "test-user")
			it, err := s.SelectSamples(ctx, logql.SelectSampleParams{SampleQueryRequest: tt.req})
			if err != nil {
				t.Errorf("store.LazyQuery() error = %v", err)
				return
			}

			series, _, err := iter.ReadSampleBatch(it, uint32(100000))
			_ = it.Close()
			if err != nil {
				t.Fatalf("error reading batch %s", err)
			}
			assertSeries(t, tt.expected, series.Series)
		})
	}
}

func Test_PostFilter_Sample_OverlappingChunks(t *testing.T) {
	chunks := []chunk.Chunk{
		newChunk(logproto.Stream{
			Labels: `{foo="bar"}`,
			Entries: []logproto.Entry{
				{Timestamp: from.Add(1 * time.Millisecond), Line: "1"},
				{Timestamp: from.Add(4 * time.Millisecond), Line: "4"},
			},
		}),
		newChunk(logproto.Stream{
			Labels: `{foo="bar"}`,
			Entries: []logproto.Entry{
				{Timestamp: from.Add(2 * time.Millisecond), Line: "2"},
				{Timestamp: from.Add(3 * time.Millisecond), Line: "3"},
			},
		}),
	}
	s := &store{
		Store: &mockChunkStore{chunks: chunks, client: &mockChunkStoreClient{chunks: chunks}},
		cfg: Config{
			MaxChunkBatchSize: 10,
		},
		chunkMetrics: NilMetrics,
	}
	s.SetPostFetcherChunkMetricsFilterer(NewRequestPostFetcherChunkFiltererForRequest(10))

	ctx = user.InjectOrgID(context.Background(), "test-user")
	req := newSampleQuery(`count_over_time({foo="bar"} |~ "2|3" [1m])`, from, from.Add(10*time.Millisecond), nil)

	it, err := s.SelectSamples(ctx, logql.SelectSampleParams{SampleQueryRequest: req})
	if err != nil {
		t.Errorf("store.SelectLogs() error = %v", err)
		return
	}

	series, _, err := iter.ReadSampleBatch(it, uint32(100000))
	_ = it.Close()
	if err != nil {
		t.Fatalf("error reading batch %s", err)
	}

	expected := []logproto.Series{
		{
			Labels: "{foo=\"bar\"}",
			Samples: []logproto.Sample{
				{
					Timestamp: from.Add(2 * time.Millisecond).UnixNano(),
					Hash:      xxhash.Sum64String("2"),
					Value:     1.,
				},
				{
					Timestamp: from.Add(3 * time.Millisecond).UnixNano(),
					Hash:      xxhash.Sum64String("3"),
					Value:     1.,
				},
				//
			},
		},
	}
	assertSeries(t, expected, series.Series)
}

func Test_PostFilter_OverlappingChunks(t *testing.T) {
	chunks := []chunk.Chunk{
		newChunk(logproto.Stream{
			Labels: `{foo="bar"}`,
			Entries: []logproto.Entry{
				{Timestamp: time.Unix(0, 1), Line: "1"},
				{Timestamp: time.Unix(0, 4), Line: "4"},
			},
		}),
		newChunk(logproto.Stream{
			Labels: `{foo="bar"}`,
			Entries: []logproto.Entry{
				{Timestamp: time.Unix(0, 2), Line: "2"},
				{Timestamp: time.Unix(0, 3), Line: "3"},
			},
		}),
	}
	s := &store{
		Store: &mockChunkStore{chunks: chunks, client: &mockChunkStoreClient{chunks: chunks}},
		cfg: Config{
			MaxChunkBatchSize: 10,
		},
		chunkMetrics: NilMetrics,
	}
	s.SetPostFetcherChunkFilterer(NewRequestPostFetcherChunkFiltererForRequest(10))

	ctx = user.InjectOrgID(context.Background(), "test-user")
	it, err := s.SelectLogs(ctx, logql.SelectLogParams{QueryRequest: &logproto.QueryRequest{
		Selector:  `{foo="bar"} |~ "1|4"`,
		Limit:     1000,
		Direction: logproto.BACKWARD,
		Start:     time.Unix(0, 0),
		End:       time.Unix(0, 10),
	}})
	if err != nil {
		t.Errorf("store.SelectLogs() error = %v", err)
		return
	}
	defer it.Close()
	require.True(t, it.Next())
	require.Equal(t, "4", it.Entry().Line)
	require.True(t, it.Next())
	require.Equal(t, "1", it.Entry().Line)
	require.False(t, it.Next())
}

func TestCICD(t *testing.T) {
	//TODO just trigger test
}
