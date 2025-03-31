package series_test

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/test"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/chunkenc"
	"github.com/grafana/loki/v3/pkg/compression"
	"github.com/grafana/loki/v3/pkg/ingester/client"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/storage"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/testutils"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/series/index"
	"github.com/grafana/loki/v3/pkg/util/constants"
	"github.com/grafana/loki/v3/pkg/validation"
)

type configFactory func() config.ChunkStoreConfig

const userID = "1"

var (
	ctx     = user.InjectOrgID(context.Background(), userID)
	schemas = []string{"v9", "v10", "v11", "v12", "v13"}
	stores  = []struct {
		name     string
		configFn configFactory
	}{
		{
			name: "store",
			configFn: func() config.ChunkStoreConfig {
				var storeCfg config.ChunkStoreConfig
				flagext.DefaultValues(&storeCfg)
				return storeCfg
			},
		},
		{
			name: "cached_store",
			configFn: func() config.ChunkStoreConfig {
				var storeCfg config.ChunkStoreConfig
				flagext.DefaultValues(&storeCfg)
				storeCfg.WriteDedupeCacheConfig.Cache = cache.NewEmbeddedCache("test", cache.EmbeddedCacheConfig{MaxSizeItems: 500}, prometheus.NewRegistry(), log.NewNopLogger(), stats.ChunkCache)
				return storeCfg
			},
		},
	}
	cm = storage.NewClientMetrics()
)

// newTestStore creates a new Store for testing.
func newTestChunkStore(t require.TestingT, schemaName string) (storage.Store, config.SchemaConfig) {
	var storeCfg config.ChunkStoreConfig
	flagext.DefaultValues(&storeCfg)
	return newTestChunkStoreConfig(t, schemaName, storeCfg)
}

func newTestChunkStoreConfig(t require.TestingT, schemaName string, storeCfg config.ChunkStoreConfig) (storage.Store, config.SchemaConfig) {
	schemaCfg := testutils.SchemaConfig("", schemaName, 0)
	schemaCfg.Configs[0].IndexType = "inmemory"
	schemaCfg.Configs[0].ObjectType = "inmemory"

	return newTestChunkStoreConfigWithMockStorage(t, schemaCfg, storeCfg), schemaCfg
}

func newTestChunkStoreConfigWithMockStorage(t require.TestingT, schemaCfg config.SchemaConfig, storeCfg config.ChunkStoreConfig) storage.Store {
	testutils.ResetMockStorage()
	var tbmConfig index.TableManagerConfig
	err := schemaCfg.Validate()
	require.NoError(t, err)
	flagext.DefaultValues(&tbmConfig)

	limits, err := validation.NewOverrides(validation.Limits{
		MaxQueryLength: model.Duration(30 * 24 * time.Hour),
	}, nil)
	require.NoError(t, err)

	store, err := storage.NewStore(storage.Config{MaxChunkBatchSize: 1}, storeCfg, schemaCfg, limits, cm, prometheus.NewRegistry(), log.NewNopLogger(), constants.Loki)
	require.NoError(t, err)
	tm, err := index.NewTableManager(tbmConfig, schemaCfg, 12*time.Hour, testutils.NewMockStorage(), nil, nil, nil, log.NewNopLogger())
	require.NoError(t, err)
	_ = tm.SyncTables(context.Background())
	return store
}

