package hintprovider

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/RoaringBitmap/roaring"
	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logline"
	"github.com/grafana/loki/v3/pkg/logline/format"
	"github.com/grafana/loki/v3/pkg/logline/store"
)

// The hint cache keys on needleKey, so two queries with the same key must get
// the same hints from the real lookup. These tests run the lookup, not a stub,
// to catch it starting to depend on something the key leaves out.

// singleNeedlePairs have the same key and a single needle, "timeout".
var singleNeedlePairs = [][2]string{
	{
		`{app="api"} |= "timeout"`,
		`sum by (level, detected_level) (count_over_time({app="web"} |= "timeout" | drop __error__ [5m]))`,
	},
	{`{app="api"} |= "TIMEOUT"`, `{app="api"} |~ "timeout" | json`},
	{
		`{app="api"} |= "timeout"`,
		`{app="api"} |= "timeout" | json | drop __error__ | level="critical"`, // the drop hides the filter
	},
}

func TestNeedleKey_EqualKeysGetEqualHints(t *testing.T) {
	const ngram = 6
	t0 := time.Date(2026, 2, 26, 10, 0, 0, 0, time.UTC)
	t1 := t0.Add(5 * time.Minute)
	indexStore := newTestStore(t)
	// "timeout" is in both documents and "failure" only in the second, so
	// requiring both narrows the hints to the second.
	writeNeedleIndex(t, indexStore, "aaaaaaaaaaaaaaaa",
		[]format.DocumentMetadata{
			{ID: 0, MinTimeUnix: t0.UnixMilli(), MaxTimeUnix: t0.Add(time.Minute).UnixMilli()},
			{ID: 1, MinTimeUnix: t1.UnixMilli(), MaxTimeUnix: t1.Add(time.Minute).UnixMilli()},
		},
		map[string][]uint32{"timeout": {0, 1}, "failure": {1}})
	provider, err := NewLoglineHintProvider(indexStore, ngram, 0, nil, log.NewNopLogger(), nil)
	require.NoError(t, err)
	hints := func(query string) []HintTimeRange {
		return realHints(t, provider, query, t0.Add(-time.Minute), t1.Add(2*time.Minute))
	}

	pairs := append([][2]string{
		// Filter order doesn't matter, and the second query adds a stage.
		{
			`{app="api"} |= "timeout" |= "failure"`,
			`{app="web"} |= "failure" |= "timeout" | json`,
		},
	}, singleNeedlePairs...)
	for _, pair := range pairs {
		keyA, okA := needleKey(mustParseExpr(t, pair[0]), ngram)
		keyB, okB := needleKey(mustParseExpr(t, pair[1]), ngram)
		require.True(t, okA && okB, pair)
		require.Equal(t, keyA, keyB, "test case should share a key: %v", pair)

		got := hints(pair[0])
		require.NotEmpty(t, got, pair)
		require.Equal(t, got, hints(pair[1]), pair)
	}

	// The check can tell hints apart: an extra needle changes the key and the
	// hints.
	one, two := `{app="api"} |= "timeout"`, `{app="api"} |= "timeout" |= "failure"`
	keyOne, _ := needleKey(mustParseExpr(t, one), ngram)
	keyTwo, _ := needleKey(mustParseExpr(t, two), ngram)
	require.NotEqual(t, keyOne, keyTwo)
	require.NotEqual(t, hints(one), hints(two))
}

// v5 ranges come from the footer's layout, one per document shard, so check
// that equal keys still mean equal hints there.
func TestNeedleKey_EqualKeysGetEqualHints_V5(t *testing.T) {
	const ngram = 6
	day := time.Date(2026, 2, 26, 0, 0, 0, 0, time.UTC)
	indexStore := newTestStore(t)
	writeV5TestIndex(t, indexStore, "aaaaaaaaaaaaaaaa", "timeout", day, day,
		[]uint32{v5Cell(2250, 3), v5Cell(2250, 17), v5Cell(2260, 3)})
	provider, err := NewLoglineHintProvider(indexStore, ngram, 0, nil, log.NewNopLogger(), nil)
	require.NoError(t, err)

	t0 := day.Add(10 * time.Hour)
	for _, pair := range singleNeedlePairs {
		got := realHints(t, provider, pair[0], t0.Add(-time.Minute), t0.Add(5*time.Minute))
		require.NotEmpty(t, got, pair)
		require.Equal(t, got, realHints(t, provider, pair[1], t0.Add(-time.Minute), t0.Add(5*time.Minute)), pair)
	}
}

func realHints(t *testing.T, p *LoglineHintProvider, query string, from, through time.Time) []HintTimeRange {
	t.Helper()
	hints, _, err := p.ProvideHints(context.Background(), "test-tenant", mustParseExpr(t, query),
		model.TimeFromUnixNano(from.UnixNano()), model.TimeFromUnixNano(through.UnixNano()))
	require.NoError(t, err, query)
	require.NotNil(t, hints, query)
	return hints.TimeRanges
}

// writeNeedleIndex stores an index over docs where each needle's terms are posted
// to the document IDs listed for it.
func writeNeedleIndex(t *testing.T, indexStore *store.Store, hash string, docs []format.DocumentMetadata, postings map[string][]uint32) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "needles.lidx")
	writer, err := logline.NewWriter(logline.CurrentVersion, path, docs, nil)
	require.NoError(t, err)

	byTerm := map[string][]uint32{}
	for needle, ids := range postings {
		terms, err := ExtractQueryNgrams(needle, 6, logline.CurrentVersion)
		require.NoError(t, err)
		for _, term := range terms {
			byTerm[term] = ids
		}
	}
	terms := make([]string, 0, len(byTerm))
	for term := range byTerm {
		terms = append(terms, term)
	}
	sort.Strings(terms)
	for _, term := range terms {
		ids := roaring.New()
		ids.AddMany(byTerm[term])
		var key [8]byte
		copy(key[:], term)
		require.NoError(t, writer.WriteTermBitmap(key, format.Bitmap{Roaring: ids}))
	}
	require.NoError(t, writer.Close())

	reader, _, err := logline.OpenFile(path)
	require.NoError(t, err)
	header := reader.ReadHeader()
	require.NoError(t, reader.Close())
	data, err := os.ReadFile(path)
	require.NoError(t, err)

	minTS := time.UnixMilli(docs[0].MinTimeUnix).UTC()
	maxTS := time.UnixMilli(docs[len(docs)-1].MaxTimeUnix).UTC()
	meta := store.Meta{
		Date:        minTS.Format("2006-01-02"),
		Hash:        hash,
		Version:     logline.CurrentVersion,
		MinLogTs:    minTS,
		MaxLogTs:    maxTS,
		MinRecordTs: minTS,
		MaxRecordTs: maxTS,
		IndexHeader: &header,
		SizeBytes:   int64(len(data)),
	}
	require.NoError(t, indexStore.PutIndex(context.Background(), bytes.NewReader(data), meta))
	require.NoError(t, indexStore.Poll(context.Background()))
}
