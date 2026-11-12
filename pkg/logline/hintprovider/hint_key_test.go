package hintprovider

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RoaringBitmap/roaring"
	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logline"
	"github.com/grafana/loki/v3/pkg/logline/format"
	"github.com/grafana/loki/v3/pkg/logline/store"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

// Tests for the hint cache key (needleKey). There are three layers:
//
//  1. The key builder: which queries get the same key.
//  2. The cache wiring: the key is used for entries and singleflight, and the
//     delegate is asked about the expression as written.
//  3. The contract: queries with equal keys get equal hints from the real
//     lookup. This is what makes sharing an entry safe, so it must fail if the
//     lookup starts to read anything the key leaves out.

const (
	keyTestLogQuery = `{app="api"} |= "timeout"`
	// The shape of Grafana's logs volume query for keyTestLogQuery.
	keyTestVolumeQuery = `sum by (level, detected_level) (count_over_time({app="api"} |= "timeout" | drop __error__ [5m]))`
)

// --- 1. The key builder ---

func TestNeedleKey(t *testing.T) {
	const ngram = 6

	key := func(t *testing.T, query string, ngramLength int) string {
		t.Helper()
		k, ok := needleKey(mustParseExpr(t, query), ngramLength)
		require.True(t, ok, query)
		return k
	}

	base := key(t, `{app="api"} |= "timeout"`, ngram)

	// Queries with the same needles share a key, however the rest of the query
	// differs.
	for _, q := range []string{
		// The lookup does not use stream matchers, so they are not part of the key.
		`{app="web"} |= "timeout"`,
		`{app="api", env="prod"} |= "timeout"`,
		`{app="api"} |= "timeout" | drop __error__`,
		`sum by (level, detected_level) (count_over_time({app="api"} |= "timeout" | drop __error__ [1m]))`,
		`rate({app="api"} |= "timeout" [5m] offset 10m)`,
		`{app="api"} |= "timeout" | json | line_format "{{.msg}}"`,
		`{app="api"} |= "TIMEOUT"`,
		`{app="api"} |~ "timeout"`,
		// The drop stops the later filter from becoming a needle, so the lookup is
		// the same as for the plain query.
		`{app="api"} |= "timeout" | json | drop __error__ | level="critical"`,
	} {
		require.Equal(t, base, key(t, q, ngram), q)
	}
	require.Equal(t,
		key(t, `{app="api", env="prod"} |= "timeout" |= "failure"`, ngram),
		key(t, `{env="prod", app="api"} |= "failure" |= "timeout"`, ngram),
		"matcher and filter order do not matter")

	// Anything that changes the lookup changes the key.
	for _, q := range []string{
		`{app="api"} |= "timeouts"`,
		`{app="api"} |= "timeout" |= "failure"`,
		`{app="api"} |= "timeout" | json | level="critical"`,
	} {
		require.NotEqual(t, base, key(t, q, ngram), q)
	}
	require.NotEqual(t, base, key(t, `{app="api"} |= "timeout"`, 5), "n-gram length")

	// Every needle counts: two needles differ from either one alone, whichever
	// sorts first.
	both := key(t, `{app="api"} |= "timeout" |= "failure"`, ngram)
	require.NotEqual(t, both, key(t, `{app="api"} |= "timeout"`, ngram))
	require.NotEqual(t, both, key(t, `{app="api"} |= "failure"`, ngram))

	// Needles are arbitrary bytes, so one needle containing the separator must
	// not look like two.
	require.NotEqual(t,
		key(t, `{app="api"} |= "a,b"`, 1),
		key(t, `{app="api"} |= "a" |= "b"`, 1))
}

func TestNeedleKey_NotUsable(t *testing.T) {
	for name, tc := range map[string]struct {
		query string
		ngram int
	}{
		"no needles":          {`{app="api"}`, 6},
		"only a short needle": {`{app="api"} |= "err"`, 6},
		"unsupported filter":  {`{app="api"} |= "a" or "b"`, 6},
		"no n-gram length":    {`{app="api"} |= "timeout"`, 0},
		"binary operation":    {`count_over_time({app="api"} |= "timeout" [5m]) / count_over_time({app="api"} |= "timeout" [5m])`, 6},
	} {
		t.Run(name, func(t *testing.T) {
			_, ok := needleKey(mustParseExpr(t, tc.query), tc.ngram)
			require.False(t, ok)
		})
	}
}

// The adapter sends expr.String() and the querier looks up the needles of the
// re-parsed expression, so those must be the needles in the key.
func TestNeedleKey_MatchesQuerierNeedles(t *testing.T) {
	for _, q := range []string{
		keyTestVolumeQuery,
		"{app=\"api\"} |= `say \"hello\"`",
		`{app="api"} |~ "time\\.out\\d+"`,
		`{app="api"} |~ "(?i)timeout"`,
		`{app="api"} |= "a,b:c\"d\\e"`,
		`{app="api"} |= "\xff\xfetimeout"`,
		`{app="api"} |= "timeout" |= "failure" or "error"`,
		`{app="api"} |= "timeout" | (level="critical" or level="warning") and region="us-east"`,
		`{app="api"} |= "timeout" | json | msg="failure" | line_format "{{.msg}}" | logfmt | other="ignored"`,
		`{app="api"} | logfmt trace_id="tid" | trace_id="abc123def"`,
		`sum(sum_over_time({app="api"} |= "timeout" | unwrap latency | region="us-east" [5m]))`,
	} {
		expr := mustParseExpr(t, q)
		for _, n := range []int{3, 6} {
			key, ok := needleKey(expr, n)
			require.True(t, ok, q)

			needles := SupportedQuery(mustParseExpr(t, expr.String()), n)
			sort.Strings(needles)
			for i := range needles {
				needles[i] = strconv.Quote(needles[i])
			}
			require.Equal(t, key, fmt.Sprintf("n=%d f=%s", n, strings.Join(needles, ",")), q)
		}
	}
}

