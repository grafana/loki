package hintprovider

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logline/store"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

// Tests for the hint cache key (needleKey). There are three layers:
//
//  1. The key builder: which queries get the same key.
//  2. The cache wiring: the key is used for entries and singleflight, and the
//     delegate is asked about the expression as written.
//  3. The contract. The key is the needles, so different queries share an entry.
//     That is safe only while the lookup's hints depend on nothing the key
//     leaves out. These tests run the real lookup and must fail if it starts to
//     read stream matchers or the rest of the pipeline.

const (
	keyTestLogQuery = `{app="api"} |= "timeout"`
	// The shape of Grafana's logs volume query for keyTestLogQuery.
	keyTestVolumeQuery = `sum by (level, detected_level) (count_over_time({app="api"} |= "timeout" | drop __error__ [5m]))`
)

// --- 1. The key builder ---

func needleKeyOf(t *testing.T, query string, ngram int) string {
	t.Helper()
	k, ok := needleKey(mustParseExpr(t, query), ngram)
	require.True(t, ok, query)
	return k
}

func TestNeedleKey(t *testing.T) {
	const ngram = 6
	base := needleKeyOf(t, keyTestLogQuery, ngram)

	// Same needles, same key, however the rest of the query differs. The contract
	// test below has more such pairs.
	for _, q := range []string{
		keyTestVolumeQuery,
		`{app="web", env="prod"} |= "timeout"`, // the lookup does not use stream matchers
	} {
		require.Equal(t, base, needleKeyOf(t, q, ngram), q)
	}

	// Anything that changes the lookup changes the key.
	for _, q := range []string{
		`{app="api"} |= "timeouts"`,
		`{app="api"} |= "timeout" | json | level="critical"`,
	} {
		require.NotEqual(t, base, needleKeyOf(t, q, ngram), q)
	}
	require.NotEqual(t, base, needleKeyOf(t, keyTestLogQuery, 5), "n-gram length")

	require.Equal(t,
		needleKeyOf(t, `{app="api"} |= "timeout" |= "failure"`, ngram),
		needleKeyOf(t, `{app="api"} |= "failure" |= "timeout"`, ngram), "filter order")

	// Every needle counts: two needles differ from either one alone, whichever
	// sorts first.
	both := needleKeyOf(t, `{app="api"} |= "timeout" |= "failure"`, ngram)
	require.NotEqual(t, both, base)
	require.NotEqual(t, both, needleKeyOf(t, `{app="api"} |= "failure"`, ngram))

	// Needles are arbitrary bytes: one needle containing the separator must not
	// look like two.
	require.NotEqual(t,
		needleKeyOf(t, `{app="api"} |= "a,b"`, 1),
		needleKeyOf(t, `{app="api"} |= "a" |= "b"`, 1))
}

func TestNeedleKey_NotUsable(t *testing.T) {
	for name, tc := range map[string]struct {
		query string
		ngram int
	}{
		"no needles":       {`{app="api"}`, 6},
		"no n-gram length": {keyTestLogQuery, 0},
		"binary operation": {`count_over_time({app="api"} |= "timeout" [5m]) / count_over_time({app="api"} |= "timeout" [5m])`, 6},
	} {
		t.Run(name, func(t *testing.T) {
			_, ok := needleKey(mustParseExpr(t, tc.query), tc.ngram)
			require.False(t, ok)
		})
	}
}

// The adapter sends expr.String() and the querier looks up the needles of the
// re-parsed expression, so the key must not change when the expression is
// re-parsed.
func TestNeedleKey_SurvivesReparse(t *testing.T) {
	for _, q := range []string{
		keyTestVolumeQuery,
		"{app=\"api\"} |= `say \"hello\"`",
		`{app="api"} |~ "time\\.out\\d+"`,
		`{app="api"} |= "a,b:c\"d\\e"`,
		`{app="api"} |= "\xff\xfetimeout"`,
		`{app="api"} |= "timeout" |= "failure" or "error"`,
		`{app="api"} |= "timeout" | (level="critical" or level="warning") and region="us-east"`,
		`{app="api"} |= "timeout" | json | msg="failure" | line_format "{{.msg}}" | logfmt | other="ignored"`,
	} {
		expr := mustParseExpr(t, q)
		for _, n := range []int{3, 6} {
			key, ok := needleKey(expr, n)
			require.True(t, ok, q)
			reparsed, ok := needleKey(mustParseExpr(t, expr.String()), n)
			require.True(t, ok, q)
			require.Equal(t, key, reparsed, q)
		}
	}
}

// --- 2. The cache wiring ---

// ngramDelegate is a delegate that reports an n-gram length and records the
// expressions it is asked about.
type ngramDelegate struct {
	*stubHintProvider
	n int

	mu    sync.Mutex
	exprs []string
}

func (d *ngramDelegate) NgramLength() int { return d.n }

func (d *ngramDelegate) ProvideHints(
	ctx context.Context, tenant string, expr syntax.Expr, from, through model.Time,
) (*Hints, *QueryStats, error) {
	d.mu.Lock()
	d.exprs = append(d.exprs, expr.String())
	d.mu.Unlock()
	return d.stubHintProvider.ProvideHints(ctx, tenant, expr, from, through)
}

