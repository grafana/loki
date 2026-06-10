package storage

import (
	"context"
	"fmt"
	_ "net/http/pprof"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/grafana/loki/v3/pkg/compression"
	"github.com/grafana/loki/v3/pkg/storage/types"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/httpreq"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/chunkenc"
	"github.com/grafana/loki/v3/pkg/ingester/client"
	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	lokilog "github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/querier/astmapper"
	"github.com/grafana/loki/v3/pkg/querier/plan"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper"
	"github.com/grafana/loki/v3/pkg/util/constants"
	"github.com/grafana/loki/v3/pkg/util/marshal"
	"github.com/grafana/loki/v3/pkg/validation"
)

var (
	start = model.Time(1523750400000) // April 15, 2018
	ctx   = user.InjectOrgID(context.Background(), "fake")
	cm    = NewClientMetrics()
)

func getLocalStore(path string, cm ClientMetrics) Store {
	limits, err := validation.NewOverrides(validation.Limits{
		MaxQueryLength: model.Duration(6000 * time.Hour),
	}, nil)
	if err != nil {
		panic(err)
	}

	storeConfig := Config{
		FSConfig: local.FSConfig{
			Directory: filepath.Join(path, "chunks"),
		},
		TSDBShipperConfig: indexshipper.Config{
			ActiveIndexDirectory:   filepath.Join(path, "index"),
			CacheLocation:          filepath.Join(path, "cache"),
			ResyncInterval:         1 * time.Minute,
			IngesterDBRetainPeriod: 1 * time.Minute,
			IngesterName:           "ingester-1",
			Mode:                   indexshipper.ModeReadWrite,
		},
		MaxChunkBatchSize: 10,
	}

	schemaConfig := config.SchemaConfig{
		Configs: []config.PeriodConfig{
			{
				From:       config.DayTime{Time: start},
				IndexType:  "tsdb",
				ObjectType: types.StorageTypeFileSystem,
				Schema:     "v13",
				IndexTables: config.IndexPeriodicTableConfig{
					PathPrefix: "index/",
					PeriodicTableConfig: config.PeriodicTableConfig{
						Prefix: "index_",
						Period: time.Hour * 24,
					}},
				RowShards: 16,
			},
		},
	}

	store, err := NewStore(storeConfig, config.ChunkStoreConfig{}, schemaConfig, limits, cm, nil, log.NewNopLogger(), constants.Loki)
	if err != nil {
		panic(err)
	}
	return store
}

