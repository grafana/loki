package chunk

import (
	"context"
	"fmt"
	"math/rand"
	"reflect"
	"testing"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/test"

	"github.com/cortexproject/cortex/pkg/util/flagext"
	"github.com/cortexproject/cortex/pkg/util/validation"

	"github.com/grafana/loki/pkg/storage/chunk/cache"
	"github.com/grafana/loki/pkg/storage/chunk/encoding"
)

type configFactory func() StoreConfig

var seriesStoreSchemas = []string{"v9", "v10", "v11"}

var schemas = append([]string{"v1", "v2", "v3", "v4", "v5", "v6"}, seriesStoreSchemas...)

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
func newTestChunkStore(t require.TestingT, schemaName string) Store {
	var storeCfg StoreConfig
	flagext.DefaultValues(&storeCfg)
	return newTestChunkStoreConfig(t, schemaName, storeCfg)
}

func newTestChunkStoreConfig(t require.TestingT, schemaName string, storeCfg StoreConfig) Store {
	schemaCfg := DefaultSchemaConfig("", schemaName, 0)

	schema, err := schemaCfg.Configs[0].CreateSchema()
	require.NoError(t, err)

	return newTestChunkStoreConfigWithMockStorage(t, schemaCfg, schema, storeCfg)
}

func newTestChunkStoreConfigWithMockStorage(t require.TestingT, schemaCfg SchemaConfig, schema BaseSchema, storeCfg StoreConfig) Store {
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
	err = store.addSchema(storeCfg, schema, schemaCfg.Configs[0].From.Time, storage, storage, overrides, chunksCache, writeDedupeCache)
	require.NoError(t, err)
	return store
}