func newNgramDelegate(delay time.Duration) *ngramDelegate {
	hints := &Hints{TimeRanges: []HintTimeRange{{
		Start: time.Date(2026, 3, 10, 0, 10, 0, 0, time.UTC),
		End:   time.Date(2026, 3, 10, 0, 20, 0, 0, time.UTC),
	}}}
	return &ngramDelegate{stubHintProvider: &stubHintProvider{hints: hints, delay: delay}, n: 6}
}

func provideKeyTestHints(p *CachingHintProvider, expr syntax.Expr) error {
	from := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)
	_, _, err := p.ProvideHints(context.Background(), "tenant-a", expr,
		model.TimeFromUnixNano(from.UnixNano()), model.TimeFromUnixNano(from.Add(time.Hour).UnixNano()))
	return err
}

func TestCachingHintProvider_SharesEntriesByNeedles(t *testing.T) {
	delegate := newNgramDelegate(0)
	provider := NewCachingHintProvider(delegate, newMockHintCacheBackend(), prometheus.NewRegistry())

	require.NoError(t, provideKeyTestHints(provider, mustParseExpr(t, keyTestVolumeQuery)))
	require.NoError(t, provideKeyTestHints(provider, mustParseExpr(t, keyTestLogQuery)))
	// One lookup, for the volume query as written, not a selector or the key.
	require.Equal(t, []string{mustParseExpr(t, keyTestVolumeQuery).String()}, delegate.exprs)

	require.NoError(t, provideKeyTestHints(provider, mustParseExpr(t, `{app="api"} |= "another-needle"`)))
	require.Len(t, delegate.exprs, 2, "different needles are a different entry")

	// The key uses the delegate's n-gram length. At 8, "timeout" is too short to
	// be a needle, so there is nothing to share and the queries key on their text.
	delegate = newNgramDelegate(0)
	delegate.n = 8
	provider = NewCachingHintProvider(delegate, newMockHintCacheBackend(), prometheus.NewRegistry())
	require.NoError(t, provideKeyTestHints(provider, mustParseExpr(t, keyTestVolumeQuery)))
	require.NoError(t, provideKeyTestHints(provider, mustParseExpr(t, keyTestLogQuery)))
	require.Len(t, delegate.exprs, 2)
}

// Concurrent lookups for a log query and a volume query with the same needles
// share one delegate call. The delay only makes the lookups overlap: if they
// don't, the second is a cache hit and a broken singleflight key goes unseen.
func TestCachingHintProvider_SingleflightSharesByNeedles(t *testing.T) {
	delegate := newNgramDelegate(100 * time.Millisecond)
	provider := NewCachingHintProvider(delegate, newMockHintCacheBackend(), prometheus.NewRegistry())

	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i, q := range []string{keyTestLogQuery, keyTestVolumeQuery, keyTestLogQuery, keyTestVolumeQuery} {
		expr := mustParseExpr(t, q)
		wg.Go(func() { errs[i] = provideKeyTestHints(provider, expr) })
	}
	wg.Wait()

	require.NoError(t, errors.Join(errs...))
	require.Equal(t, 1, delegate.Calls())
}

// --- 3. The contract: equal keys, equal hints, from the real lookup ---

// singleNeedlePairs have the same key and a single needle, "timeout". They differ
// in what the key leaves out: stream matchers and the rest of the pipeline.
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
	day := time.Date(2026, 2, 26, 0, 0, 0, 0, time.UTC)
	t5 := day.Add(10 * time.Hour)

	v3 := newTestStore(t)
	writeTestIndex(t, v3, "aaaaaaaaaaaaaaaa", "timeout", t0, t0.Add(time.Minute))
	// v5 ranges come from the footer's layout, one per document shard.
	v5 := newTestStore(t)
	writeV5TestIndex(t, v5, "aaaaaaaaaaaaaaaa", "timeout", day, day,
		[]uint32{v5Cell(2250, 3), v5Cell(2250, 17), v5Cell(2260, 3)})

	for _, tc := range []struct {
		name     string
		store    *store.Store
		from, to time.Time
		pairs    [][2]string
		distinct [2]string // queries with different hints, so the check can tell hints apart
	}{
		{
			"v3", v3, t0.Add(-time.Minute), t0.Add(5 * time.Minute),
			singleNeedlePairs,
			[2]string{`{app="api"} |= "timeout"`, `{app="api"} |= "absent-needle"`},
		},
		{
			"v5", v5, t5.Add(-time.Minute), t5.Add(5 * time.Minute),
			singleNeedlePairs,
			[2]string{`{app="api"} |= "timeout"`, `{app="api"} |= "absent-needle"`},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider, err := NewLoglineHintProvider(tc.store, ngram, 0, nil, log.NewNopLogger(), nil)
			require.NoError(t, err)
			hints := func(query string) []HintTimeRange {
				h, _, err := provider.ProvideHints(context.Background(), "test-tenant", mustParseExpr(t, query),
					model.TimeFromUnixNano(tc.from.UnixNano()), model.TimeFromUnixNano(tc.to.UnixNano()))
				require.NoError(t, err, query)
				require.NotNil(t, h, query)
				return h.TimeRanges
			}

			for _, pair := range tc.pairs {
				require.Equal(t, needleKeyOf(t, pair[0], ngram), needleKeyOf(t, pair[1], ngram), "the pair should share a key: %v", pair)
				got := hints(pair[0])
				require.NotEmpty(t, got, pair)
				require.Equal(t, got, hints(pair[1]), pair)
			}
			require.NotEqual(t, hints(tc.distinct[0]), hints(tc.distinct[1]))
		})
	}
}
