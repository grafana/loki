package chunk

import (
	"fmt"
	"math/rand"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/context"

	"github.com/cortexproject/cortex/pkg/chunk/cache"
	"github.com/cortexproject/cortex/pkg/chunk/encoding"
	"github.com/cortexproject/cortex/pkg/util/extract"
	"github.com/cortexproject/cortex/pkg/util/flagext"
	"github.com/cortexproject/cortex/pkg/util/validation"
	"github.com/weaveworks/common/test"
	"github.com/weaveworks/common/user"
)

type configFactory func() StoreConfig

var schemas = []struct {
	name              string
	requireMetricName bool
}{
	{"v1", true},
	{"v2", true},
	{"v3", true},
	{"v4", true},
	{"v5", true},
	{"v6", true},
	{"v9", true},
	{"v10", true},
}

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
				Size: 500,
			})
			return storeCfg
		},
	},
}

// newTestStore creates a new Store for testing.
func newTestChunkStore(t *testing.T, schemaName string) Store {
	var storeCfg StoreConfig
	flagext.DefaultValues(&storeCfg)
	return newTestChunkStoreConfig(t, schemaName, storeCfg)
}

func newTestChunkStoreConfig(t *testing.T, schemaName string, storeCfg StoreConfig) Store {
	var (
		tbmConfig TableManagerConfig
		schemaCfg = DefaultSchemaConfig("", schemaName, 0)
	)
	flagext.DefaultValues(&tbmConfig)
	storage := NewMockStorage()
	tableManager, err := NewTableManager(tbmConfig, schemaCfg, maxChunkAge, storage)
	require.NoError(t, err)

	err = tableManager.SyncTables(context.Background())
	require.NoError(t, err)

	var limits validation.Limits
	flagext.DefaultValues(&limits)
	limits.MaxQueryLength = 30 * 24 * time.Hour
	overrides, err := validation.NewOverrides(limits)
	require.NoError(t, err)

	store := NewCompositeStore()
	err = store.AddPeriod(storeCfg, schemaCfg.Configs[0], storage, storage, overrides)
	require.NoError(t, err)
	return store
}

func createSampleStreamFrom(chunk Chunk) (*model.SampleStream, error) {
	samples, err := chunk.Samples(chunk.From, chunk.Through)
	if err != nil {
		return nil, err
	}
	return &model.SampleStream{
		Metric: chunk.Metric,
		Values: samples,
	}, nil
}

// Allow sorting of local.SeriesIterator by fingerprint (for comparisation tests)
type ByFingerprint model.Matrix

func (bfp ByFingerprint) Len() int {
	return len(bfp)
}
func (bfp ByFingerprint) Swap(i, j int) {
	bfp[i], bfp[j] = bfp[j], bfp[i]
}
func (bfp ByFingerprint) Less(i, j int) bool {
	return bfp[i].Metric.Fingerprint() < bfp[j].Metric.Fingerprint()
}