// --- 2. The cache wiring ---

// ngramDelegate is a delegate that reports an n-gram length.
type ngramDelegate struct {
	*stubHintProvider
	n int
}

func (d ngramDelegate) NgramLength() int { return d.n }

func TestCacheKeyNgramLength(t *testing.T) {
	require.Zero(t, cacheKeyNgramLength(&stubHintProvider{}), "a delegate that reports none")
	require.Equal(t, 7, cacheKeyNgramLength(ngramDelegate{&stubHintProvider{}, 7}))

	provider, err := NewLoglineHintProvider(newTestStore(t), 6, 0, nil, nil, nil)
	require.NoError(t, err)
	require.Equal(t, 6, cacheKeyNgramLength(provider))

	var none *LoglineHintProvider
	require.Zero(t, none.NgramLength())
}

// exprRecorder records the expressions its delegate is asked about.
type exprRecorder struct {
	ngramDelegate
	mu    sync.Mutex
	exprs []string
}

func (r *exprRecorder) ProvideHints(
	ctx context.Context, tenant string, expr syntax.Expr, from, through model.Time,
) (*Hints, *QueryStats, error) {
	r.mu.Lock()
	r.exprs = append(r.exprs, expr.String())
	r.mu.Unlock()
	return r.ngramDelegate.ProvideHints(ctx, tenant, expr, from, through)
}

func newRecorder(ngramLength int, delay time.Duration) *exprRecorder {
	hints := &Hints{TimeRanges: []HintTimeRange{{
		Start: time.Date(2026, 3, 10, 0, 10, 0, 0, time.UTC),
		End:   time.Date(2026, 3, 10, 0, 20, 0, 0, time.UTC),
	}}}
	return &exprRecorder{ngramDelegate: ngramDelegate{&stubHintProvider{hints: hints, delay: delay}, ngramLength}}
}

func provideKeyTestHints(t *testing.T, p *CachingHintProvider, query string, from, through time.Time) {
	t.Helper()
	_, _, err := p.ProvideHints(context.Background(), "tenant-a", mustParseExpr(t, query),
		model.TimeFromUnixNano(from.UnixNano()), model.TimeFromUnixNano(through.UnixNano()))
	require.NoError(t, err)
}

func TestCachingHintProvider_SharesEntriesByNeedles(t *testing.T) {
	from := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)
	through := from.Add(time.Hour)

	t.Run("the delegate reports an n-gram length", func(t *testing.T) {
		delegate := newRecorder(6, 0)
		provider := NewCachingHintProvider(delegate, newMockHintCacheBackend(), prometheus.NewRegistry())

		provideKeyTestHints(t, provider, keyTestVolumeQuery, from, through)
		provideKeyTestHints(t, provider, keyTestLogQuery, from, through)
		// One lookup. The delegate was asked about the volume query as written, not
		// about a selector or the key, and the log query was served from its entry.
		require.Equal(t, []string{mustParseExpr(t, keyTestVolumeQuery).String()}, delegate.exprs)

		provideKeyTestHints(t, provider, `{app="api"} |= "another-needle"`, from, through)
		require.Len(t, delegate.exprs, 2, "different needles are a different entry")
	})

	t.Run("the delegate reports none", func(t *testing.T) {
		delegate := newRecorder(0, 0)
		provider := NewCachingHintProvider(delegate, newMockHintCacheBackend(), prometheus.NewRegistry())

		provideKeyTestHints(t, provider, keyTestVolumeQuery, from, through)
		provideKeyTestHints(t, provider, keyTestLogQuery, from, through)
		require.Len(t, delegate.exprs, 2, "keyed on the expression, so nothing is shared")
	})
}

// Concurrent lookups for a log query and a volume query with the same needles
// share one delegate call.
func TestCachingHintProvider_SingleflightSharesByNeedles(t *testing.T) {
	delegate := newRecorder(6, 100*time.Millisecond)
	provider := NewCachingHintProvider(delegate, newMockHintCacheBackend(), prometheus.NewRegistry())
	from := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)
	through := from.Add(time.Hour)

	logExpr := mustParseExpr(t, keyTestLogQuery)
	volumeExpr := mustParseExpr(t, keyTestVolumeQuery)

	const workers = 8
	var wg sync.WaitGroup
	errCh := make(chan error, workers)
	for i := range workers {
		expr := logExpr
		if i%2 == 1 {
			expr = volumeExpr
		}
		wg.Go(func() {
			_, _, err := provider.ProvideHints(context.Background(), "tenant-a", expr,
				model.TimeFromUnixNano(from.UnixNano()), model.TimeFromUnixNano(through.UnixNano()))
			errCh <- err
		})
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		require.NoError(t, err)
	}
	require.Equal(t, 1, delegate.Calls())
}

// --- 3. The contract: equal keys, equal hints, from the real lookup ---

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