// TestChunkStore_Get tests results are returned correctly depending on the type of query
func TestChunkStore_Get(t *testing.T) {
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

	barChunk1 := dummyChunkFor(now, barMetric1)
	barChunk2 := dummyChunkFor(now, barMetric2)

	testCases := []struct {
		query  string
		expect []Chunk
		err    string
	}{
		{
			query:  `foo`,
			expect: []Chunk{fooChunk1, fooChunk2},
		},
		{
			query:  `foo{flip=""}`,
			expect: []Chunk{fooChunk2},
		},
		{
			query:  `foo{bar="baz"}`,
			expect: []Chunk{fooChunk1},
		},
		{
			query:  `foo{bar="beep"}`,
			expect: []Chunk{fooChunk2},
		},
		{
			query:  `foo{toms="code"}`,
			expect: []Chunk{fooChunk1, fooChunk2},
		},
		{
			query:  `foo{bar!="baz"}`,
			expect: []Chunk{fooChunk2},
		},
		{
			query:  `foo{bar=~"beep|baz"}`,
			expect: []Chunk{fooChunk1, fooChunk2},
		},
		{
			query:  `foo{toms="code", bar=~"beep|baz"}`,
			expect: []Chunk{fooChunk1, fooChunk2},
		},
		{
			query:  `foo{toms="code", bar="baz"}`,
			expect: []Chunk{fooChunk1},
		},
		{
			query:  `foo{a="b", bar="baz"}`,
			expect: nil,
		},
		{
			query: `{__name__=~"foo"}`,
			err:   "query must contain metric name",
		},
	}
	for _, schema := range schemas {
		for _, storeCase := range stores {
			storeCfg := storeCase.configFn()
			store := newTestChunkStoreConfig(t, schema, storeCfg)
			defer store.Stop()

			if err := store.Put(ctx, []Chunk{
				fooChunk1,
				fooChunk2,
				barChunk1,
				barChunk2,
			}); err != nil {
				t.Fatal(err)
			}

			for _, tc := range testCases {
				t.Run(fmt.Sprintf("%s / %s / %s", tc.query, schema, storeCase.name), func(t *testing.T) {
					t.Log("========= Running query", tc.query, "with schema", schema)
					matchers, err := parser.ParseMetricSelector(tc.query)
					if err != nil {
						t.Fatal(err)
					}

					// Query with ordinary time-range
					chunks1, err := store.Get(ctx, userID, now.Add(-time.Hour), now, matchers...)
					if tc.err != "" {
						require.Error(t, err)
						require.Equal(t, tc.err, err.Error())
						return
					}
					require.NoError(t, err)
					if !reflect.DeepEqual(tc.expect, chunks1) {
						t.Fatalf("%s: wrong chunks - %s", tc.query, test.Diff(tc.expect, chunks1))
					}

					// Pushing end of time-range into future should yield exact same resultset
					chunks2, err := store.Get(ctx, userID, now.Add(-time.Hour), now.Add(time.Hour*24*10), matchers...)
					require.NoError(t, err)
					if !reflect.DeepEqual(tc.expect, chunks2) {
						t.Fatalf("%s: wrong chunks - %s", tc.query, test.Diff(tc.expect, chunks2))
					}

					// Query with both begin & end of time-range in future should yield empty resultset
					chunks3, err := store.Get(ctx, userID, now.Add(time.Hour), now.Add(time.Hour*2), matchers...)
					require.NoError(t, err)
					if len(chunks3) != 0 {
						t.Fatalf("%s: future query should yield empty resultset ... actually got %v chunks: %#v",
							tc.query, len(chunks3), chunks3)
					}
				})
			}
		}
	}
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
					store := newTestChunkStoreConfig(t, schema, storeCfg)
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
					store := newTestChunkStoreConfig(t, schema, storeCfg)
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
			store := newTestChunkStoreConfig(t, schema, storeCfg)
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

					chunks, err := store.Get(ctx, userID, now.Add(-time.Hour), now, matchers...)
					require.NoError(t, err)

					if !reflect.DeepEqual(tc.expect, chunks) {
						t.Fatalf("%s: wrong chunks - %s", tc.query, test.Diff(tc.expect, chunks))
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
			store := newTestChunkStore(t, schema)
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
				chunks, err := store.Get(ctx, userID, startTime, endTime, matchers...)
				require.NoError(t, err)

				// We need to check that each chunk is in the time range
				for _, chunk := range chunks {
					assert.False(t, chunk.From.After(endTime))
					assert.False(t, chunk.Through.Before(startTime))
					samples, err := chunk.Samples(chunk.From, chunk.Through)
					assert.NoError(t, err)
					assert.Equal(t, 1, len(samples))
					// TODO verify chunk contents
				}

				// And check we got all the chunks we want
				numChunks := (end / chunkLen) - (start / chunkLen) + 1
				assert.Equal(t, int(numChunks), len(chunks))
			}
		})
	}
}

func TestChunkStoreLeastRead(t *testing.T) {
	// Test we don't read too much from the index
	ctx := context.Background()
	store := newTestChunkStore(t, "v6")
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

		chunks, err := store.Get(ctx, userID, startTime, endTime, matchers...)
		require.NoError(t, err)

		// We need to check that each chunk is in the time range
		for _, chunk := range chunks {
			assert.False(t, chunk.From.After(endTime))
			assert.False(t, chunk.Through.Before(startTime))
			samples, err := chunk.Samples(chunk.From, chunk.Through)
			assert.NoError(t, err)
			assert.Equal(t, 1, len(samples))
		}

		// And check we got all the chunks we want
		numChunks := 24 - (start / chunkLen) + 1
		assert.Equal(t, int(numChunks), len(chunks))
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

	store := newTestChunkStoreConfig(t, "v9", storeCfg)
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

	store := newTestChunkStoreConfig(b, "v9", storeCfg)
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
				store := newTestChunkStore(t, schema)
				defer store.Stop()

				matchers, err := parser.ParseMetricSelector(tc.query)
				require.NoError(t, err)

				// Query with ordinary time-range
				_, err = store.Get(ctx, userID, tc.from, tc.through, matchers...)
				require.EqualError(t, err, tc.err)
			})
		}
	}
}

