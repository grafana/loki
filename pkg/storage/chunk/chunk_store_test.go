package chunk

import (
	"context"
	"fmt"
	"math/rand"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/test"

	"github.com/grafana/loki/pkg/storage/chunk/cache"
	"github.com/grafana/loki/pkg/storage/chunk/encoding"
	"github.com/grafana/loki/pkg/util/validation"
)

type configFactory func() StoreConfig

var schemas = []string{"v9", "v10", "v11", "v12"}

var stores = []struct {
	name     string
	configFn configFactory
}{
	{
		name: "store",
		configFn: func() StoreConfig {
			var storeCfg StoreConfig
			flagext.DefaultValues(&storeCfg)
			return storeCfg
		},
	},
	{
		name: "cached_store",
		configFn: func() StoreConfig {
			var storeCfg StoreConfig
			flagext.DefaultValues(&storeCfg)
			storeCfg.WriteDedupeCacheConfig.Cache = cache.NewFifoCache("test", cache.FifoCacheConfig{
				MaxSizeItems: 500,
			}, prometheus.NewRegistry(), log.NewNopLogger())
			return storeCfg
		},
	},
}

// newTestStore creates a new Store for testing.
func newTestChunkStore(t require.TestingT, schemaName string) (Store, SchemaConfig) {
	var storeCfg StoreConfig
	flagext.DefaultValues(&storeCfg)
	return newTestChunkStoreConfig(t, schemaName, storeCfg)
}

func newTestChunkStoreConfig(t require.TestingT, schemaName string, storeCfg StoreConfig) (Store, SchemaConfig) {
	schemaCfg := DefaultSchemaConfig("", schemaName, 0)

	schema, err := schemaCfg.Configs[0].CreateSchema()
	require.NoError(t, err)

	return newTestChunkStoreConfigWithMockStorage(t, schemaCfg, schema, storeCfg), schemaCfg
}

func newTestChunkStoreConfigWithMockStorage(t require.TestingT, schemaCfg SchemaConfig, schema SeriesStoreSchema, storeCfg StoreConfig) Store {
	var tbmConfig TableManagerConfig
	err := schemaCfg.Validate()
	require.NoError(t, err)
	flagext.DefaultValues(&tbmConfig)
	storage := NewMockStorage()
	tableManager, err := NewTableManager(tbmConfig, schemaCfg, maxChunkAge, storage, nil, nil, nil)
	require.NoError(t, err)

	err = tableManager.SyncTables(context.Background())
	require.NoError(t, err)

	var limits validation.Limits
	flagext.DefaultValues(&limits)
	limits.MaxQueryLength = model.Duration(30 * 24 * time.Hour)
	overrides, err := validation.NewOverrides(limits, nil)
	require.NoError(t, err)

	reg := prometheus.NewRegistry()
	logger := log.NewNopLogger()
	chunksCache, err := cache.New(storeCfg.ChunkCacheConfig, reg, logger)
	require.NoError(t, err)
	writeDedupeCache, err := cache.New(storeCfg.WriteDedupeCacheConfig, reg, logger)
	require.NoError(t, err)

	store := NewCompositeStore(nil)
	err = store.addSchema(storeCfg, schemaCfg.Configs[0], schema, schemaCfg.Configs[0].From.Time, storage, storage, overrides, chunksCache, writeDedupeCache)
	require.NoError(t, err)
	return store
}