// TestChunkStore_Get tests results are returned correctly depending on the type of query
func TestChunkStore_Get(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), userID)
	now := model.Now()

	fooMetric1 := model.Metric{
		model.MetricNameLabel: "foo",
		"bar":                 "baz",
		"toms":                "code",
		"flip":                "flop",
	}
	fooMetric2 := model.Metric{
		model.MetricNameLabel: "foo",
		"bar":                 "beep",
		"toms":                "code",
	}

	// barMetric1 is a subset of barMetric2 to test over-matching bug.
	barMetric1 := model.Metric{
		model.MetricNameLabel: "bar",
		"bar":                 "baz",
	}
	barMetric2 := model.Metric{
		model.MetricNameLabel: "bar",
		"bar":                 "baz",
		"toms":                "code",
	}

	fooChunk1 := dummyChunkFor(now, fooMetric1)
	fooChunk2 := dummyChunkFor(now, fooMetric2)

	barChunk1 := dummyChunkFor(now, barMetric1)
	barChunk2 := dummyChunkFor(now, barMetric2)

	fooSampleStream1, err := createSampleStreamFrom(fooChunk1)
	require.NoError(t, err)
	fooSampleStream2, err := createSampleStreamFrom(fooChunk2)
	require.NoError(t, err)

	barSampleStream1, err := createSampleStreamFrom(barChunk1)
	require.NoError(t, err)
	barSampleStream2, err := createSampleStreamFrom(barChunk2)
	require.NoError(t, err)

	testCases := []struct {
		query  string
		expect model.Matrix
	}{
		{
			`foo`,
			model.Matrix{fooSampleStream1, fooSampleStream2},
		},
		{
			`foo{flip=""}`,
			model.Matrix{fooSampleStream2},
		},
		{
			`foo{bar="baz"}`,
			model.Matrix{fooSampleStream1},
		},
		{
			`foo{bar="beep"}`,
			model.Matrix{fooSampleStream2},
		},
		{
			`foo{toms="code"}`,
			model.Matrix{fooSampleStream1, fooSampleStream2},
		},
		{
			`foo{bar!="baz"}`,
			model.Matrix{fooSampleStream2},
		},
		{
			`foo{bar=~"beep|baz"}`,
			model.Matrix{fooSampleStream1, fooSampleStream2},
		},
		{
			`foo{toms="code", bar=~"beep|baz"}`,
			model.Matrix{fooSampleStream1, fooSampleStream2},
		},
		{
			`foo{toms="code", bar="baz"}`,
			model.Matrix{fooSampleStream1},
		},
		{
			`{__name__=~"foo"}`,
			model.Matrix{fooSampleStream1, fooSampleStream2},
		},
		{
			`{__name__=~"foobar"}`,
			model.Matrix{},
		},
		{
			`{__name__=~"fo.*"}`,
			model.Matrix{fooSampleStream1, fooSampleStream2},
		},
		{
			`{__name__=~"foo", toms="code"}`,
			model.Matrix{fooSampleStream1, fooSampleStream2},
		},
		{
			`{__name__!="foo", toms="code"}`,
			model.Matrix{barSampleStream2},
		},
		{
			`{__name__!="bar", toms="code"}`,
			model.Matrix{fooSampleStream1, fooSampleStream2},
		},
		{
			`{__name__=~"bar", bar="baz"}`,
			model.Matrix{barSampleStream1, barSampleStream2},
		},
		{
			`{__name__=~"bar", bar="baz",toms!="code"}`,
			model.Matrix{barSampleStream1},
		},
	}
	for _, schema := range schemas {
		for _, storeCase := range stores {
			storeCfg := storeCase.configFn()
			store := newTestChunkStoreConfig(t, schema.name, storeCfg)
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
				t.Run(fmt.Sprintf("%s / %s / %s", tc.query, schema.name, storeCase.name), func(t *testing.T) {
					t.Log("========= Running query", tc.query, "with schema", schema.name)
					matchers, err := promql.ParseMetricSelector(tc.query)
					if err != nil {
						t.Fatal(err)
					}

					metricNameMatcher, _, ok := extract.MetricNameMatcherFromMatchers(matchers)
					if schema.requireMetricName && (!ok || metricNameMatcher.Type != labels.MatchEqual) {
						return
					}

					// Query with ordinary time-range
					chunks1, err := store.Get(ctx, now.Add(-time.Hour), now, matchers...)
					require.NoError(t, err)

					matrix1, err := ChunksToMatrix(ctx, chunks1, now.Add(-time.Hour), now)
					require.NoError(t, err)

					sort.Sort(ByFingerprint(matrix1))
					if !reflect.DeepEqual(tc.expect, matrix1) {
						t.Fatalf("%s: wrong chunks - %s", tc.query, test.Diff(tc.expect, matrix1))
					}

					// Pushing end of time-range into future should yield exact same resultset
					chunks2, err := store.Get(ctx, now.Add(-time.Hour), now.Add(time.Hour*24*10), matchers...)
					require.NoError(t, err)

					matrix2, err := ChunksToMatrix(ctx, chunks2, now.Add(-time.Hour), now)
					require.NoError(t, err)

					sort.Sort(ByFingerprint(matrix2))
					if !reflect.DeepEqual(tc.expect, matrix2) {
						t.Fatalf("%s: wrong chunks - %s", tc.query, test.Diff(tc.expect, matrix2))
					}

					// Query with both begin & end of time-range in future should yield empty resultset
					matrix3, err := store.Get(ctx, now.Add(time.Hour), now.Add(time.Hour*2), matchers...)
					require.NoError(t, err)
					if len(matrix3) != 0 {
						t.Fatalf("%s: future query should yield empty resultset ... actually got %v chunks: %#v",
							tc.query, len(matrix3), matrix3)
					}
				})
			}
		}
	}
}