func benchmarkParseIndexEntries(i int64, regex string, b *testing.B) {
	b.ReportAllocs()
	b.StopTimer()
	store := &store{}
	ctx := context.Background()
	entries := generateIndexEntries(i)
	matcher, err := labels.NewMatcher(labels.MatchRegexp, "", regex)
	if err != nil {
		b.Fatal(err)
	}
	b.StartTimer()
	for n := 0; n < b.N; n++ {
		keys, err := store.parseIndexEntries(ctx, entries, matcher)
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

func getNonDeletedIntervals(originalInterval, deletedInterval model.Interval) []model.Interval {
	if !intervalsOverlap(originalInterval, deletedInterval) {
		return []model.Interval{originalInterval}
	}

	nonDeletedIntervals := []model.Interval{}
	if deletedInterval.Start > originalInterval.Start {
		nonDeletedIntervals = append(nonDeletedIntervals, model.Interval{Start: originalInterval.Start, End: deletedInterval.Start - 1})
	}

	if deletedInterval.End < originalInterval.End {
		nonDeletedIntervals = append(nonDeletedIntervals, model.Interval{Start: deletedInterval.End + 1, End: originalInterval.End})
	}

	return nonDeletedIntervals
}

func TestStore_DeleteChunk(t *testing.T) {
	ctx := context.Background()

	metric1 := labels.Labels{
		{Name: labels.MetricName, Value: "foo"},
		{Name: "bar", Value: "baz"},
	}

	metric2 := labels.Labels{
		{Name: labels.MetricName, Value: "foo"},
		{Name: "bar", Value: "baz2"},
	}

	metric3 := labels.Labels{
		{Name: labels.MetricName, Value: "foo"},
		{Name: "bar", Value: "baz3"},
	}

	fooChunk1 := dummyChunkForEncoding(model.Now(), metric1, encoding.Varbit, 200)
	err := fooChunk1.Encode()
	require.NoError(t, err)

	fooChunk2 := dummyChunkForEncoding(model.Now(), metric2, encoding.Varbit, 200)
	err = fooChunk2.Encode()
	require.NoError(t, err)

	nonExistentChunk := dummyChunkForEncoding(model.Now(), metric3, encoding.Varbit, 200)

	fooMetricNameMatcher, err := parser.ParseMetricSelector(`foo`)
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name                           string
		chunks                         []Chunk
		chunkToDelete                  Chunk
		partialDeleteInterval          *model.Interval
		err                            error
		numChunksToExpectAfterDeletion int
	}{
		{
			name:                           "delete whole chunk",
			chunkToDelete:                  fooChunk1,
			numChunksToExpectAfterDeletion: 1,
		},
		{
			name:                           "delete chunk partially at start",
			chunkToDelete:                  fooChunk1,
			partialDeleteInterval:          &model.Interval{Start: fooChunk1.From, End: fooChunk1.From.Add(30 * time.Minute)},
			numChunksToExpectAfterDeletion: 2,
		},
		{
			name:                           "delete chunk partially at end",
			chunkToDelete:                  fooChunk1,
			partialDeleteInterval:          &model.Interval{Start: fooChunk1.Through.Add(-30 * time.Minute), End: fooChunk1.Through},
			numChunksToExpectAfterDeletion: 2,
		},
		{
			name:                           "delete chunk partially in the middle",
			chunkToDelete:                  fooChunk1,
			partialDeleteInterval:          &model.Interval{Start: fooChunk1.From.Add(15 * time.Minute), End: fooChunk1.Through.Add(-15 * time.Minute)},
			numChunksToExpectAfterDeletion: 3,
		},
		{
			name:                           "delete non-existent chunk",
			chunkToDelete:                  nonExistentChunk,
			numChunksToExpectAfterDeletion: 2,
		},
		{
			name:                           "delete first second",
			chunkToDelete:                  fooChunk1,
			partialDeleteInterval:          &model.Interval{Start: fooChunk1.From, End: fooChunk1.From},
			numChunksToExpectAfterDeletion: 2,
		},
		{
			name:                           "delete chunk out of range",
			chunkToDelete:                  fooChunk1,
			partialDeleteInterval:          &model.Interval{Start: fooChunk1.Through.Add(time.Minute), End: fooChunk1.Through.Add(10 * time.Minute)},
			numChunksToExpectAfterDeletion: 2,
			err:                            errors.Wrapf(ErrParialDeleteChunkNoOverlap, "chunkID=%s", fooChunk1.ExternalKey()),
		},
	} {
		for _, schema := range schemas {
			t.Run(fmt.Sprintf("%s / %s", schema, tc.name), func(t *testing.T) {
				store := newTestChunkStore(t, schema)
				defer store.Stop()

				// inserting 2 chunks with different labels but same metric name
				err = store.Put(ctx, []Chunk{fooChunk1, fooChunk2})
				require.NoError(t, err)

				// we expect to get 2 chunks back using just metric name matcher
				chunks, err := store.Get(ctx, userID, model.Now().Add(-time.Hour), model.Now(), fooMetricNameMatcher...)
				require.NoError(t, err)
				require.Equal(t, 2, len(chunks))

				err = store.DeleteChunk(ctx, tc.chunkToDelete.From, tc.chunkToDelete.Through, userID,
					tc.chunkToDelete.ExternalKey(), tc.chunkToDelete.Metric, tc.partialDeleteInterval)

				if tc.err != nil {
					require.Error(t, err)
					require.Equal(t, tc.err.Error(), err.Error())

					// we expect to get same results back if delete operation is expected to fail
					chunks, err := store.Get(ctx, userID, model.Now().Add(-time.Hour), model.Now(), fooMetricNameMatcher...)
					require.NoError(t, err)

					require.Equal(t, 2, len(chunks))

					return
				}
				require.NoError(t, err)

				matchersForDeletedChunk, err := parser.ParseMetricSelector(tc.chunkToDelete.Metric.String())
				require.NoError(t, err)

				var nonDeletedIntervals []model.Interval

				if tc.partialDeleteInterval != nil {
					nonDeletedIntervals = getNonDeletedIntervals(model.Interval{
						Start: tc.chunkToDelete.From,
						End:   tc.chunkToDelete.Through,
					}, *tc.partialDeleteInterval)
				}

				// we expect to get 1 non deleted chunk + new chunks that were created (if any) after partial deletion
				chunks, err = store.Get(ctx, userID, model.Now().Add(-time.Hour), model.Now(), fooMetricNameMatcher...)
				require.NoError(t, err)
				require.Equal(t, tc.numChunksToExpectAfterDeletion, len(chunks))

				chunks, err = store.Get(ctx, userID, model.Now().Add(-time.Hour), model.Now(), matchersForDeletedChunk...)
				require.NoError(t, err)
				require.Equal(t, len(nonDeletedIntervals), len(chunks))

				// comparing intervals of new chunks that were created after partial deletion
				for i, nonDeletedInterval := range nonDeletedIntervals {
					require.Equal(t, chunks[i].From, nonDeletedInterval.Start)
					require.Equal(t, chunks[i].Through, nonDeletedInterval.End)
				}
			})
		}
	}
}

func TestStore_DeleteSeriesIDs(t *testing.T) {
	ctx := context.Background()
	metric1 := labels.Labels{
		{Name: labels.MetricName, Value: "foo"},
		{Name: "bar", Value: "baz"},
	}

	metric2 := labels.Labels{
		{Name: labels.MetricName, Value: "foo"},
		{Name: "bar", Value: "baz2"},
	}

	matchers, err := parser.ParseMetricSelector(`foo`)
	if err != nil {
		t.Fatal(err)
	}

	for _, schema := range seriesStoreSchemas {
		t.Run(schema, func(t *testing.T) {
			store := newTestChunkStore(t, schema)
			defer store.Stop()

			seriesStore := store.(CompositeStore).stores[0].Store.(*seriesStore)

			fooChunk1 := dummyChunkForEncoding(model.Now(), metric1, encoding.Varbit, 200)
			err := fooChunk1.Encode()
			require.NoError(t, err)

			fooChunk2 := dummyChunkForEncoding(model.Now(), metric2, encoding.Varbit, 200)
			err = fooChunk2.Encode()
			require.NoError(t, err)

			err = store.Put(ctx, []Chunk{fooChunk1, fooChunk2})
			require.NoError(t, err)

			// we expect to have 2 series IDs in index for the chunks that were added above
			seriesIDs, err := seriesStore.lookupSeriesByMetricNameMatcher(ctx, model.Now().Add(-time.Hour), model.Now(),
				userID, "foo", nil, nil)
			require.NoError(t, err)
			require.Equal(t, 2, len(seriesIDs))

			// we expect to have 2 chunks in store that were added above
			chunks, err := store.Get(ctx, userID, model.Now().Add(-time.Hour), model.Now(), matchers...)
			require.NoError(t, err)
			require.Equal(t, 2, len(chunks))

			// lets try deleting series ID without deleting the chunk
			err = store.DeleteSeriesIDs(ctx, fooChunk1.From, fooChunk1.Through, userID, fooChunk1.Metric)
			require.NoError(t, err)

			// series IDs should still be there since chunks for them still exist
			seriesIDs, err = seriesStore.lookupSeriesByMetricNameMatcher(ctx, model.Now().Add(-time.Hour), model.Now(),
				userID, "foo", nil, nil)
			require.NoError(t, err)
			require.Equal(t, 2, len(seriesIDs))

			// lets delete a chunk and then delete its series ID
			err = store.DeleteChunk(ctx, fooChunk1.From, fooChunk1.Through, userID, fooChunk1.ExternalKey(), metric1, nil)
			require.NoError(t, err)

			err = store.DeleteSeriesIDs(ctx, fooChunk1.From, fooChunk1.Through, userID, fooChunk1.Metric)
			require.NoError(t, err)

			// there should be only be 1 chunk and 1 series ID left for it
			chunks, err = store.Get(ctx, userID, model.Now().Add(-time.Hour), model.Now(), matchers...)
			require.NoError(t, err)
			require.Equal(t, 1, len(chunks))

			seriesIDs, err = seriesStore.lookupSeriesByMetricNameMatcher(ctx, model.Now().Add(-time.Hour), model.Now(),
				userID, "foo", nil, nil)
			require.NoError(t, err)
			require.Equal(t, 1, len(seriesIDs))
			require.Equal(t, string(labelsSeriesID(fooChunk2.Metric)), seriesIDs[0])

			// lets delete the other chunk partially and try deleting the series ID
			err = store.DeleteChunk(ctx, fooChunk2.From, fooChunk2.Through, userID, fooChunk2.ExternalKey(), metric2,
				&model.Interval{Start: fooChunk2.From, End: fooChunk2.From.Add(30 * time.Minute)})
			require.NoError(t, err)

			err = store.DeleteSeriesIDs(ctx, fooChunk1.From, fooChunk1.Through, userID, fooChunk1.Metric)
			require.NoError(t, err)

			// partial deletion should have left another chunk and a series ID in store
			chunks, err = store.Get(ctx, userID, model.Now().Add(-time.Hour), model.Now(), matchers...)
			require.NoError(t, err)
			require.Equal(t, 1, len(chunks))

			seriesIDs, err = seriesStore.lookupSeriesByMetricNameMatcher(ctx, model.Now().Add(-time.Hour), model.Now(),
				userID, "foo", nil, nil)
			require.NoError(t, err)
			require.Equal(t, 1, len(seriesIDs))
			require.Equal(t, string(labelsSeriesID(fooChunk2.Metric)), seriesIDs[0])
		})
	}
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

			store := newTestChunkStoreConfig(t, "v9", storeCfg)
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