func TestChunkStore_LabelValuesForMetricName(t *testing.T) {
	now := model.Now()

	fooMetric1 := labels.FromStrings(labels.MetricName, "foo",
		"bar", "baz",
		"flip", "flop",
		"toms", "code",
	)
	fooMetric2 := labels.FromStrings(labels.MetricName, "foo",
		"bar", "beep",
		"toms", "code",
	)
	fooMetric3 := labels.FromStrings(labels.MetricName, "foo",
		"bar", "bop",
		"flip", "flap",
	)

	// barMetric1 is a subset of barMetric2 to test over-matching bug.
	barMetric1 := labels.FromStrings(labels.MetricName, "bar",
		"bar", "baz",
	)
	barMetric2 := labels.FromStrings(labels.MetricName, "bar",
		"bar", "baz",
		"toms", "code",
	)

	for _, tc := range []struct {
		metricName, labelName string
		expect                []string
		matchers              []*labels.Matcher
	}{
		{
			`foo`, `bar`,
			[]string{"baz", "beep", "bop"},
			nil,
		},
		{
			`bar`, `toms`,
			[]string{"code"},
			nil,
		},
		{
			`bar`, `bar`,
			[]string{"baz"},
			nil,
		},
		{
			`foo`, `foo`,
			nil,
			nil,
		},
		{
			`foo`, `flip`,
			[]string{"flap", "flop"},
			nil,
		},
		{
			`foo`, `toms`,
			[]string{"code"},
			[]*labels.Matcher{
				labels.MustNewMatcher(labels.MatchRegexp, "bar", "baz|beep"),
			},
		},
	} {
		for _, schema := range schemas {
			for _, storeCase := range stores {
				t.Run(fmt.Sprintf("%s / %s / %s / %s", tc.metricName, tc.labelName, schema, storeCase.name), func(t *testing.T) {
					t.Log("========= Running labelValues with metricName", tc.metricName, "with labelName", tc.labelName, "with schema", schema)
					storeCfg := storeCase.configFn()
					store, schemaCfg := newTestChunkStoreConfig(t, schema, storeCfg)
					defer store.Stop()

					chunkFmt, headBlockFmt, _ := schemaCfg.Configs[0].ChunkFormat()

					fooChunk1 := dummyChunkWithFormat(t, now, fooMetric1, chunkFmt, headBlockFmt)
					fooChunk2 := dummyChunkWithFormat(t, now, fooMetric2, chunkFmt, headBlockFmt)
					fooChunk3 := dummyChunkWithFormat(t, now, fooMetric3, chunkFmt, headBlockFmt)
					barChunk1 := dummyChunkWithFormat(t, now, barMetric1, chunkFmt, headBlockFmt)
					barChunk2 := dummyChunkWithFormat(t, now, barMetric2, chunkFmt, headBlockFmt)

					if err := store.Put(ctx, []chunk.Chunk{
						fooChunk1,
						fooChunk2,
						fooChunk3,
						barChunk1,
						barChunk2,
					}); err != nil {
						t.Fatal(err)
					}

					// Query with ordinary time-range
					labelValues1, err := store.LabelValuesForMetricName(ctx, userID, now.Add(-time.Hour), now, tc.metricName, tc.labelName, tc.matchers...)
					require.NoError(t, err)

					if !reflect.DeepEqual(tc.expect, labelValues1) {
						t.Fatalf("%s/%s: wrong label values - %s", tc.metricName, tc.labelName, test.Diff(tc.expect, labelValues1))
					}

					// Pushing end of time-range into future should yield exact same resultset
					labelValues2, err := store.LabelValuesForMetricName(ctx, userID, now.Add(-time.Hour), now.Add(time.Hour*24*10), tc.metricName, tc.labelName, tc.matchers...)
					require.NoError(t, err)

					if !reflect.DeepEqual(tc.expect, labelValues2) {
						t.Fatalf("%s/%s: wrong label values - %s", tc.metricName, tc.labelName, test.Diff(tc.expect, labelValues2))
					}

					// Query with both begin & end of time-range in future should yield empty resultset
					labelValues3, err := store.LabelValuesForMetricName(ctx, userID, now.Add(time.Hour), now.Add(time.Hour*2), tc.metricName, tc.labelName, tc.matchers...)
					require.NoError(t, err)
					if len(labelValues3) != 0 {
						t.Fatalf("%s/%s: future query should yield empty resultset ... actually got %v label values: %#v",
							tc.metricName, tc.labelName, len(labelValues3), labelValues3)
					}
				})
			}
		}
	}
}

