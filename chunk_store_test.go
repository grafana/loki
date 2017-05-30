package chunk

import (
	"fmt"
	"math/rand"
	"reflect"
	"testing"
	"time"

	"github.com/prometheus/common/log"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/storage/local/chunk"
	"github.com/prometheus/prometheus/storage/metric"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/context"

	"github.com/weaveworks/common/test"
	"github.com/weaveworks/common/user"
)

// newTestStore creates a new Store for testing.
func newTestChunkStore(t *testing.T, cfg StoreConfig) *Store {
	storage := NewMockStorage()
	tableManager, err := NewTableManager(TableManagerConfig{}, storage)
	require.NoError(t, err)
	err = tableManager.syncTables(context.Background())
	require.NoError(t, err)
	store, err := NewStore(cfg, storage)
	require.NoError(t, err)
	return store
}

func TestChunkStore(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), userID)
	now := model.Now()
	chunk1 := dummyChunkFor(model.Metric{
		model.MetricNameLabel: "foo",
		"bar":  "baz",
		"toms": "code",
		"flip": "flop",
	})
	chunk2 := dummyChunkFor(model.Metric{
		model.MetricNameLabel: "foo",
		"bar":  "beep",
		"toms": "code",
	})

	schemas := []struct {
		name string
		fn   func(cfg SchemaConfig) Schema
	}{
		{"v1 schema", v1Schema},
		{"v2 schema", v2Schema},
		{"v3 schema", v3Schema},
		{"v4 schema", v4Schema},
		{"v5 schema", v5Schema},
		{"v6 schema", v6Schema},
		{"v7 schema", v7Schema},
	}

	nameMatcher := mustNewLabelMatcher(metric.Equal, model.MetricNameLabel, "foo")

	for _, tc := range []struct {
		query    string
		expect   []Chunk
		matchers []*metric.LabelMatcher
	}{
		{
			`foo`,
			[]Chunk{chunk1, chunk2},
			[]*metric.LabelMatcher{nameMatcher},
		},
		{
			`foo{flip=""}`,
			[]Chunk{chunk2},
			[]*metric.LabelMatcher{nameMatcher, mustNewLabelMatcher(metric.Equal, "flip", "")},
		},
		{
			`foo{bar="baz"}`,
			[]Chunk{chunk1},
			[]*metric.LabelMatcher{nameMatcher, mustNewLabelMatcher(metric.Equal, "bar", "baz")},
		},
		{
			`foo{bar="beep"}`,
			[]Chunk{chunk2},
			[]*metric.LabelMatcher{nameMatcher, mustNewLabelMatcher(metric.Equal, "bar", "beep")},
		},
		{
			`foo{toms="code"}`,
			[]Chunk{chunk1, chunk2},
			[]*metric.LabelMatcher{nameMatcher, mustNewLabelMatcher(metric.Equal, "toms", "code")},
		},
		{
			`foo{bar!="baz"}`,
			[]Chunk{chunk2},
			[]*metric.LabelMatcher{nameMatcher, mustNewLabelMatcher(metric.NotEqual, "bar", "baz")},
		},
		{
			`foo{bar=~"beep|baz"}`,
			[]Chunk{chunk1, chunk2},
			[]*metric.LabelMatcher{nameMatcher, mustNewLabelMatcher(metric.RegexMatch, "bar", "beep|baz")},
		},
		{
			`foo{toms="code", bar=~"beep|baz"}`,
			[]Chunk{chunk1, chunk2},
			[]*metric.LabelMatcher{nameMatcher, mustNewLabelMatcher(metric.Equal, "toms", "code"), mustNewLabelMatcher(metric.RegexMatch, "bar", "beep|baz")},
		},
		{
			`foo{toms="code", bar="baz"}`,
			[]Chunk{chunk1}, []*metric.LabelMatcher{nameMatcher, mustNewLabelMatcher(metric.Equal, "toms", "code"), mustNewLabelMatcher(metric.Equal, "bar", "baz")},
		},
	} {
		for _, schema := range schemas {
			t.Run(fmt.Sprintf("%s / %s", tc.query, schema.name), func(t *testing.T) {
				log.Infoln("========= Running query", tc.query, "with schema", schema.name)
				store := newTestChunkStore(t, StoreConfig{
					schemaFactory: schema.fn,
				})

				if err := store.Put(ctx, []Chunk{chunk1, chunk2}); err != nil {
					t.Fatal(err)
				}

				chunks, err := store.Get(ctx, now.Add(-time.Hour), now, tc.matchers...)
				require.NoError(t, err)

				if !reflect.DeepEqual(tc.expect, chunks) {
					t.Fatalf("%s: wrong chunks - %s", tc.query, test.Diff(tc.expect, chunks))
				}
			})
		}
	}
}

