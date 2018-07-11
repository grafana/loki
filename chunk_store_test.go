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

	"github.com/weaveworks/common/test"
	"github.com/weaveworks/common/user"
	"github.com/weaveworks/cortex/pkg/prom1/storage/local/chunk"
	"github.com/weaveworks/cortex/pkg/util"
	"github.com/weaveworks/cortex/pkg/util/extract"
)

type schemaFactory func(cfg SchemaConfig) Schema
type storeFactory func(StoreConfig, Schema, StorageClient) (Store, error)

var schemas = []struct {
	name              string
	schemaFn          schemaFactory
	storeFn           storeFactory
	requireMetricName bool
}{
	{"v1 schema", v1Schema, newStore, true},
	{"v2 schema", v2Schema, newStore, true},
	{"v3 schema", v3Schema, newStore, true},
	{"v4 schema", v4Schema, newStore, true},
	{"v5 schema", v5Schema, newStore, true},
	{"v6 schema", v6Schema, newStore, true},
	{"v7 schema", v7Schema, newStore, true},
	{"v8 schema", v8Schema, newStore, false},
	{"v9 schema", v9Schema, newSeriesStore, true},
}

// newTestStore creates a new Store for testing.
func newTestChunkStore(t *testing.T, schemaFactory schemaFactory, storeFactory storeFactory) Store {
	var (
		storeCfg  StoreConfig
		schemaCfg SchemaConfig
	)
	util.DefaultValues(&storeCfg, &schemaCfg)

	storage := NewMockStorage()
	tableManager, err := NewTableManager(schemaCfg, maxChunkAge, storage)
	require.NoError(t, err)

	err = tableManager.SyncTables(context.Background())
	require.NoError(t, err)

	store, err := storeFactory(storeCfg, schemaFactory(schemaCfg), storage)
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
		"bar":  "baz",
		"toms": "code",
		"flip": "flop",
	}
	fooMetric2 := model.Metric{
		model.MetricNameLabel: "foo",
		"bar":  "beep",
		"toms": "code",
	}

	// barMetric1 is a subset of barMetric2 to test over-matching bug.
	barMetric1 := model.Metric{
		model.MetricNameLabel: "bar",
		"bar": "baz",
	}
	barMetric2 := model.Metric{
		model.MetricNameLabel: "bar",
		"bar":  "baz",
		"toms": "code",
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

	for _, tc := range []struct {
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
	} {
		for _, schema := range schemas {
			t.Run(fmt.Sprintf("%s / %s", tc.query, schema.name), func(t *testing.T) {
				t.Log("========= Running query", tc.query, "with schema", schema.name)
				store := newTestChunkStore(t, schema.schemaFn, schema.storeFn)

				if err := store.Put(ctx, []Chunk{
					fooChunk1,
					fooChunk2,
					barChunk1,
					barChunk2,
				}); err != nil {
					t.Fatal(err)
				}

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
				chunks2, err := store.Get(ctx, now.Add(-time.Hour), now.Add(time.Hour*24*30), matchers...)
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

// TestChunkStore_getMetricNameChunks tests if chunks are fetched correctly when we have the metric name
func TestChunkStore_getMetricNameChunks(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), userID)
	now := model.Now()
	chunk1 := dummyChunkFor(now, model.Metric{
		model.MetricNameLabel: "foo",
		"bar":  "baz",
		"toms": "code",
		"flip": "flop",
	})
	chunk2 := dummyChunkFor(now, model.Metric{
		model.MetricNameLabel: "foo",
		"bar":  "beep",
		"toms": "code",
	})

	for _, tc := range []struct {
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
	} {
		for _, schema := range schemas {
			t.Run(fmt.Sprintf("%s / %s", tc.query, schema.name), func(t *testing.T) {
				t.Log("========= Running query", tc.query, "with schema", schema.name)
				store := newTestChunkStore(t, schema.schemaFn, schema.storeFn)

				if err := store.Put(ctx, []Chunk{chunk1, chunk2}); err != nil {
					t.Fatal(err)
				}

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
			store := newTestChunkStore(t, schema.schemaFn, schema.storeFn)

			// put 100 chunks from 0 to 99
			const chunkLen = 13 * 3600 // in seconds
			for i := 0; i < 100; i++ {
				ts := model.TimeFromUnix(int64(i * chunkLen))
				chunks, _ := chunk.New().Add(model.SamplePair{
					Timestamp: ts,
					Value:     model.SampleValue(float64(i)),
				})
				chunk := NewChunk(
					userID,
					model.Fingerprint(1),
					model.Metric{
						model.MetricNameLabel: "foo",
						"bar": "baz",
					},
					chunks[0],
					ts,
					ts.Add(chunkLen*time.Second),
				)

				err := store.Put(ctx, []Chunk{chunk})
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
	store := newTestChunkStore(t, v6Schema, newStore)

	// Put 24 chunks 1hr chunks in the store
	const chunkLen = 60 // in seconds
	for i := 0; i < 24; i++ {
		ts := model.TimeFromUnix(int64(i * chunkLen))
		chunks, _ := chunk.New().Add(model.SamplePair{
			Timestamp: ts,
			Value:     model.SampleValue(float64(i)),
		})
		chunk := NewChunk(
			userID,
			model.Fingerprint(1),
			model.Metric{
				model.MetricNameLabel: "foo",
				"bar": "baz",
			},
			chunks[0],
			ts,
			ts.Add(chunkLen*time.Second),
		)
		t.Logf("Loop %d", i)
		err := store.Put(ctx, []Chunk{chunk})
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
		if err != nil {
			t.Fatal(t, err)
		}

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