func TestChunkStore_LabelNamesForMetricName(t *testing.T) {
	now := model.Now()

	fooMetric1 := labels.FromStrings(labels.MetricName, "foo",
		"bar", "baz",
		"flip", "flop",
		"toms", "code",
	)
	fooMetric2 := labels.FromStrings(labels.MetricName, "foo",
		"bar", "beep",
		"toms", "code",
	)
	fooMetric3 := labels.FromStrings(labels.MetricName, "foo",
		"bar", "bop",
		"flip", "flap",
	)

	// barMetric1 is a subset of barMetric2 to test over-matching bug.
	barMetric1 := labels.FromStrings(labels.MetricName, "bar",
		"bar", "baz",
	)
	barMetric2 := labels.FromStrings(labels.MetricName, "bar",
		"bar", "baz",
		"toms", "code",
	)

	for _, tc := range []struct {
		metricName string
		expect     []string
		matchers   []*labels.Matcher
	}{
		{
			`foo`,
			[]string{"bar", "flip", "toms"},
			nil,
		},
		{
			`bar`,
			[]string{"bar", "toms"},
			nil,
		},
		{
			`foo`,
			[]string{"bar", "toms"},
			[]*labels.Matcher{
				labels.MustNewMatcher(labels.MatchRegexp, "bar", "beep"),
			},
		},
	} {
		for _, schema := range schemas {
			for _, storeCase := range stores {
				t.Run(fmt.Sprintf("%s / %s / %s ", tc.metricName, schema, storeCase.name), func(t *testing.T) {
					t.Log("========= Running labelNames with metricName", tc.metricName, "with schema", schema)
					storeCfg := storeCase.configFn()
					store, schemaCfg := newTestChunkStoreConfig(t, schema, storeCfg)
					defer store.Stop()

					chunkFmt, headBlockFmt, _ := schemaCfg.Configs[0].ChunkFormat()

					fooChunk1 := dummyChunkWithFormat(t, now, fooMetric1, chunkFmt, headBlockFmt)
					fooChunk2 := dummyChunkWithFormat(t, now, fooMetric2, chunkFmt, headBlockFmt)
					fooChunk3 := dummyChunkWithFormat(t, now, fooMetric3, chunkFmt, headBlockFmt)
					fooChunk4 := dummyChunkWithFormat(t, now.Add(-time.Hour), fooMetric1, chunkFmt, headBlockFmt) // same series but different chunk

					barChunk1 := dummyChunkWithFormat(t, now, barMetric1, chunkFmt, headBlockFmt)
					barChunk2 := dummyChunkWithFormat(t, now, barMetric2, chunkFmt, headBlockFmt)

					if err := store.Put(ctx, []chunk.Chunk{
						fooChunk1,
						fooChunk2,
						fooChunk3,
						fooChunk4,
						barChunk1,
						barChunk2,
					}); err != nil {
						t.Fatal(err)
					}

					// Query with ordinary time-range
					labelNames1, err := store.LabelNamesForMetricName(ctx, userID, now.Add(-time.Hour), now, tc.metricName, tc.matchers...)
					require.NoError(t, err)

					if !reflect.DeepEqual(tc.expect, labelNames1) {
						t.Fatalf("%s: wrong label name - %s", tc.metricName, test.Diff(tc.expect, labelNames1))
					}

					// Pushing end of time-range into future should yield exact same resultset
					labelNames2, err := store.LabelNamesForMetricName(ctx, userID, now.Add(-time.Hour), now.Add(time.Hour*24*10), tc.metricName, tc.matchers...)
					require.NoError(t, err)

					if !reflect.DeepEqual(tc.expect, labelNames2) {
						t.Fatalf("%s: wrong label name - %s", tc.metricName, test.Diff(tc.expect, labelNames2))
					}

					// Query with both begin & end of time-range in future should yield empty resultset
					labelNames3, err := store.LabelNamesForMetricName(ctx, userID, now.Add(time.Hour), now.Add(time.Hour*2), tc.metricName, tc.matchers...)
					require.NoError(t, err)
					if len(labelNames3) != 0 {
						t.Fatalf("%s: future query should yield empty resultset ... actually got %v label names: %#v",
							tc.metricName, len(labelNames3), labelNames3)
					}
				})
			}
		}
	}
}