func TestChunkStore_LabelValuesForMetricName(t *testing.T) {
	ctx := context.Background()
	now := model.Now()

	fooMetric1 := labels.Labels{
		{Name: labels.MetricName, Value: "foo"},
		{Name: "bar", Value: "baz"},
		{Name: "flip", Value: "flop"},
		{Name: "toms", Value: "code"},
	}
	fooMetric2 := labels.Labels{
		{Name: labels.MetricName, Value: "foo"},
		{Name: "bar", Value: "beep"},
		{Name: "toms", Value: "code"},
	}
	fooMetric3 := labels.Labels{
		{Name: labels.MetricName, Value: "foo"},
		{Name: "bar", Value: "bop"},
		{Name: "flip", Value: "flap"},
	}

	// barMetric1 is a subset of barMetric2 to test over-matching bug.
	barMetric1 := labels.Labels{
		{Name: labels.MetricName, Value: "bar"},
		{Name: "bar", Value: "baz"},
	}
	barMetric2 := labels.Labels{
		{Name: labels.MetricName, Value: "bar"},
		{Name: "bar", Value: "baz"},
		{Name: "toms", Value: "code"},
	}

	fooChunk1 := dummyChunkFor(now, fooMetric1)
	fooChunk2 := dummyChunkFor(now, fooMetric2)
	fooChunk3 := dummyChunkFor(now, fooMetric3)

	barChunk1 := dummyChunkFor(now, barMetric1)
	barChunk2 := dummyChunkFor(now, barMetric2)

	for _, tc := range []struct {
		metricName, labelName string
		expect                []string
	}{
		{
			`foo`, `bar`,
			[]string{"baz", "beep", "bop"},
		},
		{
			`bar`, `toms`,
			[]string{"code"},
		},
		{
			`bar`, `bar`,
			[]string{"baz"},
		},
		{
			`foo`, `foo`,
			nil,
		},
		{
			`foo`, `flip`,
			[]string{"flap", "flop"},
		},
	} {
		for _, schema := range schemas {
			for _, storeCase := range stores {
				t.Run(fmt.Sprintf("%s / %s / %s / %s", tc.metricName, tc.labelName, schema, storeCase.name), func(t *testing.T) {
					t.Log("========= Running labelValues with metricName", tc.metricName, "with labelName", tc.labelName, "with schema", schema)
					storeCfg := storeCase.configFn()
					store, _ := newTestChunkStoreConfig(t, schema, storeCfg)
					defer store.Stop()

					if err := store.Put(ctx, []Chunk{
						fooChunk1,
						fooChunk2,
						fooChunk3,
						barChunk1,
						barChunk2,
					}); err != nil {
						t.Fatal(err)
					}

					// Query with ordinary time-range
					labelValues1, err := store.LabelValuesForMetricName(ctx, userID, now.Add(-time.Hour), now, tc.metricName, tc.labelName)
					require.NoError(t, err)

					if !reflect.DeepEqual(tc.expect, labelValues1) {
						t.Fatalf("%s/%s: wrong label values - %s", tc.metricName, tc.labelName, test.Diff(tc.expect, labelValues1))
					}

					// Pushing end of time-range into future should yield exact same resultset
					labelValues2, err := store.LabelValuesForMetricName(ctx, userID, now.Add(-time.Hour), now.Add(time.Hour*24*10), tc.metricName, tc.labelName)
					require.NoError(t, err)

					if !reflect.DeepEqual(tc.expect, labelValues2) {
						t.Fatalf("%s/%s: wrong label values - %s", tc.metricName, tc.labelName, test.Diff(tc.expect, labelValues2))
					}

					// Query with both begin & end of time-range in future should yield empty resultset
					labelValues3, err := store.LabelValuesForMetricName(ctx, userID, now.Add(time.Hour), now.Add(time.Hour*2), tc.metricName, tc.labelName)
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
	ctx := context.Background()
	now := model.Now()

	fooMetric1 := labels.Labels{
		{Name: labels.MetricName, Value: "foo"},
		{Name: "bar", Value: "baz"},
		{Name: "flip", Value: "flop"},
		{Name: "toms", Value: "code"},
	}
	fooMetric2 := labels.Labels{
		{Name: labels.MetricName, Value: "foo"},
		{Name: "bar", Value: "beep"},
		{Name: "toms", Value: "code"},
	}
	fooMetric3 := labels.Labels{
		{Name: labels.MetricName, Value: "foo"},
		{Name: "bar", Value: "bop"},
		{Name: "flip", Value: "flap"},
	}

	// barMetric1 is a subset of barMetric2 to test over-matching bug.
	barMetric1 := labels.Labels{
		{Name: labels.MetricName, Value: "bar"},
		{Name: "bar", Value: "baz"},
	}
	barMetric2 := labels.Labels{
		{Name: labels.MetricName, Value: "bar"},
		{Name: "bar", Value: "baz"},
		{Name: "toms", Value: "code"},
	}

	fooChunk1 := dummyChunkFor(now, fooMetric1)
	fooChunk2 := dummyChunkFor(now, fooMetric2)
	fooChunk3 := dummyChunkFor(now, fooMetric3)
	fooChunk4 := dummyChunkFor(now.Add(-time.Hour), fooMetric1) // same series but different chunk

	barChunk1 := dummyChunkFor(now, barMetric1)
	barChunk2 := dummyChunkFor(now, barMetric2)

	for _, tc := range []struct {
		metricName string
		expect     []string
	}{
		{
			`foo`,
			[]string{labels.MetricName, "bar", "flip", "toms"},
		},
		{
			`bar`,
			[]string{labels.MetricName, "bar", "toms"},
		},
	} {
		for _, schema := range schemas {
			for _, storeCase := range stores {
				t.Run(fmt.Sprintf("%s / %s / %s ", tc.metricName, schema, storeCase.name), func(t *testing.T) {
					t.Log("========= Running labelNames with metricName", tc.metricName, "with schema", schema)
					storeCfg := storeCase.configFn()
					store, _ := newTestChunkStoreConfig(t, schema, storeCfg)
					defer store.Stop()

					if err := store.Put(ctx, []Chunk{
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
					labelNames1, err := store.LabelNamesForMetricName(ctx, userID, now.Add(-time.Hour), now, tc.metricName)
					require.NoError(t, err)

					if !reflect.DeepEqual(tc.expect, labelNames1) {
						t.Fatalf("%s: wrong label name - %s", tc.metricName, test.Diff(tc.expect, labelNames1))
					}

					// Pushing end of time-range into future should yield exact same resultset
					labelNames2, err := store.LabelNamesForMetricName(ctx, userID, now.Add(-time.Hour), now.Add(time.Hour*24*10), tc.metricName)
					require.NoError(t, err)

					if !reflect.DeepEqual(tc.expect, labelNames2) {
						t.Fatalf("%s: wrong label name - %s", tc.metricName, test.Diff(tc.expect, labelNames2))
					}

					// Query with both begin & end of time-range in future should yield empty resultset
					labelNames3, err := store.LabelNamesForMetricName(ctx, userID, now.Add(time.Hour), now.Add(time.Hour*2), tc.metricName)
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
	ctx := context.Background()
	now := model.Now()
	chunk1 := dummyChunkFor(now, labels.Labels{
		{Name: labels.MetricName, Value: "foo"},
		{Name: "bar", Value: "baz"},
		{Name: "flip", Value: "flop"},
		{Name: "toms", Value: "code"},
	})
	chunk2 := dummyChunkFor(now, labels.Labels{
		{Name: labels.MetricName, Value: "foo"},
		{Name: "bar", Value: "beep"},
		{Name: "toms", Value: "code"},
	})

	testCases := []struct {
		query  string
		expect []Chunk
	}{
		{
			`foo`,
			[]Chunk{chunk1, chunk2},
		},
		{
			`foo{flip=""}`,
			[]Chunk{chunk2},
		},
		{
			`foo{bar="baz"}`,
			[]Chunk{chunk1},
		},
		{
			`foo{bar="beep"}`,
			[]Chunk{chunk2},
		},
		{
			`foo{toms="code"}`,
			[]Chunk{chunk1, chunk2},
		},
		{
			`foo{bar!="baz"}`,
			[]Chunk{chunk2},
		},
		{
			`foo{bar=~"beep|baz"}`,
			[]Chunk{chunk1, chunk2},
		},
		{
			`foo{bar=~"beeping|baz"}`,
			[]Chunk{chunk1},
		},
		{
			`foo{toms="code", bar=~"beep|baz"}`,
			[]Chunk{chunk1, chunk2},
		},
		{
			`foo{toms="code", bar="baz"}`,
			[]Chunk{chunk1},
		},
	}
	for _, schema := range schemas {
		for _, storeCase := range stores {
			storeCfg := storeCase.configFn()

			store, schemaCfg := newTestChunkStoreConfig(t, schema, storeCfg)
			defer store.Stop()

			if err := store.Put(ctx, []Chunk{chunk1, chunk2}); err != nil {
				t.Fatal(err)
			}

			for _, tc := range testCases {
				t.Run(fmt.Sprintf("%s / %s / %s", tc.query, schema, storeCase.name), func(t *testing.T) {
					t.Log("========= Running query", tc.query, "with schema", schema)
					matchers, err := parser.ParseMetricSelector(tc.query)
					if err != nil {
						t.Fatal(err)
					}

					chunks, fetchers, err := store.GetChunkRefs(ctx, userID, now.Add(-time.Hour), now, matchers...)
					require.NoError(t, err)
					fetchedChunk := []Chunk{}
					for _, f := range fetchers {
						for _, cs := range chunks {
							keys := make([]string, 0, len(cs))
							sort.Slice(chunks, func(i, j int) bool { return schemaCfg.ExternalKey(cs[i]) < schemaCfg.ExternalKey(cs[j]) })

							for _, c := range cs {
								keys = append(keys, schemaCfg.ExternalKey(c))
							}
							cks, err := f.FetchChunks(ctx, cs, keys)
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

					if !reflect.DeepEqual(tc.expect, fetchedChunk) {
						t.Fatalf("%s: wrong chunks - %s", tc.query, test.Diff(tc.expect, fetchedChunk))
					}
				})
			}
		}
	}
}

// nolint
func mustNewLabelMatcher(matchType labels.MatchType, name string, value string) *labels.Matcher {
	return labels.MustNewMatcher(matchType, name, value)
}

func TestChunkStoreRandom(t *testing.T) {
	ctx := context.Background()

	for _, schema := range schemas {
		t.Run(schema, func(t *testing.T) {
			store, schemaCfg := newTestChunkStore(t, schema)
			defer store.Stop()

			// put 100 chunks from 0 to 99
			const chunkLen = 2 * 3600 // in seconds
			for i := 0; i < 100; i++ {
				ts := model.TimeFromUnix(int64(i * chunkLen))
				ch := encoding.New()
				nc, err := ch.Add(model.SamplePair{
					Timestamp: ts,
					Value:     model.SampleValue(float64(i)),
				})
				require.NoError(t, err)
				require.Nil(t, nc)
				chunk := NewChunk(
					userID,
					model.Fingerprint(1),
					labels.Labels{
						{Name: labels.MetricName, Value: "foo"},
						{Name: "bar", Value: "baz"},
					},
					ch,
					ts,
					ts.Add(chunkLen*time.Second).Add(-1*time.Second),
				)
				err = chunk.Encode()
				require.NoError(t, err)
				err = store.Put(ctx, []Chunk{chunk})
				require.NoError(t, err)
			}

			// pick two random numbers and do a query
			for i := 0; i < 100; i++ {
				start := rand.Int63n(99 * chunkLen)
				end := start + 1 + rand.Int63n((99*chunkLen)-start)
				assert.True(t, start < end)

				startTime := model.TimeFromUnix(start)
				endTime := model.TimeFromUnix(end)

				matchers := []*labels.Matcher{
					mustNewLabelMatcher(labels.MatchEqual, labels.MetricName, "foo"),
					mustNewLabelMatcher(labels.MatchEqual, "bar", "baz"),
				}
				chunks, fetchers, err := store.GetChunkRefs(ctx, userID, startTime, endTime, matchers...)
				require.NoError(t, err)
				fetchedChunk := make([]Chunk, 0, len(chunks))
				for _, f := range fetchers {
					for _, cs := range chunks {
						keys := make([]string, 0, len(cs))
						sort.Slice(chunks, func(i, j int) bool { return schemaCfg.ExternalKey(cs[i]) < schemaCfg.ExternalKey(cs[j]) })

						for _, c := range cs {
							keys = append(keys, schemaCfg.ExternalKey(c))
						}
						cks, err := f.FetchChunks(ctx, cs, keys)
						if err != nil {
							t.Fatal(err)
						}
						fetchedChunk = append(fetchedChunk, cks...)
					}
				}

				// We need to check that each chunk is in the time range
				for _, chunk := range fetchedChunk {
					assert.False(t, chunk.From.After(endTime))
					assert.False(t, chunk.Through.Before(startTime))
					samples, err := chunk.Samples(chunk.From, chunk.Through)
					assert.NoError(t, err)
					assert.Equal(t, 1, len(samples))
					// TODO verify chunk contents
				}

				// And check we got all the chunks we want
				numChunks := (end / chunkLen) - (start / chunkLen) + 1
				assert.Equal(t, int(numChunks), len(fetchedChunk))
			}
		})
	}
}

func TestChunkStoreLeastRead(t *testing.T) {
	// Test we don't read too much from the index
	ctx := context.Background()
	store, schemaCfg := newTestChunkStore(t, "v12")
	defer store.Stop()

	// Put 24 chunks 1hr chunks in the store
	const chunkLen = 60 // in seconds
	for i := 0; i < 24; i++ {
		ts := model.TimeFromUnix(int64(i * chunkLen))
		ch := encoding.New()
		nc, err := ch.Add(model.SamplePair{
			Timestamp: ts,
			Value:     model.SampleValue(float64(i)),
		})
		require.NoError(t, err)
		require.Nil(t, nc)
		chunk := NewChunk(
			userID,
			model.Fingerprint(1),
			labels.Labels{
				{Name: labels.MetricName, Value: "foo"},
				{Name: "bar", Value: "baz"},
			},
			ch,
			ts,
			ts.Add(chunkLen*time.Second),
		)
		t.Logf("Loop %d", i)
		err = chunk.Encode()
		require.NoError(t, err)
		err = store.Put(ctx, []Chunk{chunk})
		require.NoError(t, err)
	}

	// pick a random numbers and do a query to end of row
	for i := 1; i < 24; i++ {
		start := int64(i * chunkLen)
		end := int64(24 * chunkLen)
		assert.True(t, start <= end)

		startTime := model.TimeFromUnix(start)
		endTime := model.TimeFromUnix(end)
		matchers := []*labels.Matcher{
			mustNewLabelMatcher(labels.MatchEqual, labels.MetricName, "foo"),
			mustNewLabelMatcher(labels.MatchEqual, "bar", "baz"),
		}

		chunks, fetchers, err := store.GetChunkRefs(ctx, userID, startTime, endTime, matchers...)
		require.NoError(t, err)
		fetchedChunk := make([]Chunk, 0, len(chunks))
		for _, f := range fetchers {
			for _, cs := range chunks {
				keys := make([]string, 0, len(cs))
				sort.Slice(chunks, func(i, j int) bool { return schemaCfg.ExternalKey(cs[i]) < schemaCfg.ExternalKey(cs[j]) })

				for _, c := range cs {
					keys = append(keys, schemaCfg.ExternalKey(c))
				}
				cks, err := f.FetchChunks(ctx, cs, keys)
				if err != nil {
					t.Fatal(err)
				}
				fetchedChunk = append(fetchedChunk, cks...)
			}
		}

		// We need to check that each chunk is in the time range
		for _, chunk := range fetchedChunk {
			assert.False(t, chunk.From.After(endTime))
			assert.False(t, chunk.Through.Before(startTime))
			samples, err := chunk.Samples(chunk.From, chunk.Through)
			assert.NoError(t, err)
			assert.Equal(t, 1, len(samples))
		}

		// And check we got all the chunks we want
		numChunks := 24 - (start / chunkLen) + 1
		assert.Equal(t, int(numChunks), len(fetchedChunk))
	}
}

func TestIndexCachingWorks(t *testing.T) {
	ctx := context.Background()
	metric := labels.Labels{
		{Name: labels.MetricName, Value: "foo"},
		{Name: "bar", Value: "baz"},
	}
	storeMaker := stores[1]
	storeCfg := storeMaker.configFn()

	store, _ := newTestChunkStoreConfig(t, "v9", storeCfg)
	defer store.Stop()

	storage := store.(CompositeStore).stores[0].Store.(*seriesStore).fetcher.storage.(*MockStorage)

	fooChunk1 := dummyChunkFor(model.Time(0).Add(15*time.Second), metric)
	err := fooChunk1.Encode()
	require.NoError(t, err)
	err = store.Put(ctx, []Chunk{fooChunk1})
	require.NoError(t, err)
	n := storage.numIndexWrites

	// Only one extra entry for the new chunk of same series.
	fooChunk2 := dummyChunkFor(model.Time(0).Add(30*time.Second), metric)
	err = fooChunk2.Encode()
	require.NoError(t, err)
	err = store.Put(ctx, []Chunk{fooChunk2})
	require.NoError(t, err)
	require.Equal(t, n+1, storage.numIndexWrites)
}

func BenchmarkIndexCaching(b *testing.B) {
	ctx := context.Background()
	storeMaker := stores[1]
	storeCfg := storeMaker.configFn()

	store, _ := newTestChunkStoreConfig(b, "v9", storeCfg)
	defer store.Stop()

	fooChunk1 := dummyChunkFor(model.Time(0).Add(15*time.Second), BenchmarkLabels)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		err := store.Put(ctx, []Chunk{fooChunk1})
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
			err:     "the query time range exceeds the limit (query length: 744h0m0s, limit: 720h0m0s)",
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
				_, _, err = store.GetChunkRefs(ctx, userID, tc.from, tc.through, matchers...)
				require.EqualError(t, err, tc.err)
			})
		}
	}
}

func benchmarkParseIndexEntries(i int64, regex string, b *testing.B) {
	b.ReportAllocs()
	b.StopTimer()
	ctx := context.Background()
	entries := generateIndexEntries(i)
	matcher, err := labels.NewMatcher(labels.MatchRegexp, "", regex)
	if err != nil {
		b.Fatal(err)
	}
	b.StartTimer()
	for n := 0; n < b.N; n++ {
		keys, err := parseIndexEntries(ctx, entries, matcher)
		if err != nil {
			b.Fatal(err)
		}
		if regex == ".*" && len(keys) != len(entries)/2 {
			b.Fatalf("expected keys:%d got:%d", len(entries)/2, len(keys))
		}
	}
}

func BenchmarkParseIndexEntries500(b *testing.B)   { benchmarkParseIndexEntries(500, ".*", b) }
func BenchmarkParseIndexEntries2500(b *testing.B)  { benchmarkParseIndexEntries(2500, ".*", b) }
func BenchmarkParseIndexEntries10000(b *testing.B) { benchmarkParseIndexEntries(10000, ".*", b) }
func BenchmarkParseIndexEntries50000(b *testing.B) { benchmarkParseIndexEntries(50000, ".*", b) }

func BenchmarkParseIndexEntriesRegexSet500(b *testing.B) {
	benchmarkParseIndexEntries(500, "labelvalue0|labelvalue1|labelvalue2|labelvalue3|labelvalue600", b)
}

func BenchmarkParseIndexEntriesRegexSet2500(b *testing.B) {
	benchmarkParseIndexEntries(2500, "labelvalue0|labelvalue1|labelvalue2|labelvalue3|labelvalue600", b)
}

func BenchmarkParseIndexEntriesRegexSet10000(b *testing.B) {
	benchmarkParseIndexEntries(10000, "labelvalue0|labelvalue1|labelvalue2|labelvalue3|labelvalue600", b)
}

func BenchmarkParseIndexEntriesRegexSet50000(b *testing.B) {
	benchmarkParseIndexEntries(50000, "labelvalue0|labelvalue1|labelvalue2|labelvalue3|labelvalue600", b)
}

func generateIndexEntries(n int64) []IndexEntry {
	res := make([]IndexEntry, 0, n)
	for i := n - 1; i >= 0; i-- {
		labelValue := fmt.Sprintf("labelvalue%d", i%(n/2))
		chunkID := fmt.Sprintf("chunkid%d", i%(n/2))
		rangeValue := []byte{}
		rangeValue = append(rangeValue, []byte("component1")...)
		rangeValue = append(rangeValue, 0)
		rangeValue = append(rangeValue, []byte(labelValue)...)
		rangeValue = append(rangeValue, 0)
		rangeValue = append(rangeValue, []byte(chunkID)...)
		rangeValue = append(rangeValue, 0)
		res = append(res, IndexEntry{
			RangeValue: rangeValue,
		})
	}
	return res
}

func TestDisableIndexDeduplication(t *testing.T) {
	for i, disableIndexDeduplication := range []bool{
		false, true,
	} {
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			ctx := context.Background()
			metric := labels.Labels{
				{Name: labels.MetricName, Value: "foo"},
				{Name: "bar", Value: "baz"},
			}
			storeMaker := stores[0]
			storeCfg := storeMaker.configFn()
			storeCfg.ChunkCacheConfig.Cache = cache.NewFifoCache("chunk-cache", cache.FifoCacheConfig{
				MaxSizeItems: 5,
			}, prometheus.NewRegistry(), log.NewNopLogger())
			storeCfg.DisableIndexDeduplication = disableIndexDeduplication

			store, _ := newTestChunkStoreConfig(t, "v9", storeCfg)
			defer store.Stop()

			storage := store.(CompositeStore).stores[0].Store.(*seriesStore).fetcher.storage.(*MockStorage)

			fooChunk1 := dummyChunkFor(model.Time(0).Add(15*time.Second), metric)
			err := fooChunk1.Encode()
			require.NoError(t, err)
			err = store.Put(ctx, []Chunk{fooChunk1})
			require.NoError(t, err)
			n := storage.numIndexWrites

			// see if we have written the chunk to the store
			require.Equal(t, 1, storage.numChunkWrites)

			// Put the same chunk again
			err = store.Put(ctx, []Chunk{fooChunk1})
			require.NoError(t, err)

			expectedTotalWrites := n
			if disableIndexDeduplication {
				expectedTotalWrites *= 2
			}
			require.Equal(t, expectedTotalWrites, storage.numIndexWrites)

			// see if we deduped the chunk and the number of chunks we wrote is still 1
			require.Equal(t, 1, storage.numChunkWrites)
		})
	}
}