// TestChunkStore_getMetricNameChunks tests if chunks are fetched correctly when we have the metric name
func TestChunkStore_getMetricNameChunks(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), userID)
	now := model.Now()
	chunk1 := dummyChunkFor(now, model.Metric{
		model.MetricNameLabel: "foo",
		"bar":                 "baz",
		"toms":                "code",
		"flip":                "flop",
	})
	chunk2 := dummyChunkFor(now, model.Metric{
		model.MetricNameLabel: "foo",
		"bar":                 "beep",
		"toms":                "code",
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
			store := newTestChunkStoreConfig(t, schema.name, storeCfg)
			defer store.Stop()

			if err := store.Put(ctx, []Chunk{chunk1, chunk2}); err != nil {
				t.Fatal(err)
			}

			for _, tc := range testCases {
				t.Run(fmt.Sprintf("%s / %s / %s", tc.query, schema.name, storeCase.name), func(t *testing.T) {
					t.Log("========= Running query", tc.query, "with schema", schema.name)
					matchers, err := promql.ParseMetricSelector(tc.query)
					if err != nil {
						t.Fatal(err)
					}

					chunks, err := store.Get(ctx, now.Add(-time.Hour), now, matchers...)
					require.NoError(t, err)

					if !reflect.DeepEqual(tc.expect, chunks) {
						t.Fatalf("%s: wrong chunks - %s", tc.query, test.Diff(tc.expect, chunks))
					}
				})
			}
		}
	}
}

func mustNewLabelMatcher(matchType labels.MatchType, name string, value string) *labels.Matcher {
	matcher, err := labels.NewMatcher(matchType, name, value)
	if err != nil {
		panic(err)
	}
	return matcher
}