// TestChunkStore_getMetricNameChunks tests if chunks are fetched correctly when we have the metric name
func TestChunkStore_getMetricNameChunks(t *testing.T) {
	for _, schema := range schemas {
		for _, storeCase := range stores {
			storeCfg := storeCase.configFn()

			store, schemaCfg := newTestChunkStoreConfig(t, schema, storeCfg)
			defer store.Stop()

			chunkFmt, headBlockFmt, _ := schemaCfg.Configs[0].ChunkFormat()

			now := model.Now()
			chunk1 := dummyChunkWithFormat(t, now, labels.FromStrings(labels.MetricName, "foo",
				"bar", "baz",
				"flip", "flop",
				"toms", "code",
			), chunkFmt, headBlockFmt)
			chunk2 := dummyChunkWithFormat(t, now, labels.FromStrings(labels.MetricName, "foo",
				"bar", "beep",
				"toms", "code",
			), chunkFmt, headBlockFmt)

			if err := store.Put(ctx, []chunk.Chunk{chunk1, chunk2}); err != nil {
				t.Fatal(err)
			}

			testCases := []struct {
				query  string
				expect []chunk.Chunk
			}{
				{
					`foo`,
					[]chunk.Chunk{chunk1, chunk2},
				},
				{
					`foo{flip=""}`,
					[]chunk.Chunk{chunk2},
				},
				{
					`foo{bar="baz"}`,
					[]chunk.Chunk{chunk1},
				},
				{
					`foo{bar="beep"}`,
					[]chunk.Chunk{chunk2},
				},
				{
					`foo{toms="code"}`,
					[]chunk.Chunk{chunk1, chunk2},
				},
				{
					`foo{bar!="baz"}`,
					[]chunk.Chunk{chunk2},
				},
				{
					`foo{bar=~"beep|baz"}`,
					[]chunk.Chunk{chunk1, chunk2},
				},
				{
					`foo{bar=~"beeping|baz"}`,
					[]chunk.Chunk{chunk1},
				},
				{
					`foo{toms="code", bar=~"beep|baz"}`,
					[]chunk.Chunk{chunk1, chunk2},
				},
				{
					`foo{toms="code", bar="baz"}`,
					[]chunk.Chunk{chunk1},
				},
			}

			for _, tc := range testCases {
				t.Run(fmt.Sprintf("%s / %s / %s", tc.query, schema, storeCase.name), func(t *testing.T) {
					matchers, err := parser.ParseMetricSelector(tc.query)
					if err != nil {
						t.Fatal(err)
					}

					chunks, fetchers, err := store.GetChunks(ctx, userID, now.Add(-time.Hour), now, chunk.NewPredicate(matchers, nil), nil)
					require.NoError(t, err)
					fetchedChunk := []chunk.Chunk{}
					for _, f := range fetchers {
						for _, cs := range chunks {
							cks, err := f.FetchChunks(ctx, cs)
							if err != nil {
								t.Fatal(err)
							}
						outer:
							for _, c := range cks {
								for _, matcher := range matchers {
									if !matcher.Matches(c.Metric.Get(matcher.Name)) {
										continue outer
									}
								}
								fetchedChunk = append(fetchedChunk, c)
							}

						}
					}

					for i, c := range fetchedChunk {
						require.Equal(t, tc.expect[i].ChunkRef, c.ChunkRef)
						require.Equal(t, tc.expect[i].Metric, c.Metric)
						require.Equal(t, tc.expect[i].Encoding, c.Encoding)
						ee, err := tc.expect[i].Encoded()
						require.NoError(t, err)
						fe, err := c.Encoded()
						require.NoError(t, err)
						require.Equal(t, ee, fe)
					}
				})
			}
		}
	}
}