func Test_store_SelectLogs(t *testing.T) {
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
			s := &LokiStore{
				Store: storeFixture,
				cfg: Config{
					MaxChunkBatchSize: 10,
				},
				chunkMetrics: NilMetrics,
				logger:       log.NewNopLogger(),
			}

			tt.req.Plan = plan.QueryPlan{
				AST: syntax.MustParseExpr(tt.req.Selector),
			}

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

func Test_store_SelectSample(t *testing.T) {
	tests := []struct {
		name     string
		req      *logproto.SampleQueryRequest
		expected []logproto.Series
	}{
		{
			"all",
			newSampleQuery("count_over_time({foo=~\"ba.*\"}[5m])", from, from.Add(6*time.Millisecond), nil, nil),
			[]logproto.Series{
				{
					Labels: "{foo=\"bar\"}",
					Samples: []logproto.Sample{
						{
							Timestamp: from.UnixNano(),
							Hash:      util.UniqueSampleHash("{foo=\"bar\"}", unsafeGetBytes("1")),
							Value:     1.,
						},
						{
							Timestamp: from.Add(time.Millisecond).UnixNano(),
							Hash:      util.UniqueSampleHash("{foo=\"bar\"}", unsafeGetBytes("2")),
							Value:     1.,
						},
						{
							Timestamp: from.Add(2 * time.Millisecond).UnixNano(),
							Hash:      util.UniqueSampleHash("{foo=\"bar\"}", unsafeGetBytes("3")),
							Value:     1.,
						},
						{
							Timestamp: from.Add(3 * time.Millisecond).UnixNano(),
							Hash:      util.UniqueSampleHash("{foo=\"bar\"}", unsafeGetBytes("4")),
							Value:     1.,
						},
						{
							Timestamp: from.Add(4 * time.Millisecond).UnixNano(),
							Hash:      util.UniqueSampleHash("{foo=\"bar\"}", unsafeGetBytes("5")),
							Value:     1.,
						},
						{
							Timestamp: from.Add(5 * time.Millisecond).UnixNano(),
							Hash:      util.UniqueSampleHash("{foo=\"bar\"}", unsafeGetBytes("6")),
							Value:     1.,
						},
					},
				},
				{
					Labels: "{foo=\"bazz\"}",
					Samples: []logproto.Sample{
						{
							Timestamp: from.UnixNano(),
							Hash:      util.UniqueSampleHash("{foo=\"bazz\"}", unsafeGetBytes("1")),
							Value:     1.,
						},
						{
							Timestamp: from.Add(time.Millisecond).UnixNano(),
							Hash:      util.UniqueSampleHash("{foo=\"bazz\"}", unsafeGetBytes("2")),
							Value:     1.,
						},
						{
							Timestamp: from.Add(2 * time.Millisecond).UnixNano(),
							Hash:      util.UniqueSampleHash("{foo=\"bazz\"}", unsafeGetBytes("3")),
							Value:     1.,
						},
						{
							Timestamp: from.Add(3 * time.Millisecond).UnixNano(),
							Hash:      util.UniqueSampleHash("{foo=\"bazz\"}", unsafeGetBytes("4")),
							Value:     1.,
						},
						{
							Timestamp: from.Add(4 * time.Millisecond).UnixNano(),
							Hash:      util.UniqueSampleHash("{foo=\"bazz\"}", unsafeGetBytes("5")),
							Value:     1.,
						},
						{
							Timestamp: from.Add(5 * time.Millisecond).UnixNano(),
							Hash:      util.UniqueSampleHash("{foo=\"bazz\"}", unsafeGetBytes("6")),
							Value:     1.,
						},
					},
				},
			},
		},
		{
			"filter regex",
			newSampleQuery("rate({foo=~\"ba.*\"} |~ \"1|2|3\" !~ \"2|3\"[1m])", from, from.Add(6*time.Millisecond), nil, nil),
			[]logproto.Series{
				{
					Labels: "{foo=\"bar\"}",
					Samples: []logproto.Sample{
						{
							Timestamp: from.UnixNano(),
							Hash:      util.UniqueSampleHash("{foo=\"bar\"}", unsafeGetBytes("1")),
							Value:     1.,
						},
					},
				},
				{
					Labels: "{foo=\"bazz\"}",
					Samples: []logproto.Sample{
						{
							Timestamp: from.UnixNano(),
							Hash:      util.UniqueSampleHash("{foo=\"bazz\"}", unsafeGetBytes("1")),
							Value:     1.,
						},
					},
				},
			},
		},
		{
			"filter matcher",
			newSampleQuery("count_over_time({foo=\"bar\"}[10m])", from, from.Add(6*time.Millisecond), nil, nil),
			[]logproto.Series{
				{
					Labels: "{foo=\"bar\"}",
					Samples: []logproto.Sample{
						{
							Timestamp: from.UnixNano(),
							Hash:      util.UniqueSampleHash("{foo=\"bar\"}", unsafeGetBytes("1")),
							Value:     1.,
						},
						{
							Timestamp: from.Add(time.Millisecond).UnixNano(),
							Hash:      util.UniqueSampleHash("{foo=\"bar\"}", unsafeGetBytes("2")),
							Value:     1.,
						},
						{
							Timestamp: from.Add(2 * time.Millisecond).UnixNano(),
							Hash:      util.UniqueSampleHash("{foo=\"bar\"}", unsafeGetBytes("3")),
							Value:     1.,
						},
						{
							Timestamp: from.Add(3 * time.Millisecond).UnixNano(),
							Hash:      util.UniqueSampleHash("{foo=\"bar\"}", unsafeGetBytes("4")),
							Value:     1.,
						},

						{
							Timestamp: from.Add(4 * time.Millisecond).UnixNano(),
							Hash:      util.UniqueSampleHash("{foo=\"bar\"}", unsafeGetBytes("5")),
							Value:     1.,
						},
						{
							Timestamp: from.Add(5 * time.Millisecond).UnixNano(),
							Hash:      util.UniqueSampleHash("{foo=\"bar\"}", unsafeGetBytes("6")),
							Value:     1.,
						},
					},
				},
			},
		},
		{
			"filter time",
			newSampleQuery("count_over_time({foo=~\"ba.*\"}[1s])", from, from.Add(time.Millisecond), nil, nil),
			[]logproto.Series{
				{
					Labels: "{foo=\"bar\"}",
					Samples: []logproto.Sample{
						{
							Timestamp: from.UnixNano(),
							Hash:      util.UniqueSampleHash("{foo=\"bar\"}", unsafeGetBytes("1")),
							Value:     1.,
						},
					},
				},
				{
					Labels: "{foo=\"bazz\"}",
					Samples: []logproto.Sample{
						{
							Timestamp: from.UnixNano(),
							Hash:      util.UniqueSampleHash("{foo=\"bazz\"}", unsafeGetBytes("1")),
							Value:     1.,
						},
					},
				},
			},
		},
		{
			"delete covers whole time range",
			newSampleQuery(
				"count_over_time({foo=~\"ba.*\"}[5m])",
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
			[]logproto.Series{
				{
					Labels: "{foo=\"bazz\"}",
					Samples: []logproto.Sample{
						{
							Timestamp: from.UnixNano(),
							Hash:      util.UniqueSampleHash("{foo=\"bazz\"}", unsafeGetBytes("1")),
							Value:     1.,
						},

						{
							Timestamp: from.Add(time.Millisecond).UnixNano(),
							Hash:      util.UniqueSampleHash("{foo=\"bazz\"}", unsafeGetBytes("2")),
							Value:     1.,
						},
						{
							Timestamp: from.Add(2 * time.Millisecond).UnixNano(),
							Hash:      util.UniqueSampleHash("{foo=\"bazz\"}", unsafeGetBytes("3")),
							Value:     1.,
						},
						{
							Timestamp: from.Add(3 * time.Millisecond).UnixNano(),
							Hash:      util.UniqueSampleHash("{foo=\"bazz\"}", unsafeGetBytes("4")),
							Value:     1.,
						},

						{
							Timestamp: from.Add(4 * time.Millisecond).UnixNano(),
							Hash:      util.UniqueSampleHash("{foo=\"bazz\"}", unsafeGetBytes("5")),
							Value:     1.,
						},
					},
				},
			},
		},
		{
			"delete covers partial time range",
			newSampleQuery(
				"count_over_time({foo=~\"ba.*\"}[5m])",
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
			[]logproto.Series{
				{
					Labels: "{foo=\"bar\"}",
					Samples: []logproto.Sample{
						{
							Timestamp: from.Add(4 * time.Millisecond).UnixNano(),
							Hash:      util.UniqueSampleHash("{foo=\"bar\"}", unsafeGetBytes("5")),
							Value:     1.,
						},
						{
							Timestamp: from.Add(5 * time.Millisecond).UnixNano(),
							Hash:      util.UniqueSampleHash("{foo=\"bar\"}", unsafeGetBytes("6")),
							Value:     1.,
						},
					},
				},
				{
					Labels: "{foo=\"bazz\"}",
					Samples: []logproto.Sample{
						{
							Timestamp: from.UnixNano(),
							Hash:      util.UniqueSampleHash("{foo=\"bazz\"}", unsafeGetBytes("1")),
							Value:     1.,
						},
						{
							Timestamp: from.Add(2 * time.Millisecond).UnixNano(),
							Hash:      util.UniqueSampleHash("{foo=\"bazz\"}", unsafeGetBytes("3")),
							Value:     1.,
						},
						{
							Timestamp: from.Add(3 * time.Millisecond).UnixNano(),
							Hash:      util.UniqueSampleHash("{foo=\"bazz\"}", unsafeGetBytes("4")),
							Value:     1.,
						},
						{
							Timestamp: from.Add(4 * time.Millisecond).UnixNano(),
							Hash:      util.UniqueSampleHash("{foo=\"bazz\"}", unsafeGetBytes("5")),
							Value:     1.,
						},
						{
							Timestamp: from.Add(5 * time.Millisecond).UnixNano(),
							Hash:      util.UniqueSampleHash("{foo=\"bazz\"}", unsafeGetBytes("6")),
							Value:     1.,
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &LokiStore{
				Store: storeFixture,
				cfg: Config{
					MaxChunkBatchSize: 10,
				},
				chunkMetrics: NilMetrics,
			}

			tt.req.Plan = plan.QueryPlan{
				AST: syntax.MustParseExpr(tt.req.Selector),
			}

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

type fakeChunkFilterer struct{}

func (f fakeChunkFilterer) ForRequest(_ context.Context) chunk.Filterer {
	return f
}

func (f fakeChunkFilterer) ShouldFilter(metric labels.Labels) bool {
	return metric.Get("foo") == "bazz"
}

func (f fakeChunkFilterer) RequiredLabelNames() []string {
	return []string{"foo"}
}

func Test_ChunkFilterer(t *testing.T) {
	s := &LokiStore{
		Store: storeFixture,
		cfg: Config{
			MaxChunkBatchSize: 10,
		},
		chunkMetrics: NilMetrics,
	}
	s.SetChunkFilterer(&fakeChunkFilterer{})
	ctx = user.InjectOrgID(context.Background(), "test-user")
	it, err := s.SelectSamples(ctx, logql.SelectSampleParams{SampleQueryRequest: newSampleQuery("count_over_time({foo=~\"ba.*\"}[1s])", from, from.Add(1*time.Hour), nil, nil)})
	if err != nil {
		t.Errorf("store.SelectSamples() error = %v", err)
		return
	}
	defer it.Close()
	for it.Next() {
		l, err := syntax.ParseLabels(it.Labels())
		require.NoError(t, err)
		require.NotEqual(t, "bazz", l.Get("foo"))
	}

	logit, err := s.SelectLogs(ctx, logql.SelectLogParams{QueryRequest: newQuery("{foo=~\"ba.*\"}", from, from.Add(1*time.Hour), nil, nil)})
	if err != nil {
		t.Errorf("store.SelectLogs() error = %v", err)
		return
	}
	defer logit.Close()
	for logit.Next() {
		l, err := syntax.ParseLabels(it.Labels())
		require.NoError(t, err)
		require.NotEqual(t, "bazz", l.Get("foo"))
	}
	ids, err := s.SelectSeries(ctx, logql.SelectLogParams{QueryRequest: newQuery("{foo=~\"ba.*\"}", from, from.Add(1*time.Hour), nil, nil)})
	require.NoError(t, err)
	for _, id := range ids {
		require.NotEqual(t, "bazz", id.Get("foo"))
	}
}

func Test_PipelineWrapper(t *testing.T) {
	s := &LokiStore{
		Store: storeFixture,
		cfg: Config{
			MaxChunkBatchSize: 10,
		},
		chunkMetrics: NilMetrics,
	}
	wrapper := &testPipelineWrapper{
		pipeline: newMockPipeline(),
	}

	s.SetPipelineWrapper(wrapper)
	ctx = user.InjectOrgID(context.Background(), "test-user")
	logit, err := s.SelectLogs(ctx, logql.SelectLogParams{QueryRequest: newQuery("{foo=~\"ba.*\"}", from, from.Add(1*time.Hour), []astmapper.ShardAnnotation{{Shard: 1, Of: 5}}, nil)})

	if err != nil {
		t.Errorf("store.SelectLogs() error = %v", err)
		return
	}
	defer logit.Close()
	for logit.Next() {
		require.NoError(t, logit.Err()) // consume the iterator
	}

	require.Equal(t, "test-user", wrapper.tenant)
	require.Equal(t, "{foo=~\"ba.*\"}", wrapper.query)
	require.Equal(t, 28, wrapper.pipeline.sp.called) // we've passed every log line through the wrapper
}

func Test_PipelineWrapper_disabled(t *testing.T) {
	s := &LokiStore{
		Store: storeFixture,
		cfg: Config{
			MaxChunkBatchSize: 10,
		},
		chunkMetrics: NilMetrics,
	}
	wrapper := &testPipelineWrapper{
		pipeline: newMockPipeline(),
	}

	s.SetPipelineWrapper(wrapper)
	ctx = user.InjectOrgID(context.Background(), "test-user")
	ctx = httpreq.InjectHeader(ctx, httpreq.LokiDisablePipelineWrappersHeader, "true")
	logit, err := s.SelectLogs(ctx, logql.SelectLogParams{QueryRequest: newQuery("{foo=~\"ba.*\"}", from, from.Add(1*time.Hour), []astmapper.ShardAnnotation{{Shard: 1, Of: 5}}, nil)})

	if err != nil {
		t.Errorf("store.SelectLogs() error = %v", err)
		return
	}
	defer logit.Close()
	for logit.Next() {
		require.NoError(t, logit.Err()) // consume the iterator
	}

	require.Equal(t, "", wrapper.tenant)
	require.Equal(t, "", wrapper.query)
	require.Equal(t, 0, wrapper.pipeline.sp.called) // we've passed every log line through the wrapper
}

type testPipelineWrapper struct {
	query    string
	pipeline *mockPipeline
	tenant   string
}

func (t *testPipelineWrapper) Wrap(_ context.Context, pipeline lokilog.Pipeline, query, tenant string) lokilog.Pipeline {
	t.tenant = tenant
	t.query = query
	t.pipeline.wrappedExtractor = pipeline
	return t.pipeline
}

func newMockPipeline() *mockPipeline {
	return &mockPipeline{
		sp: &mockStreamPipeline{},
	}
}

type mockPipeline struct {
	wrappedExtractor lokilog.Pipeline
	sp               *mockStreamPipeline
}

func (p *mockPipeline) ForStream(l labels.Labels) lokilog.StreamPipeline {
	sp := p.wrappedExtractor.ForStream(l)
	p.sp.wrappedSP = sp
	return p.sp
}

func (p *mockPipeline) Reset() {}

// A stub always returns the same data
type mockStreamPipeline struct {
	wrappedSP lokilog.StreamPipeline
	called    int
}

func (p *mockStreamPipeline) ReferencedStructuredMetadata() bool {
	return false
}

func (p *mockStreamPipeline) BaseLabels() lokilog.LabelsResult {
	return p.wrappedSP.BaseLabels()
}

func (p *mockStreamPipeline) Process(ts int64, line []byte, lbs labels.Labels) ([]byte, lokilog.LabelsResult, bool) {
	p.called++
	return p.wrappedSP.Process(ts, line, lbs)
}

func (p *mockStreamPipeline) ProcessString(ts int64, line string, lbs labels.Labels) (string, lokilog.LabelsResult, bool) {
	p.called++
	return p.wrappedSP.ProcessString(ts, line, lbs)
}

func Test_SampleWrapper(t *testing.T) {
	s := &LokiStore{
		Store: storeFixture,
		cfg: Config{
			MaxChunkBatchSize: 10,
		},
		chunkMetrics: NilMetrics,
	}
	wrapper := &testExtractorWrapper{
		extractor: newMockExtractor(),
	}
	s.SetExtractorWrapper(wrapper)

	ctx = user.InjectOrgID(context.Background(), "test-user")
	it, err := s.SelectSamples(ctx, logql.SelectSampleParams{SampleQueryRequest: newSampleQuery("count_over_time({foo=~\"ba.*\"}[1s])", from, from.Add(1*time.Hour), []astmapper.ShardAnnotation{{Shard: 1, Of: 3}}, nil)})
	if err != nil {
		t.Errorf("store.SelectSamples() error = %v", err)
		return
	}
	defer it.Close()
	for it.Next() {
		require.NoError(t, it.Err()) // consume the iterator
	}

	require.Equal(t, "test-user", wrapper.tenant)
	require.Equal(t, "count_over_time({foo=~\"ba.*\"}[1s])", wrapper.query)
	require.Equal(t, 28, wrapper.extractor.sp.called) // we've passed every log line through the wrapper
}

func Test_SampleWrapper_disabled(t *testing.T) {
	s := &LokiStore{
		Store: storeFixture,
		cfg: Config{
			MaxChunkBatchSize: 10,
		},
		chunkMetrics: NilMetrics,
	}
	wrapper := &testExtractorWrapper{
		extractor: newMockExtractor(),
	}
	s.SetExtractorWrapper(wrapper)

	ctx = user.InjectOrgID(context.Background(), "test-user")
	ctx = httpreq.InjectHeader(ctx, httpreq.LokiDisablePipelineWrappersHeader, "true")
	it, err := s.SelectSamples(ctx, logql.SelectSampleParams{SampleQueryRequest: newSampleQuery("count_over_time({foo=~\"ba.*\"}[1s])", from, from.Add(1*time.Hour), []astmapper.ShardAnnotation{{Shard: 1, Of: 3}}, nil)})
	if err != nil {
		t.Errorf("store.SelectSamples() error = %v", err)
		return
	}
	defer it.Close()
	for it.Next() {
		require.NoError(t, it.Err()) // consume the iterator
	}

	require.Equal(t, "", wrapper.tenant)
	require.Equal(t, "", wrapper.query)
	require.Equal(t, 0, wrapper.extractor.sp.called) // we've passed every log line through the wrapper
}

type testExtractorWrapper struct {
	query     string
	tenant    string
	extractor *mockExtractor
}

func (t *testExtractorWrapper) Wrap(_ context.Context, extractor lokilog.SampleExtractor, query, tenant string) lokilog.SampleExtractor {
	t.tenant = tenant
	t.query = query
	t.extractor.wrappedExtractor = extractor
	return t.extractor
}

func newMockExtractor() *mockExtractor {
	return &mockExtractor{
		sp: &mockStreamExtractor{},
	}
}

type mockExtractor struct {
	wrappedExtractor lokilog.SampleExtractor
	sp               *mockStreamExtractor
}

func (p *mockExtractor) ForStream(l labels.Labels) lokilog.StreamSampleExtractor {
	sp := p.wrappedExtractor.ForStream(l)
	p.sp.wrappedSP = sp
	return p.sp
}

func (p *mockExtractor) Reset() {}

// A stub always returns the same data
type mockStreamExtractor struct {
	wrappedSP lokilog.StreamSampleExtractor
	called    int
}

func (p *mockStreamExtractor) ReferencedStructuredMetadata() bool {
	return false
}

func (p *mockStreamExtractor) BaseLabels() lokilog.LabelsResult {
	return p.wrappedSP.BaseLabels()
}

func (p *mockStreamExtractor) Process(ts int64, line []byte, lbs labels.Labels) ([]lokilog.ExtractedSample, bool) {
	p.called++
	return p.wrappedSP.Process(ts, line, lbs)
}

func (p *mockStreamExtractor) ProcessString(ts int64, line string, lbs labels.Labels) ([]lokilog.ExtractedSample, bool) {
	p.called++
	return p.wrappedSP.ProcessString(ts, line, lbs)
}

func Test_store_GetSeries(t *testing.T) {
	periodConfig := config.PeriodConfig{
		From:   config.DayTime{Time: 0},
		Schema: "v11",
	}

	chunkfmt, headfmt, err := periodConfig.ChunkFormat()
	require.NoError(t, err)

	tests := []struct {
		name      string
		req       *logproto.QueryRequest
		expected  []logproto.SeriesIdentifier
		batchSize int
	}{
		{
			"all",
			newQuery("{foo=~\"ba.*\"}", from, from.Add(6*time.Millisecond), nil, nil),
			[]logproto.SeriesIdentifier{
				{Labels: mustParseLabels("{foo=\"bar\"}")},
				{Labels: mustParseLabels("{foo=\"bazz\"}")},
			},
			1,
		},
		{
			"all-single-batch",
			newQuery("{foo=~\"ba.*\"}", from, from.Add(6*time.Millisecond), nil, nil),
			[]logproto.SeriesIdentifier{
				{Labels: mustParseLabels("{foo=\"bar\"}")},
				{Labels: mustParseLabels("{foo=\"bazz\"}")},
			},
			5,
		},
		{
			"regexp filter (post chunk fetching)",
			newQuery("{foo=~\"bar.*\"}", from, from.Add(6*time.Millisecond), nil, nil),
			[]logproto.SeriesIdentifier{
				{Labels: mustParseLabels("{foo=\"bar\"}")},
			},
			1,
		},
		{
			"filter matcher",
			newQuery("{foo=\"bar\"}", from, from.Add(6*time.Millisecond), nil, nil),
			[]logproto.SeriesIdentifier{
				{Labels: mustParseLabels("{foo=\"bar\"}")},
			},
			1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &LokiStore{
				Store: newMockChunkStore(chunkfmt, headfmt, streamsFixture),
				cfg: Config{
					MaxChunkBatchSize: tt.batchSize,
				},
				chunkMetrics: NilMetrics,
			}
			ctx = user.InjectOrgID(context.Background(), "test-user")
			out, err := s.SelectSeries(ctx, logql.SelectLogParams{QueryRequest: tt.req})
			if err != nil {
				t.Errorf("store.GetSeries() error = %v", err)
				return
			}
			require.Equal(t, tt.expected, out)
		})
	}
}

func Test_store_decodeReq_Matchers(t *testing.T) {
	tests := []struct {
		name     string
		req      *logproto.QueryRequest
		matchers []*labels.Matcher
	}{
		{
			"unsharded",
			newQuery("{foo=~\"ba.*\"}", from, from.Add(6*time.Millisecond), nil, nil),
			[]*labels.Matcher{
				labels.MustNewMatcher(labels.MatchRegexp, "foo", "ba.*"),
				labels.MustNewMatcher(labels.MatchEqual, model.MetricNameLabel, "logs"),
			},
		},
		{
			"sharded",
			newQuery(
				"{foo=~\"ba.*\"}", from, from.Add(6*time.Millisecond),
				[]astmapper.ShardAnnotation{
					{Shard: 1, Of: 2},
				},
				nil,
			),
			[]*labels.Matcher{
				labels.MustNewMatcher(labels.MatchRegexp, "foo", "ba.*"),
				labels.MustNewMatcher(labels.MatchEqual, model.MetricNameLabel, "logs"),
				labels.MustNewMatcher(
					labels.MatchEqual,
					astmapper.ShardLabel,
					astmapper.ShardAnnotation{Shard: 1, Of: 2}.String(),
				),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ms, _, _, err := decodeReq(logql.SelectLogParams{QueryRequest: tt.req})
			if err != nil {
				t.Errorf("store.GetSeries() error = %v", err)
				return
			}

			syntax.AssertMatchers(t, tt.matchers, ms)
		})
	}
}

type timeRange struct {
	from, to time.Time
}

func TestStore_indexPrefixChange(t *testing.T) {
	tempDir := t.TempDir()

	shipperConfig := indexshipper.Config{}
	flagext.DefaultValues(&shipperConfig)
	shipperConfig.ActiveIndexDirectory = path.Join(tempDir, "active_index")
	shipperConfig.CacheLocation = path.Join(tempDir, "cache")
	shipperConfig.Mode = indexshipper.ModeReadWrite

	cfg := Config{
		FSConfig:          local.FSConfig{Directory: path.Join(tempDir, "chunks")},
		TSDBShipperConfig: shipperConfig,
		NamedStores: NamedStores{
			Filesystem: map[string]NamedFSConfig{
				"named-store": {Directory: path.Join(tempDir, "named-store")},
			},
		},
	}
	require.NoError(t, cfg.NamedStores.Validate())

	firstPeriodDate := parseDate("2019-01-01")
	secondPeriodDate := parseDate("2019-01-02")

	periodConfig := config.PeriodConfig{
		From:       config.DayTime{Time: timeToModelTime(firstPeriodDate)},
		IndexType:  types.IndexTypeTSDB,
		ObjectType: types.StorageTypeFileSystem,
		Schema:     "v9",
		IndexTables: config.IndexPeriodicTableConfig{
			PathPrefix: "index/",
			PeriodicTableConfig: config.PeriodicTableConfig{
				Prefix: "index_",
				Period: time.Hour * 24,
			}},
	}

	schemaConfig := config.SchemaConfig{
		Configs: []config.PeriodConfig{
			periodConfig,
		},
	}

	// time ranges for adding chunks to the first period
	chunksToBuildForTimeRanges := []timeRange{
		{
			secondPeriodDate.Add(-10 * time.Hour),
			secondPeriodDate.Add(-9 * time.Hour),
		},
		{
			secondPeriodDate.Add(-3 * time.Hour),
			secondPeriodDate.Add(-2 * time.Hour),
		},
	}

	limits, err := validation.NewOverrides(validation.Limits{}, nil)
	require.NoError(t, err)

	store, err := NewStore(cfg, config.ChunkStoreConfig{}, schemaConfig, limits, cm, nil, log.NewNopLogger(), constants.Loki)
	require.NoError(t, err)

	// build and add chunks to the store
	addedChunkIDs := map[string]struct{}{}
	for _, tr := range chunksToBuildForTimeRanges {
		periodConfig, err := schemaConfig.SchemaForTime(timeToModelTime(tr.from))
		require.NoError(t, err)
		require.NotNil(t, periodConfig)

		chunkfmt, headfmt, err := periodConfig.ChunkFormat()
		require.NoError(t, err)

		chk := newChunk(chunkfmt, headfmt, buildTestStreams(fooLabelsWithName, tr))

		err = store.PutOne(ctx, chk.From, chk.Through, chk)
		require.NoError(t, err)

		addedChunkIDs[schemaConfig.ExternalKey(chk.ChunkRef)] = struct{}{}
	}

	// get all the chunks from the first period
	predicate := chunk.NewPredicate(newMatchers(fooLabelsWithName.String()), nil)
	chunks, _, err := store.GetChunks(ctx, "fake", timeToModelTime(firstPeriodDate), timeToModelTime(secondPeriodDate), predicate, nil)
	require.NoError(t, err)
	var totalChunks int
	for _, chks := range chunks {
		totalChunks += len(chks)
	}
	require.Equal(t, totalChunks, len(addedChunkIDs))

	// check whether we got back all the chunks which were added
	for i := range chunks {
		for _, c := range chunks[i] {
			_, ok := addedChunkIDs[schemaConfig.ExternalKey(c.ChunkRef)]
			require.True(t, ok)
		}
	}

	// update schema with a new period that uses different index prefix
	periodConfig2 := config.PeriodConfig{
		From:       config.DayTime{Time: timeToModelTime(secondPeriodDate)},
		IndexType:  types.IndexTypeTSDB,
		ObjectType: "named-store",
		Schema:     "v11",
		IndexTables: config.IndexPeriodicTableConfig{
			PathPrefix: "index/",
			PeriodicTableConfig: config.PeriodicTableConfig{
				Prefix: "index_tsdb_",
				Period: time.Hour * 24,
			}},
		RowShards: 2,
	}
	schemaConfig.Configs = append(schemaConfig.Configs, periodConfig2)

	// time ranges adding a chunk to the new period and one that overlaps both
	chunksToBuildForTimeRanges = []timeRange{
		{
			// chunk overlapping both the stores
			secondPeriodDate.Add(-time.Hour),
			secondPeriodDate.Add(time.Hour),
		},
		{
			// chunk just for second store
			secondPeriodDate.Add(2 * time.Hour),
			secondPeriodDate.Add(3 * time.Hour),
		},
	}

	// restart to load the updated schema
	store.Stop()
	store, err = NewStore(cfg, config.ChunkStoreConfig{}, schemaConfig, limits, cm, nil, log.NewNopLogger(), constants.Loki)
	require.NoError(t, err)
	defer store.Stop()

	// build and add chunks to the store
	for _, tr := range chunksToBuildForTimeRanges {
		periodConfig, err := schemaConfig.SchemaForTime(timeToModelTime(tr.from))
		require.NoError(t, err)
		require.NotNil(t, periodConfig)

		chunkfmt, headfmt, err := periodConfig.ChunkFormat()
		require.NoError(t, err)

		chk := newChunk(chunkfmt, headfmt, buildTestStreams(fooLabelsWithName, tr))

		err = store.PutOne(ctx, chk.From, chk.Through, chk)
		require.NoError(t, err)

		addedChunkIDs[schemaConfig.ExternalKey(chk.ChunkRef)] = struct{}{}
	}

	// get all the chunks from both the stores
	predicate = chunk.NewPredicate(newMatchers(fooLabelsWithName.String()), nil)
	chunks, _, err = store.GetChunks(ctx, "fake", timeToModelTime(firstPeriodDate), timeToModelTime(secondPeriodDate.Add(24*time.Hour)), predicate, nil)
	require.NoError(t, err)

	totalChunks = 0
	for _, chks := range chunks {
		totalChunks += len(chks)
	}
	// we get common chunk twice because it is indexed in both the stores
	require.Equal(t, len(addedChunkIDs)+1, totalChunks)

	// check whether we got back all the chunks which were added
	for i := range chunks {
		for _, c := range chunks[i] {
			_, ok := addedChunkIDs[schemaConfig.ExternalKey(c.ChunkRef)]
			require.True(t, ok)
		}
	}
}

func TestStore_MultiPeriod(t *testing.T) {
	limits, err := validation.NewOverrides(validation.Limits{}, nil)
	require.NoError(t, err)

	firstStoreDate := parseDate("2019-01-01")
	secondStoreDate := parseDate("2019-01-02")

	for name, indexes := range map[string][]string{
		"tsdb_tsdb": {types.IndexTypeTSDB, types.IndexTypeTSDB},
	} {
		t.Run(name, func(t *testing.T) {
			tempDir := t.TempDir()

			shipperConfig := indexshipper.Config{}
			flagext.DefaultValues(&shipperConfig)
			shipperConfig.ActiveIndexDirectory = path.Join(tempDir, "index")
			shipperConfig.CacheLocation = path.Join(tempDir, "cache")
			shipperConfig.Mode = indexshipper.ModeReadWrite

			cfg := Config{
				FSConfig:          local.FSConfig{Directory: path.Join(tempDir, "chunks")},
				TSDBShipperConfig: shipperConfig,
				NamedStores: NamedStores{
					Filesystem: map[string]NamedFSConfig{
						"named-store": {Directory: path.Join(tempDir, "named-store")},
					},
				},
			}
			require.NoError(t, cfg.NamedStores.Validate())

			periodConfigV9 := config.PeriodConfig{
				From:       config.DayTime{Time: timeToModelTime(firstStoreDate)},
				IndexType:  indexes[0],
				ObjectType: types.StorageTypeFileSystem,
				Schema:     "v9",
				IndexTables: config.IndexPeriodicTableConfig{
					PeriodicTableConfig: config.PeriodicTableConfig{
						Prefix: "index_",
						Period: time.Hour * 24,
					}},
			}

			periodConfigV11 := config.PeriodConfig{
				From:       config.DayTime{Time: timeToModelTime(secondStoreDate)},
				IndexType:  indexes[1],
				ObjectType: "named-store",
				Schema:     "v11",
				IndexTables: config.IndexPeriodicTableConfig{
					PeriodicTableConfig: config.PeriodicTableConfig{
						Prefix: "index_",
						Period: time.Hour * 24,
					}},
				RowShards: 2,
			}

			schemaConfig := config.SchemaConfig{
				Configs: []config.PeriodConfig{
					periodConfigV9,
					periodConfigV11,
				},
			}

			store, err := NewStore(cfg, config.ChunkStoreConfig{}, schemaConfig, limits, cm, nil, log.NewNopLogger(), constants.Loki)
			require.NoError(t, err)

			// time ranges adding a chunk for each store and a chunk which overlaps both the stores
			chunksToBuildForTimeRanges := []timeRange{
				{
					// chunk just for first store
					secondStoreDate.Add(-3 * time.Hour),
					secondStoreDate.Add(-2 * time.Hour),
				},
				{
					// chunk overlapping both the stores
					secondStoreDate.Add(-time.Hour),
					secondStoreDate.Add(time.Hour),
				},
				{
					// chunk just for second store
					secondStoreDate.Add(2 * time.Hour),
					secondStoreDate.Add(3 * time.Hour),
				},
			}

			// build and add chunks to the store
			addedChunkIDs := map[string]struct{}{}
			for _, tr := range chunksToBuildForTimeRanges {
				periodConfig, err := schemaConfig.SchemaForTime(timeToModelTime(tr.from))
				require.NoError(t, err)
				chunkfmt, headfmt, err := periodConfig.ChunkFormat()
				require.NoError(t, err)

				chk := newChunk(chunkfmt, headfmt, buildTestStreams(fooLabelsWithName, tr))

				err = store.PutOne(ctx, chk.From, chk.Through, chk)
				require.NoError(t, err)

				addedChunkIDs[schemaConfig.ExternalKey(chk.ChunkRef)] = struct{}{}
			}

			// get all the chunks from both the stores
			predicate := chunk.NewPredicate(newMatchers(fooLabelsWithName.String()), nil)
			chunks, _, err := store.GetChunks(ctx, "fake", timeToModelTime(firstStoreDate), timeToModelTime(secondStoreDate.Add(24*time.Hour)), predicate, nil)
			require.NoError(t, err)
			var totalChunks int
			for _, chks := range chunks {
				totalChunks += len(chks)
			}
			// we get common chunk twice because it is indexed in both the stores
			require.Equal(t, totalChunks, len(addedChunkIDs)+1)

			// check whether we got back all the chunks which were added
			for i := range chunks {
				for _, c := range chunks[i] {
					_, ok := addedChunkIDs[schemaConfig.ExternalKey(c.ChunkRef)]
					require.True(t, ok)
				}
			}
		})
	}

}

func mustParseLabels(s string) []logproto.SeriesIdentifier_LabelsEntry {
	l, err := marshal.NewLabelSet(s)
	if err != nil {
		panic(fmt.Sprintf("Failed to parse %s", s))
	}

	result := make([]logproto.SeriesIdentifier_LabelsEntry, 0, len(l))
	for k, v := range l {
		result = append(result, logproto.SeriesIdentifier_LabelsEntry{Key: k, Value: v})
	}
	return result
}

func parseDate(in string) time.Time {
	t, err := time.Parse("2006-01-02", in)
	if err != nil {
		panic(err)
	}
	return t
}

func buildTestStreams(ls labels.Labels, tr timeRange) logproto.Stream {
	stream := logproto.Stream{
		Labels:  ls.String(),
		Hash:    labels.StableHash(ls),
		Entries: []logproto.Entry{},
	}

	for from := tr.from; from.Before(tr.to); from = from.Add(time.Second) {
		stream.Entries = append(stream.Entries, logproto.Entry{
			Timestamp: from,
			Line:      from.String(),
		})
	}

	return stream
}

func timeToModelTime(t time.Time) model.Time {
	return model.TimeFromUnixNano(t.UnixNano())
}

func Test_OverlappingChunks(t *testing.T) {
	periodConfig := config.PeriodConfig{
		From:   config.DayTime{Time: 0},
		Schema: "v11",
	}

	chunkfmt, headfmt, err := periodConfig.ChunkFormat()
	require.NoError(t, err)

	chunks := []chunk.Chunk{
		newChunk(chunkfmt, headfmt, logproto.Stream{
			Labels: `{foo="bar"}`,
			Entries: []logproto.Entry{
				{Timestamp: time.Unix(0, 1), Line: "1"},
				{Timestamp: time.Unix(0, 4), Line: "4"},
			},
		}),
		newChunk(chunkfmt, headfmt, logproto.Stream{
			Labels: `{foo="bar"}`,
			Entries: []logproto.Entry{
				{Timestamp: time.Unix(0, 2), Line: "2"},
				{Timestamp: time.Unix(0, 3), Line: "3"},
			},
		}),
	}
	s := &LokiStore{
		Store: &mockChunkStore{chunks: chunks, client: &mockChunkStoreClient{chunks: chunks}},
		cfg: Config{
			MaxChunkBatchSize: 10,
		},
		chunkMetrics: NilMetrics,
	}

	ctx = user.InjectOrgID(context.Background(), "test-user")
	it, err := s.SelectLogs(ctx, logql.SelectLogParams{QueryRequest: &logproto.QueryRequest{
		Selector:  `{foo="bar"}`,
		Limit:     1000,
		Direction: logproto.Direction_BACKWARD,
		Start:     time.Unix(0, 0),
		End:       time.Unix(0, 10),
		Plan: plan.QueryPlan{
			AST: syntax.MustParseExpr(`{foo="bar"}`),
		},
	}})
	if err != nil {
		t.Errorf("store.SelectLogs() error = %v", err)
		return
	}
	defer it.Close()
	require.True(t, it.Next())
	require.Equal(t, "4", it.At().Line)
	require.True(t, it.Next())
	require.Equal(t, "3", it.At().Line)
	require.True(t, it.Next())
	require.Equal(t, "2", it.At().Line)
	require.True(t, it.Next())
	require.Equal(t, "1", it.At().Line)
	require.False(t, it.Next())
}

func Test_GetSeries(t *testing.T) {
	periodConfig := config.PeriodConfig{
		From:   config.DayTime{Time: 0},
		Schema: "v11",
	}

	chunkfmt, headfmt, err := periodConfig.ChunkFormat()
	require.NoError(t, err)

	var (
		store = &LokiStore{
			Store: newMockChunkStore(chunkfmt, headfmt, []*logproto.Stream{
				{
					Labels: `{foo="bar",buzz="boo"}`,
					Entries: []logproto.Entry{
						{Timestamp: time.Unix(0, 1), Line: "1"},
					},
				},
				{
					Labels: `{foo="buzz"}`,
					Entries: []logproto.Entry{
						{Timestamp: time.Unix(0, 1), Line: "1"},
					},
				},
				{
					Labels: `{bar="foo"}`,
					Entries: []logproto.Entry{
						{Timestamp: time.Unix(0, 1), Line: "1"},
					},
				},
			}),
			cfg: Config{
				MaxChunkBatchSize: 10,
			},
			chunkMetrics: NilMetrics,
		}
		ctx            = user.InjectOrgID(context.Background(), "test-user")
		expectedSeries = []logproto.SeriesIdentifier{
			{
				Labels: logproto.MustNewSeriesEntries("bar", "foo"),
			},
			{
				Labels: logproto.MustNewSeriesEntries(
					"buzz", "boo",
					"foo", "bar",
				),
			},
			{
				Labels: logproto.MustNewSeriesEntries("foo", "buzz"),
			},
		}
	)

	for _, tt := range []struct {
		name           string
		req            logql.SelectLogParams
		expectedSeries []logproto.SeriesIdentifier
	}{
		{
			"all series",
			logql.SelectLogParams{
				QueryRequest: &logproto.QueryRequest{
					Selector: ``,
					Start:    time.Unix(0, 0),
					End:      time.Unix(0, 10),
				},
			},
			expectedSeries,
		},
		{
			"selected series",
			logql.SelectLogParams{
				QueryRequest: &logproto.QueryRequest{
					Selector: `{buzz=~".oo"}`,
					Start:    time.Unix(0, 0),
					End:      time.Unix(0, 10),
				},
			},
			[]logproto.SeriesIdentifier{
				{
					Labels: logproto.MustNewSeriesEntries(
						"buzz", "boo",
						"foo", "bar",
					),
				},
			},
		},
		{
			"no match",
			logql.SelectLogParams{
				QueryRequest: &logproto.QueryRequest{
					Selector: `{buzz=~"foo"}`,
					Start:    time.Unix(0, 0),
					End:      time.Unix(0, 10),
				},
			},
			[]logproto.SeriesIdentifier{},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.req.Selector != "" {
				tt.req.Plan = plan.QueryPlan{
					AST: syntax.MustParseExpr(tt.req.Selector),
				}
			} else {
				tt.req.Plan = plan.QueryPlan{
					AST: nil,
				}
			}
			series, err := store.SelectSeries(ctx, tt.req)
			require.NoError(t, err)
			require.Equal(t, tt.expectedSeries, series)
		})
	}
}

func TestStore_BoltdbTsdbSameIndexPrefix(t *testing.T) {
	tempDir := t.TempDir()

	ingesterName := "ingester-1"
	limits, err := validation.NewOverrides(validation.Limits{}, nil)
	require.NoError(t, err)

	// config for tsdb Shipper
	tsdbShipperConfig := indexshipper.Config{}
	flagext.DefaultValues(&tsdbShipperConfig)
	tsdbShipperConfig.ActiveIndexDirectory = path.Join(tempDir, "tsdb-index")
	tsdbShipperConfig.CacheLocation = path.Join(tempDir, "tsdb-shipper-cache")
	tsdbShipperConfig.Mode = indexshipper.ModeReadWrite
	tsdbShipperConfig.IngesterName = ingesterName

	oldStartDate := parseDate("2019-01-01")
	newStartDate := parseDate("2019-01-02")

	cfg := Config{
		FSConfig:          local.FSConfig{Directory: path.Join(tempDir, "objects")},
		TSDBShipperConfig: tsdbShipperConfig,
	}

	schemaConfig := config.SchemaConfig{
		Configs: []config.PeriodConfig{
			{
				From:       config.DayTime{Time: timeToModelTime(oldStartDate)},
				IndexType:  "tsdb",
				ObjectType: types.StorageTypeFileSystem,
				Schema:     "v12",
				IndexTables: config.IndexPeriodicTableConfig{
					PathPrefix: "index/",
					PeriodicTableConfig: config.PeriodicTableConfig{
						Prefix: "index_",
						Period: time.Hour * 24,
					}},
				RowShards: 2,
			},
			{
				From:       config.DayTime{Time: timeToModelTime(newStartDate)},
				IndexType:  "tsdb",
				ObjectType: types.StorageTypeFileSystem,
				Schema:     "v13",
				IndexTables: config.IndexPeriodicTableConfig{
					PathPrefix: "index/",
					PeriodicTableConfig: config.PeriodicTableConfig{
						Prefix: "index_",
						Period: time.Hour * 24,
					}},
			},
		},
	}

	store, err := NewStore(cfg, config.ChunkStoreConfig{}, schemaConfig, limits, cm, nil, log.NewNopLogger(), constants.Loki)
	require.NoError(t, err)

	// time ranges adding a chunk for each store and a chunk which overlaps both the stores
	chunksToBuildForTimeRanges := []timeRange{
		{
			// chunk just for first store
			newStartDate.Add(-3 * time.Hour),
			newStartDate.Add(-2 * time.Hour),
		},
		{
			// chunk overlapping both the stores
			newStartDate.Add(-time.Hour),
			newStartDate.Add(time.Hour),
		},
		{
			// chunk just for second store
			newStartDate.Add(2 * time.Hour),
			newStartDate.Add(3 * time.Hour),
		},
	}

	// build and add chunks to the store
	addedChunkIDs := map[string]struct{}{}
	for _, tr := range chunksToBuildForTimeRanges {
		periodConfig, err := schemaConfig.SchemaForTime(timeToModelTime(tr.from))
		require.NoError(t, err)

		chunkfmt, headfmt, err := periodConfig.ChunkFormat()
		require.NoError(t, err)

		chk := newChunk(chunkfmt, headfmt, buildTestStreams(fooLabelsWithName, tr))

		err = store.PutOne(ctx, chk.From, chk.Through, chk)
		require.NoError(t, err)

		addedChunkIDs[schemaConfig.ExternalKey(chk.ChunkRef)] = struct{}{}
	}

	// Stop() will write the indexes to disk
	store.Stop()

	// there should be 2 index tables in the object storage
	indexDir := filepath.Join(cfg.FSConfig.Directory, "index")

	indexTables, err := os.ReadDir(indexDir)
	require.NoError(t, err)
	require.Len(t, indexTables, 2)
	require.Equal(t, "index_17897", indexTables[0].Name())
	require.Equal(t, "index_17898", indexTables[1].Name())

	// there should be just 1 file in each table in the object storage
	tsdbFiles, err := os.ReadDir(filepath.Join(indexDir, indexTables[0].Name()))
	require.NoError(t, err)
	require.Len(t, tsdbFiles, 1)
	require.Regexp(t, regexp.MustCompile(fmt.Sprintf(`\d{10}-%s-\d{19}\.tsdb\.gz`, ingesterName)), tsdbFiles[0].Name())

	tsdbFiles, err = os.ReadDir(filepath.Join(indexDir, indexTables[1].Name()))
	require.NoError(t, err)
	require.Len(t, tsdbFiles, 1)
	require.Regexp(t, regexp.MustCompile(fmt.Sprintf(`\d{10}-%s-\d{19}\.tsdb\.gz`, ingesterName)), tsdbFiles[0].Name())

	store, err = NewStore(cfg, config.ChunkStoreConfig{}, schemaConfig, limits, cm, nil, log.NewNopLogger(), constants.Loki)
	require.NoError(t, err)
	defer store.Stop()

	// get all the chunks from both the stores
	predicate := chunk.NewPredicate(newMatchers(fooLabelsWithName.String()), nil)
	chunks, _, err := store.GetChunks(ctx, "fake", timeToModelTime(oldStartDate), timeToModelTime(newStartDate.Add(24*time.Hour)), predicate, nil)
	require.NoError(t, err)
	var totalChunks int
	for _, chks := range chunks {
		totalChunks += len(chks)
	}
	// we get common chunk twice because it is indexed in both the stores
	require.Equal(t, totalChunks, len(addedChunkIDs)+1)

	// check whether we got back all the chunks which were added
	for i := range chunks {
		for _, c := range chunks[i] {
			_, ok := addedChunkIDs[schemaConfig.ExternalKey(c.ChunkRef)]
			require.True(t, ok)
		}
	}
}

func TestStore_SyncStopInteraction(t *testing.T) {
	tempDir := t.TempDir()

	ingesterName := "ingester-1"
	limits, err := validation.NewOverrides(validation.Limits{}, nil)
	require.NoError(t, err)

	// config for tsdb Shipper
	tsdbShipperConfig := indexshipper.Config{}
	flagext.DefaultValues(&tsdbShipperConfig)
	tsdbShipperConfig.ActiveIndexDirectory = path.Join(tempDir, "tsdb-index")
	tsdbShipperConfig.CacheLocation = path.Join(tempDir, "tsdb-shipper-cache")
	tsdbShipperConfig.Mode = indexshipper.ModeReadWrite
	tsdbShipperConfig.IngesterName = ingesterName
	tsdbShipperConfig.ResyncInterval = time.Millisecond

	oldStartDate := parseDate("2019-01-01")
	newStartDate := parseDate("2019-01-02")

	cfg := Config{
		FSConfig:          local.FSConfig{Directory: path.Join(tempDir, "chunks")},
		TSDBShipperConfig: tsdbShipperConfig,
	}

	schemaConfig := config.SchemaConfig{
		Configs: []config.PeriodConfig{
			{
				From:       config.DayTime{Time: timeToModelTime(oldStartDate)},
				IndexType:  "tsdb",
				ObjectType: types.StorageTypeFileSystem,
				Schema:     "v12",
				IndexTables: config.IndexPeriodicTableConfig{
					PathPrefix: "index/",
					PeriodicTableConfig: config.PeriodicTableConfig{
						Prefix: "index_",
						Period: time.Hour * 24,
					}},
				RowShards: 2,
			},
			{
				From:       config.DayTime{Time: timeToModelTime(newStartDate)},
				IndexType:  "tsdb",
				ObjectType: types.StorageTypeFileSystem,
				Schema:     "v13",
				IndexTables: config.IndexPeriodicTableConfig{
					PathPrefix: "index/",
					PeriodicTableConfig: config.PeriodicTableConfig{
						Prefix: "index_",
						Period: time.Hour * 24,
					}},
			},
		},
	}

	store, err := NewStore(cfg, config.ChunkStoreConfig{}, schemaConfig, limits, cm, nil, log.NewNopLogger(), constants.Loki)
	require.NoError(t, err)
	store.Stop()
}

func TestQueryReferencingStructuredMetadata(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), "fake")
	tempDir := t.TempDir()
	store := getLocalStore(tempDir, cm)

	schemaCfg := store.(*LokiStore).schemaCfg
	periodcfg, err := schemaCfg.SchemaForTime(start)
	require.NoError(t, err)

	chunkfmt, headfmt, err := periodcfg.ChunkFormat()
	require.NoError(t, err)

	now := time.Now()
	chkFrom := now.Add(-50 * time.Second)
	chkThrough := now

	// add some streams with and without structured metadata
	for _, withStructuredMetadata := range []bool{true, false} {
		stream := fmt.Sprintf(`{sm="%v"}`, withStructuredMetadata)
		lbs, err := syntax.ParseLabels(stream)
		if err != nil {
			panic(err)
		}
		labelsBuilder := labels.NewBuilder(lbs)
		labelsBuilder.Set(model.MetricNameLabel, "logs")
		metric := labelsBuilder.Labels()
		fp := client.Fingerprint(lbs)

		chunkEnc := chunkenc.NewMemChunk(chunkfmt, compression.LZ4_4M, headfmt, 262144, 1572864)
		for ts := chkFrom; !ts.After(chkThrough); ts = ts.Add(time.Second) {
			entry := logproto.Entry{
				Timestamp: ts,
				Line:      fmt.Sprintf("ts=%d level=info", ts.Unix()),
				Parsed:    logproto.EmptyLabelAdapters(),
			}

			if withStructuredMetadata {
				entry.StructuredMetadata = push.LabelsAdapter{
					{
						Name:  "fizz",
						Value: "buzz",
					},
					{
						Name:  "num",
						Value: "1",
					},
				}
			} else {
				entry.StructuredMetadata = logproto.EmptyLabelAdapters()
			}
			dup, err := chunkEnc.Append(&entry)
			require.False(t, dup)
			require.NoError(t, err)
		}

		require.NoError(t, chunkEnc.Close())
		from, to := chunkEnc.Bounds()
		c := chunk.NewChunk("fake", fp, metric, chunkenc.NewFacade(chunkEnc, 0, 0), model.TimeFromUnixNano(from.UnixNano()), model.TimeFromUnixNano(to.UnixNano()))
		if err := c.Encode(); err != nil {
			panic(err)
		}
		require.NoError(t, store.Put(ctx, []chunk.Chunk{c}))
	}

	// TODO(chaudum): Can this be removed?
	store.Stop()
	store = getLocalStore(tempDir, cm)
	t.Cleanup(store.Stop)

	for _, withStructuredMetadata := range []bool{true, false} {
		stream := fmt.Sprintf(`{sm="%v"}`, withStructuredMetadata)
		// verify the data by querying it
		it, err := store.SelectLogs(ctx, logql.SelectLogParams{QueryRequest: &logproto.QueryRequest{
			Selector:  stream,
			Limit:     1000,
			Direction: logproto.Direction_FORWARD,
			Start:     chkFrom,
			End:       chkThrough.Add(time.Minute),
			Plan: plan.QueryPlan{
				AST: syntax.MustParseExpr(stream),
			},
		}})
		require.NoError(t, err)

		for ts := chkFrom; !ts.After(chkThrough); ts = ts.Add(time.Second) {
			require.True(t, it.Next())
			expectedEntry := logproto.Entry{
				Timestamp: ts.Truncate(0),
				Line:      fmt.Sprintf("ts=%d level=info", ts.Unix()),
				Parsed:    logproto.EmptyLabelAdapters(),
			}

			if withStructuredMetadata {
				expectedEntry.StructuredMetadata = push.LabelsAdapter{
					{
						Name:  "fizz",
						Value: "buzz",
					},
					{
						Name:  "num",
						Value: "1",
					},
				}
			} else {
				expectedEntry.StructuredMetadata = logproto.EmptyLabelAdapters()
			}
			require.Equal(t, expectedEntry, it.At())
		}

		require.False(t, it.Next())
		require.NoError(t, it.Close())
	}

	// test cases for logs queries
	t.Run("logs queries", func(t *testing.T) {
		for _, tc := range []struct {
			name  string
			query string

			expectedReferencedStructuredMetadata bool
		}{
			{
				name:  "logs not having structured metadata",
				query: `{sm="false"}`,
			},
			{
				name:  "not referencing structured metadata in logs having structured metadata",
				query: `{sm="true"}`,
			},
			{
				name:  "referencing a parsed field",
				query: `{sm="true"} | logfmt | level="info"`,
			},
			{
				name:  "referencing structured metadata with label filter",
				query: `{sm="true"} | fizz="buzz"`,

				expectedReferencedStructuredMetadata: true,
			},
			{
				name:  "referencing structured metadata to drop it",
				query: `{sm="true"} | drop fizz`,

				expectedReferencedStructuredMetadata: true,
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				ctx := user.InjectOrgID(context.Background(), "fake")
				_, ctx = stats.NewContext(ctx)
				it, err := store.SelectLogs(ctx, logql.SelectLogParams{QueryRequest: &logproto.QueryRequest{
					Selector:  tc.query,
					Limit:     1000,
					Direction: logproto.Direction_FORWARD,
					Start:     chkFrom,
					End:       chkThrough.Add(time.Minute),
					Plan: plan.QueryPlan{
						AST: syntax.MustParseExpr(tc.query),
					},
				}})
				require.NoError(t, err)
				numEntries := int64(0)
				for it.Next() {
					numEntries++
				}
				require.NoError(t, it.Close())
				require.Equal(t, chkThrough.Unix()-chkFrom.Unix()+1, numEntries)

				statsCtx := stats.FromContext(ctx)
				require.Equal(t, tc.expectedReferencedStructuredMetadata, statsCtx.Result(0, 0, 0).QueryReferencedStructuredMetadata())
			})
		}
	})

	// test cases for metric queries
	t.Run("metric queries", func(t *testing.T) {
		for _, tc := range []struct {
			name  string
			query string

			expectedReferencedStructuredMetadata bool
		}{
			{
				name:  "logs not having structured metadata",
				query: `sum(count_over_time({sm="false"}[1m]))`,
			},
			{
				name:  "not referencing structured metadata in logs having structured metadata",
				query: `sum(count_over_time({sm="true"}[1m]))`,
			},
			{
				name:  "referencing a parsed field",
				query: `sum by (level) (count_over_time({sm="true"} | logfmt | level="info"[1m]))`,
			},
			{
				name:  "referencing structured metadata with label filter",
				query: `sum(count_over_time({sm="true"} | fizz="buzz"[1m]))`,

				expectedReferencedStructuredMetadata: true,
			},
			{
				name:  "referencing structured metadata in by aggregation clause",
				query: `sum by (fizz) (count_over_time({sm="true"}[1m]))`,

				expectedReferencedStructuredMetadata: true,
			},
			{
				name:  "referencing structured metadata in without aggregation clause",
				query: `sum without (fizz) (count_over_time({sm="true"}[1m]))`,

				expectedReferencedStructuredMetadata: true,
			},
			{
				name:  "referencing structured metadata in unwrap",
				query: `sum(sum_over_time({sm="true"} | unwrap num[1m]))`,

				expectedReferencedStructuredMetadata: true,
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				ctx := user.InjectOrgID(context.Background(), "fake")
				_, ctx = stats.NewContext(ctx)
				it, err := store.SelectSamples(ctx, logql.SelectSampleParams{SampleQueryRequest: newSampleQuery(tc.query, chkFrom, chkThrough.Add(time.Minute), nil, nil)})
				require.NoError(t, err)
				numSamples := int64(0)
				for it.Next() {
					numSamples++
				}
				require.NoError(t, it.Close())
				require.Equal(t, chkThrough.Unix()-chkFrom.Unix()+1, numSamples)

				statsCtx := stats.FromContext(ctx)
				require.Equal(t, tc.expectedReferencedStructuredMetadata, statsCtx.Result(0, 0, 0).QueryReferencedStructuredMetadata())
			})
		}
	})
}