func TestChunkStoreRandom(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), userID)

	for _, schema := range schemas {
		t.Run(schema.name, func(t *testing.T) {
			store := newTestChunkStore(t, schema.name)
			defer store.Stop()

			// put 100 chunks from 0 to 99
			const chunkLen = 2 * 3600 // in seconds
			for i := 0; i < 100; i++ {
				ts := model.TimeFromUnix(int64(i * chunkLen))
				chunks, _ := encoding.New().Add(model.SamplePair{
					Timestamp: ts,
					Value:     model.SampleValue(float64(i)),
				})
				chunk := NewChunk(
					userID,
					model.Fingerprint(1),
					model.Metric{
						model.MetricNameLabel: "foo",
						"bar":                 "baz",
					},
					chunks[0],
					ts,
					ts.Add(chunkLen*time.Second),
				)
				err := chunk.Encode()
				require.NoError(t, err)
				err = store.Put(ctx, []Chunk{chunk})
				require.NoError(t, err)
			}

			// pick two random numbers and do a query
			for i := 0; i < 100; i++ {
				start := rand.Int63n(100 * chunkLen)
				end := start + rand.Int63n((100*chunkLen)-start)
				assert.True(t, start < end)

				startTime := model.TimeFromUnix(start)
				endTime := model.TimeFromUnix(end)

				matchers := []*labels.Matcher{
					mustNewLabelMatcher(labels.MatchEqual, model.MetricNameLabel, "foo"),
					mustNewLabelMatcher(labels.MatchEqual, "bar", "baz"),
				}
				chunks, err := store.Get(ctx, startTime, endTime, matchers...)
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
	ctx := user.InjectOrgID(context.Background(), userID)
	store := newTestChunkStore(t, "v6")
	defer store.Stop()

	// Put 24 chunks 1hr chunks in the store
	const chunkLen = 60 // in seconds
	for i := 0; i < 24; i++ {
		ts := model.TimeFromUnix(int64(i * chunkLen))
		chunks, _ := encoding.New().Add(model.SamplePair{
			Timestamp: ts,
			Value:     model.SampleValue(float64(i)),
		})
		chunk := NewChunk(
			userID,
			model.Fingerprint(1),
			model.Metric{
				model.MetricNameLabel: "foo",
				"bar":                 "baz",
			},
			chunks[0],
			ts,
			ts.Add(chunkLen*time.Second),
		)
		t.Logf("Loop %d", i)
		err := chunk.Encode()
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
			mustNewLabelMatcher(labels.MatchEqual, model.MetricNameLabel, "foo"),
			mustNewLabelMatcher(labels.MatchEqual, "bar", "baz"),
		}

		chunks, err := store.Get(ctx, startTime, endTime, matchers...)
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
	ctx := user.InjectOrgID(context.Background(), userID)
	metric := model.Metric{
		model.MetricNameLabel: "foo",
		"bar":                 "baz",
	}
	storeMaker := stores[1]
	storeCfg := storeMaker.configFn()

	store := newTestChunkStoreConfig(t, "v9", storeCfg)
	defer store.Stop()

	storage := store.(CompositeStore).stores[0].Store.(*seriesStore).storage.(*MockStorage)

	fooChunk1 := dummyChunkFor(model.Time(0).Add(15*time.Second), metric)
	err := fooChunk1.Encode()
	require.NoError(t, err)
	err = store.Put(ctx, []Chunk{fooChunk1})
	require.NoError(t, err)
	n := storage.numWrites

	// Only one extra entry for the new chunk of same series.
	fooChunk2 := dummyChunkFor(model.Time(0).Add(30*time.Second), metric)
	err = fooChunk2.Encode()
	require.NoError(t, err)
	err = store.Put(ctx, []Chunk{fooChunk2})
	require.NoError(t, err)
	require.Equal(t, n+1, storage.numWrites)
}

func TestChunkStoreError(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), userID)
	for _, tc := range []struct {
		query         string
		from, through model.Time
		err           string
	}{
		{
			query:   "foo",
			from:    model.Time(0).Add(31 * 24 * time.Hour),
			through: model.Time(0),
			err:     "rpc error: code = Code(400) desc = invalid query, through < from (0 < 2678400)",
		},
		{
			query:   "foo",
			from:    model.Time(0),
			through: model.Time(0).Add(31 * 24 * time.Hour),
			err:     "rpc error: code = Code(400) desc = invalid query, length > limit (744h0m0s > 720h0m0s)",
		},
		{
			query:   "{foo=\"bar\"}",
			from:    model.Time(0),
			through: model.Time(0).Add(1 * time.Hour),
			err:     "rpc error: code = Code(400) desc = query must contain metric name",
		},
		{
			query:   "{__name__=~\"bar\"}",
			from:    model.Time(0),
			through: model.Time(0).Add(1 * time.Hour),
			err:     "rpc error: code = Code(400) desc = query must contain metric name",
		},
	} {
		for _, schema := range schemas {
			t.Run(fmt.Sprintf("%s / %s", tc.query, schema.name), func(t *testing.T) {
				store := newTestChunkStore(t, schema.name)
				defer store.Stop()

				matchers, err := promql.ParseMetricSelector(tc.query)
				require.NoError(t, err)

				// Query with ordinary time-range
				_, err = store.Get(ctx, tc.from, tc.through, matchers...)
				require.EqualError(t, err, tc.err)
			})
		}
	}
}