func Test_GetSeries(t *testing.T) {
	now := model.Now()
	ch1lbs := labels.FromStrings(labels.MetricName, "foo",
		"bar", "baz",
		"flip", "flop",
		"toms", "code",
	)
	ch2lbs := labels.FromStrings(labels.MetricName, "foo",
		"bar", "beep",
		"toms", "code",
	)

	testCases := []struct {
		query  string
		expect []labels.Labels
	}{
		{
			`foo`,
			[]labels.Labels{
				labels.NewBuilder(ch1lbs).Del(labels.MetricName).Labels(),
				labels.NewBuilder(ch2lbs).Del(labels.MetricName).Labels(),
			},
		},
		{
			`foo{flip=""}`,
			[]labels.Labels{labels.NewBuilder(ch2lbs).Del(labels.MetricName).Labels()},
		},
		{
			`foo{bar="baz"}`,
			[]labels.Labels{labels.NewBuilder(ch1lbs).Del(labels.MetricName).Labels()},
		},
		{
			`foo{bar="beep"}`,
			[]labels.Labels{labels.NewBuilder(ch2lbs).Del(labels.MetricName).Labels()},
		},
		{
			`foo{toms="code"}`,
			[]labels.Labels{
				labels.NewBuilder(ch1lbs).Del(labels.MetricName).Labels(),
				labels.NewBuilder(ch2lbs).Del(labels.MetricName).Labels(),
			},
		},
		{
			`foo{bar!="baz"}`,
			[]labels.Labels{labels.NewBuilder(ch2lbs).Del(labels.MetricName).Labels()},
		},
		{
			`foo{bar=~"beep|baz"}`,
			[]labels.Labels{
				labels.NewBuilder(ch1lbs).Del(labels.MetricName).Labels(),
				labels.NewBuilder(ch2lbs).Del(labels.MetricName).Labels(),
			},
		},
		{
			`foo{bar=~"beeping|baz"}`,
			[]labels.Labels{labels.NewBuilder(ch1lbs).Del(labels.MetricName).Labels()},
		},
		{
			`foo{toms="code", bar=~"beep|baz"}`,
			[]labels.Labels{
				labels.NewBuilder(ch1lbs).Del(labels.MetricName).Labels(),
				labels.NewBuilder(ch2lbs).Del(labels.MetricName).Labels(),
			},
		},
		{
			`foo{toms="code", bar="baz"}`,
			[]labels.Labels{labels.NewBuilder(ch1lbs).Del(labels.MetricName).Labels()},
		},
	}
	for _, schema := range schemas {
		for _, storeCase := range stores {
			storeCfg := storeCase.configFn()

			store, schemaCfg := newTestChunkStoreConfig(t, schema, storeCfg)
			defer store.Stop()

			chunkFmt, headBlockFmt, _ := schemaCfg.Configs[0].ChunkFormat()
			chunk1 := dummyChunkWithFormat(t, now, ch1lbs, chunkFmt, headBlockFmt)
			chunk2 := dummyChunkWithFormat(t, now, ch2lbs, chunkFmt, headBlockFmt)

			if err := store.Put(ctx, []chunk.Chunk{chunk1, chunk2}); err != nil {
				t.Fatal(err)
			}

			for _, tc := range testCases {
				t.Run(fmt.Sprintf("%s / %s / %s", tc.query, schema, storeCase.name), func(t *testing.T) {
					matchers, err := parser.ParseMetricSelector(tc.query)
					if err != nil {
						t.Fatal(err)
					}

					res, err := store.GetSeries(ctx, userID, now.Add(-time.Hour), now, matchers...)
					require.NoError(t, err)
					require.Equal(t, tc.expect, res)
				})
			}
		}
	}
}

func Test_GetSeriesShard(t *testing.T) {
	now := model.Now()
	ch1lbs := labels.FromStrings(labels.MetricName, "foo",
		"bar", "baz",
		"flip", "flop",
		"toms", "code",
	)
	ch2lbs := labels.FromStrings(labels.MetricName, "foo",
		"bar", "beep",
		"toms", "code",
	)

	testCases := []struct {
		query  string
		expect []labels.Labels
	}{
		{
			`foo{__cortex_shard__="6_of_16"}`,
			[]labels.Labels{labels.NewBuilder(ch2lbs).Del(labels.MetricName).Labels()},
		},
		{
			`foo{__cortex_shard__="8_of_16"}`,
			[]labels.Labels{labels.NewBuilder(ch1lbs).Del(labels.MetricName).Labels()},
		},
	}
	for _, storeCase := range stores {
		storeCfg := storeCase.configFn()

		store, schemaCfg := newTestChunkStoreConfig(t, "v12", storeCfg)
		defer store.Stop()

		chunkFmt, headBlockFmt, _ := schemaCfg.Configs[0].ChunkFormat()
		chunk1 := dummyChunkWithFormat(t, now, ch1lbs, chunkFmt, headBlockFmt)
		chunk2 := dummyChunkWithFormat(t, now, ch2lbs, chunkFmt, headBlockFmt)

		if err := store.Put(ctx, []chunk.Chunk{chunk1, chunk2}); err != nil {
			t.Fatal(err)
		}

		for _, tc := range testCases {
			t.Run(fmt.Sprintf("%s / %s / %s", tc.query, "v12", storeCase.name), func(t *testing.T) {
				t.Log("========= Running query", tc.query, "with schema", "v12")
				matchers, err := parser.ParseMetricSelector(tc.query)
				if err != nil {
					t.Fatal(err)
				}

				res, err := store.GetSeries(ctx, userID, now.Add(-time.Hour), now, matchers...)
				require.NoError(t, err)
				require.Equal(t, tc.expect, res)
			})
		}
	}
}