// TestChunkStoreMetricNames tests no metric name queries supported from v7Schema
func TestChunkStoreMetricNames(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), userID)
	now := model.Now()

	foo1Chunk1 := dummyChunkFor(model.Metric{
		model.MetricNameLabel: "foo1",
		"bar":  "baz",
		"toms": "code",
		"flip": "flop",
	})
	foo1Chunk2 := dummyChunkFor(model.Metric{
		model.MetricNameLabel: "foo1",
		"bar":  "beep",
		"toms": "code",
	})
	foo2Chunk := dummyChunkFor(model.Metric{
		model.MetricNameLabel: "foo2",
		"bar":  "beep",
		"toms": "code",
	})
	foo3Chunk := dummyChunkFor(model.Metric{
		model.MetricNameLabel: "foo3",
		"bar":  "beep",
		"toms": "code",
	})

	schemas := []struct {
		name string
		fn   func(cfg SchemaConfig) Schema
	}{
		{"v7 schema", v7Schema},
	}

	for _, tc := range []struct {
		query    string
		expect   []Chunk
		matchers []*metric.LabelMatcher
	}{
		{
			`foo1`,
			[]Chunk{foo1Chunk1, foo1Chunk2},
			[]*metric.LabelMatcher{mustNewLabelMatcher(metric.Equal, model.MetricNameLabel, "foo1")},
		},
		{
			`foo2`,
			[]Chunk{foo2Chunk},
			[]*metric.LabelMatcher{mustNewLabelMatcher(metric.Equal, model.MetricNameLabel, "foo2")},
		},
		{
			`foo3`,
			[]Chunk{foo3Chunk},
			[]*metric.LabelMatcher{mustNewLabelMatcher(metric.Equal, model.MetricNameLabel, "foo3")},
		},

		// When name matcher is used without Equal, start matching all metric names
		// however still filter out metric names which do not match query
		{
			`{__name__!="foo1"}`,
			[]Chunk{foo3Chunk, foo2Chunk},
			[]*metric.LabelMatcher{mustNewLabelMatcher(metric.NotEqual, model.MetricNameLabel, "foo1")},
		},
		{
			`{__name__=~"foo1|foo2"}`,
			[]Chunk{foo1Chunk1, foo2Chunk, foo1Chunk2},
			[]*metric.LabelMatcher{mustNewLabelMatcher(metric.RegexMatch, model.MetricNameLabel, "foo1|foo2")},
		},

		// No metric names
		{
			`{bar="baz"}`,
			[]Chunk{foo1Chunk1},
			[]*metric.LabelMatcher{mustNewLabelMatcher(metric.Equal, "bar", "baz")},
		},
		{
			`{bar="beep"}`,
			[]Chunk{foo3Chunk, foo2Chunk, foo1Chunk2}, // doesn't match foo1 chunk1
			[]*metric.LabelMatcher{mustNewLabelMatcher(metric.Equal, "bar", "beep")},
		},
		{
			`{flip=""}`,
			[]Chunk{foo3Chunk, foo2Chunk, foo1Chunk2}, // doesn't match foo1 chunk1 as it has a flip value
			[]*metric.LabelMatcher{mustNewLabelMatcher(metric.Equal, "flip", "")},
		},
		{
			`{bar!="beep"}`,
			[]Chunk{foo1Chunk1},
			[]*metric.LabelMatcher{mustNewLabelMatcher(metric.NotEqual, "bar", "beep")},
		},
		{
			`{bar=~"beep|baz"}`,
			[]Chunk{foo3Chunk, foo1Chunk1, foo2Chunk, foo1Chunk2},
			[]*metric.LabelMatcher{mustNewLabelMatcher(metric.RegexMatch, "bar", "beep|baz")},
		},
		{
			`{toms="code", bar=~"beep|baz"}`,
			[]Chunk{foo3Chunk, foo1Chunk1, foo2Chunk, foo1Chunk2},
			[]*metric.LabelMatcher{mustNewLabelMatcher(metric.Equal, "toms", "code"), mustNewLabelMatcher(metric.RegexMatch, "bar", "beep|baz")},
		},
		{
			`{toms="code", bar="baz"}`,
			[]Chunk{foo1Chunk1},
			[]*metric.LabelMatcher{mustNewLabelMatcher(metric.Equal, "toms", "code"), mustNewLabelMatcher(metric.Equal, "bar", "baz")},
		},
	} {
		for _, schema := range schemas {
			t.Run(fmt.Sprintf("%s / %s", tc.query, schema.name), func(t *testing.T) {
				log.Infoln("========= Running query", tc.query, "with schema", schema.name)
				store := newTestChunkStore(t, StoreConfig{
					schemaFactory: schema.fn,
				})

				if err := store.Put(ctx, []Chunk{foo1Chunk1, foo1Chunk2, foo2Chunk, foo3Chunk}); err != nil {
					t.Fatal(err)
				}

				chunks, err := store.Get(ctx, now.Add(-time.Hour), now, tc.matchers...)
				require.NoError(t, err)

				if !reflect.DeepEqual(tc.expect, chunks) {
					t.Fatalf("%s: wrong chunks - %s", tc.query, test.Diff(tc.expect, chunks))
				}
			})
		}
	}
}