// nolint
func mustNewLabelMatcher(matchType labels.MatchType, name string, value string) *labels.Matcher {
	return labels.MustNewMatcher(matchType, name, value)
}

func BenchmarkIndexCaching(b *testing.B) {
	ctx := context.Background()
	storeMaker := stores[1]
	storeCfg := storeMaker.configFn()

	store, schemaCfg := newTestChunkStoreConfig(b, "v9", storeCfg)
	defer store.Stop()

	chunkFmt, headBlockFmt, _ := schemaCfg.Configs[0].ChunkFormat()

	fooChunk1 := dummyChunkWithFormat(b, model.Time(0).Add(15*time.Second), BenchmarkLabels, chunkFmt, headBlockFmt)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		err := store.Put(ctx, []chunk.Chunk{fooChunk1})
		require.NoError(b, err)
	}
}

func TestChunkStoreError(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		query         string
		from, through model.Time
		err           string
	}{
		{
			query:   "foo",
			from:    model.Time(0).Add(31 * 24 * time.Hour),
			through: model.Time(0),
			err:     "invalid query, through < from (0 < 2678400)",
		},
		{
			query:   "foo",
			from:    model.Time(0),
			through: model.Time(0).Add(31 * 24 * time.Hour),
			err:     "the query time range exceeds the limit (query length: 31d, limit: 30d)",
		},
		{
			query:   "{foo=\"bar\"}",
			from:    model.Time(0),
			through: model.Time(0).Add(1 * time.Hour),
			err:     "query must contain metric name",
		},
		{
			query:   "{__name__=~\"bar\"}",
			from:    model.Time(0),
			through: model.Time(0).Add(1 * time.Hour),
			err:     "query must contain metric name",
		},
	} {
		for _, schema := range schemas {
			t.Run(fmt.Sprintf("%s / %s", tc.query, schema), func(t *testing.T) {
				store, _ := newTestChunkStore(t, schema)
				defer store.Stop()

				matchers, err := parser.ParseMetricSelector(tc.query)
				require.NoError(t, err)

				// Query with ordinary time-range
				_, _, err = store.GetChunks(ctx, userID, tc.from, tc.through, chunk.NewPredicate(matchers, nil), nil)
				require.EqualError(t, err, tc.err)
			})
		}
	}
}

func TestSeriesStore_LabelValuesForMetricName(t *testing.T) {
	now := model.Now()

	fooMetric1 := labels.FromStrings(labels.MetricName, "foo",
		"bar", "baz",
		"flip", "flop",
		"toms", "code",
		"env", "dev",
		"class", "not-secret",
	)
	fooMetric2 := labels.FromStrings(labels.MetricName, "foo",
		"bar", "beep",
		"toms", "code",
		"env", "prod",
		"class", "secret",
	)

	for _, tc := range []struct {
		metricName, labelName string
		expect                []string
		matchers              []*labels.Matcher
	}{
		{
			`foo`, `class`,
			[]string{"not-secret"},
			[]*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "env", "dev")},
		},
		{
			`foo`, `bar`,
			[]string{"baz"},
			[]*labels.Matcher{
				labels.MustNewMatcher(labels.MatchNotEqual, "env", "prod"),
				labels.MustNewMatcher(labels.MatchEqual, "toms", "code"),
			},
		},
		{
			`foo`, `toms`,
			[]string{"code"},
			[]*labels.Matcher{
				labels.MustNewMatcher(labels.MatchRegexp, "env", "dev|prod"),
			},
		},
	} {
		for _, schema := range schemas {
			for _, storeCase := range stores {
				t.Run(fmt.Sprintf("%s / %s / %s / %s", tc.metricName, tc.labelName, schema, storeCase.name), func(t *testing.T) {
					t.Log("========= Running labelValues with metricName", tc.metricName, "with labelName", tc.labelName, "with schema", schema)
					storeCfg := storeCase.configFn()
					store, schemaCfg := newTestChunkStoreConfig(t, schema, storeCfg)
					defer store.Stop()

					chunkFmt, headBlockFmt, _ := schemaCfg.Configs[0].ChunkFormat()
					fooChunk1 := dummyChunkWithFormat(t, now, fooMetric1, chunkFmt, headBlockFmt)
					fooChunk2 := dummyChunkWithFormat(t, now, fooMetric2, chunkFmt, headBlockFmt)

					if err := store.Put(ctx, []chunk.Chunk{
						fooChunk1,
						fooChunk2,
					}); err != nil {
						t.Fatal(err)
					}

					// Query with ordinary time-range
					labelValues1, err := store.LabelValuesForMetricName(ctx, userID, now.Add(-time.Hour), now, tc.metricName, tc.labelName, tc.matchers...)
					require.NoError(t, err)
					require.ElementsMatch(t, tc.expect, labelValues1)

					// Pushing end of time-range into future should yield exact same resultset
					labelValues2, err := store.LabelValuesForMetricName(ctx, userID, now.Add(-time.Hour), now.Add(time.Hour*24*10), tc.metricName, tc.labelName, tc.matchers...)
					require.NoError(t, err)
					require.ElementsMatch(t, tc.expect, labelValues2)
				})
			}
		}
	}
}

func dummyChunkWithFormat(t testing.TB, now model.Time, metric labels.Labels, format byte, headfmt chunkenc.HeadBlockFmt) chunk.Chunk {
	t.Helper()

	samples := 1
	chunkStart := now.Add(-time.Hour)

	chk := chunkenc.NewMemChunk(format, compression.GZIP, headfmt, 256*1024, 0)
	for i := 0; i < samples; i++ {
		ts := time.Duration(i) * 15 * time.Second
		dup, err := chk.Append(&logproto.Entry{Timestamp: chunkStart.Time().Add(ts), Line: fmt.Sprintf("line %d", i)})
		require.False(t, dup)
		require.NoError(t, err)
	}

	chunk := chunk.NewChunk(
		userID,
		client.Fingerprint(metric),
		metric,
		chunkenc.NewFacade(chk, 0, 0),
		chunkStart,
		now,
	)
	// Force checksum calculation.
	if err := chunk.Encode(); err != nil {
		t.Fatal(err)
	}
	return chunk
}

// BenchmarkLabels is a real example from Kubernetes' embedded cAdvisor metrics, lightly obfuscated
var BenchmarkLabels = labels.FromStrings(model.MetricNameLabel, "container_cpu_usage_seconds_total",
	"beta_kubernetes_io_arch", "amd64",
	"beta_kubernetes_io_instance_type", "c3.somesize",
	"beta_kubernetes_io_os", "linux",
	"container_name", "some-name",
	"cpu", "cpu01",
	"failure_domain_beta_kubernetes_io_region", "somewhere-1",
	"failure_domain_beta_kubernetes_io_zone", "somewhere-1b",
	"id", "/kubepods/burstable/pod6e91c467-e4c5-11e7-ace3-0a97ed59c75e/a3c8498918bd6866349fed5a6f8c643b77c91836427fb6327913276ebc6bde28",
	"image", "registry/organisation/name@sha256:dca3d877a80008b45d71d7edc4fd2e44c0c8c8e7102ba5cbabec63a374d1d506",
	"instance", "ip-111-11-1-11.ec2.internal",
	"job", "kubernetes-cadvisor",
	"kubernetes_io_hostname", "ip-111-11-1-11",
	"monitor", "prod",
	"name", "k8s_some-name_some-other-name-5j8s8_kube-system_6e91c467-e4c5-11e7-ace3-0a97ed59c75e_0",
	"namespace", "kube-system",
	"pod_name", "some-other-name-5j8s8",
)