func mustNewLabelMatcher(matchType metric.MatchType, name model.LabelName, value model.LabelValue) *metric.LabelMatcher {
	matcher, err := metric.NewLabelMatcher(matchType, name, value)
	if err != nil {
		panic(err)
	}
	return matcher
}

func TestChunkStoreRandom(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), userID)
	schemas := []struct {
		name  string
		fn    func(cfg SchemaConfig) Schema
		store *Store
	}{
		{name: "v1 schema", fn: v1Schema},
		{name: "v2 schema", fn: v2Schema},
		{name: "v3 schema", fn: v3Schema},
		{name: "v4 schema", fn: v4Schema},
		{name: "v5 schema", fn: v5Schema},
		{name: "v6 schema", fn: v6Schema},
		{name: "v7 schema", fn: v7Schema},
	}

	for i := range schemas {
		schemas[i].store = newTestChunkStore(t, StoreConfig{
			schemaFactory: schemas[i].fn,
		})
	}

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
		for _, s := range schemas {
			err := s.store.Put(ctx, []Chunk{chunk})
			require.NoError(t, err)
		}
	}

	// pick two random numbers and do a query
	for i := 0; i < 100; i++ {
		start := rand.Int63n(100 * chunkLen)
		end := start + rand.Int63n((100*chunkLen)-start)
		assert.True(t, start < end)

		startTime := model.TimeFromUnix(start)
		endTime := model.TimeFromUnix(end)

		for _, s := range schemas {
			chunks, err := s.store.Get(ctx, startTime, endTime,
				mustNewLabelMatcher(metric.Equal, model.MetricNameLabel, "foo"),
				mustNewLabelMatcher(metric.Equal, "bar", "baz"),
			)
			require.NoError(t, err)

			// We need to check that each chunk is in the time range
			for _, chunk := range chunks {
				assert.False(t, chunk.From.After(endTime))
				assert.False(t, chunk.Through.Before(startTime))
				samples, err := chunk.samples()
				assert.NoError(t, err)
				assert.Equal(t, 1, len(samples))
				// TODO verify chunk contents
			}

			// And check we got all the chunks we want
			numChunks := (end / chunkLen) - (start / chunkLen) + 1
			assert.Equal(t, int(numChunks), len(chunks), s.name)
		}
	}
}

func TestChunkStoreLeastRead(t *testing.T) {
	// Test we don't read too much from the index
	ctx := user.InjectOrgID(context.Background(), userID)
	store := newTestChunkStore(t, StoreConfig{
		schemaFactory: v6Schema,
	})

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
		log.Infof("Loop %d", i)
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

		chunks, err := store.Get(ctx, startTime, endTime,
			mustNewLabelMatcher(metric.Equal, model.MetricNameLabel, "foo"),
			mustNewLabelMatcher(metric.Equal, "bar", "baz"),
		)
		if err != nil {
			t.Fatal(t, err)
		}

		// We need to check that each chunk is in the time range
		for _, chunk := range chunks {
			assert.False(t, chunk.From.After(endTime))
			assert.False(t, chunk.Through.Before(startTime))
			samples, err := chunk.samples()
			assert.NoError(t, err)
			assert.Equal(t, 1, len(samples))
		}

		// And check we got all the chunks we want
		numChunks := 24 - (start / chunkLen) + 1
		assert.Equal(t, int(numChunks), len(chunks))
	}
}
