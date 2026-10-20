package hintprovider

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logline"
	"github.com/grafana/loki/v3/pkg/logline/format"
	"github.com/grafana/loki/v3/pkg/logline/needle"
	"github.com/grafana/loki/v3/pkg/logline/shard"
	"github.com/grafana/loki/v3/pkg/logline/store"
)

// Two needles whose terms land on different shards, each in a different
// document. Their OR must return both documents. Intersecting the shards
// across the two conjunctions would compute their AND and return nothing.
func TestLoglineHintProvider_ExecuteQuery_OrAcrossShards(t *testing.T) {
	const shardCount = 10
	t0 := time.Date(2026, 8, 13, 10, 0, 0, 0, time.UTC)

	// Find two single-term needles on different shards.
	shardOf := func(lit string) int {
		terms, err := ExtractQueryNgrams(lit, 6, logline.CurrentVersion)
		require.NoError(t, err)
		require.Len(t, terms, 1)
		for value := range shardCount {
			meta := store.Meta{ShardCount: shardCount, ShardAlgorithm: shard.AlgorithmMurmur3Mix, ShardValue: value}
			if len(filterNgramsForShard(terms, meta)) == 1 {
				return value
			}
		}
		require.FailNow(t, "no shard for needle", lit)
		return 0
	}
	first, firstShard := "TERM00", shardOf("TERM00")
	var second string
	var secondShard int
	for i := 1; i < 100; i++ {
		lit := fmt.Sprintf("TERM%02d", i)
		if s := shardOf(lit); s != firstShard {
			second, secondShard = lit, s
			break
		}
	}
	require.NotEmpty(t, second)

	docs := []format.DocumentMetadata{
		{ID: 0, MinTimeUnix: t0.UnixMilli(), MaxTimeUnix: t0.Add(time.Second).UnixMilli()},
		{ID: 1, MinTimeUnix: t0.Add(2 * time.Second).UnixMilli(), MaxTimeUnix: t0.Add(3 * time.Second).UnixMilli()},
	}
	indexStore := newTestStore(t)
	writeShardedTermTestIndex(t, indexStore, logline.CurrentVersion, "1111111111111111", docs,
		map[string][]uint32{first: {0}}, firstShard)
	writeShardedTermTestIndex(t, indexStore, logline.CurrentVersion, "2222222222222222", docs,
		map[string][]uint32{second: {1}}, secondShard)

	provider, err := NewLoglineHintProvider(indexStore, 6, 0, 0, nil, log.NewNopLogger(), nil)
	require.NoError(t, err)
	active := indexStore.Snapshot().Active()
	require.Len(t, active, 2)

	t.Run("or returns both documents", func(t *testing.T) {
		byConj, err := provider.executeQuery(context.Background(),
			needle.Or(64, needle.Lit(first), needle.Lit(second)), active, NewQueryStats(), 6, 64)
		require.NoError(t, err)
		require.Equal(t, []HintTimeRange{
			{Start: t0, End: t0.Add(time.Second)},
			{Start: t0.Add(2 * time.Second), End: t0.Add(3 * time.Second)},
		}, withoutSources(aggregateConjunctionRanges(byConj)))
	})

	t.Run("and returns nothing", func(t *testing.T) {
		byConj, err := provider.executeQuery(context.Background(),
			needle.AllOf(first, second), active, NewQueryStats(), 6, 64)
		require.NoError(t, err)
		require.Empty(t, aggregateConjunctionRanges(byConj))
	})
}

// Every conjunction must be narrowable under every version in the window.
// v4 emits no term for a number shorter than 9 digits, so a conjunction made
// only of one makes the whole OR unconstrained. ANDing a literal v4 can look
// up into every conjunction narrows it again.
func TestBuildTermJobs_OrMixedVersions(t *testing.T) {
	v3 := minimalMeta("aaaaaaaaaaaaaaa1", "2026-01-01", "v3")
	v4 := minimalMeta("aaaaaaaaaaaaaaa2", "2026-01-02", "v4")
	or := needle.Or(64, needle.Lit("12345678"), needle.Lit("ABCDEFGH"))

	for _, tc := range []struct {
		name    string
		needles needle.Expr
		metas   []store.Meta
		wantErr error
	}{
		{"v3 narrows both conjunctions", or, []store.Meta{v3}, nil},
		{"v4 cannot narrow the number", or, []store.Meta{v4}, ErrUnconstrained},
		{"mixed window is unconstrained", or, []store.Meta{v3, v4}, ErrUnconstrained},
		{"shared literal narrows every conjunction", needle.And(64, needle.Lit("SEEDTEXTX"), or), []store.Meta{v3, v4}, nil},
		{"true is unconstrained", needle.True(), []store.Meta{v3}, ErrUnconstrained},
	} {
		t.Run(tc.name, func(t *testing.T) {
			jobs, metasByID, err := buildTermJobs(tc.needles, tc.metas, 6)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			require.NotEmpty(t, jobs)
			for _, meta := range tc.metas {
				require.Contains(t, metasByID, meta.ID())
			}
		})
	}
}

// A term needed by several conjunctions on one reader is one job that serves
// all of them.
func TestBuildTermJobs_SharedTermIsOneJob(t *testing.T) {
	meta := minimalMeta("aaaaaaaaaaaaaaa1", "2026-01-01", "v3")
	jobs, _, err := buildTermJobs(needle.Or(64, needle.Lit("ABCDEFG"), needle.Lit("BCDEFGH")), []store.Meta{meta}, 6)
	require.NoError(t, err)

	got := make(map[string][]int)
	for _, job := range jobs {
		require.NotContains(t, got, job.term, "term %s has two jobs", job.term)
		got[job.term] = job.conjs
	}
	require.Equal(t, map[string][]int{
		"ABCDEF": {0},
		"BCDEFG": {0, 1},
		"CDEFGH": {1},
	}, got)
}

func TestAggregateConjunctionRanges(t *testing.T) {
	t0 := time.Date(2026, 2, 26, 10, 0, 0, 0, time.UTC)
	at := func(m int) time.Time { return t0.Add(time.Duration(m) * time.Minute) }
	group := shardGroup{Version: "v3", ShardCount: 4, ShardAlgorithm: "first_byte"}
	shard0 := shardKey{shardGroup: group, ShardValue: 0}
	shard1 := shardKey{shardGroup: group, ShardValue: 1}

	t.Run("empty", func(t *testing.T) {
		require.Nil(t, aggregateConjunctionRanges(nil))
	})

	t.Run("shards intersect within a conjunction and conjunctions union", func(t *testing.T) {
		got := aggregateConjunctionRanges(map[int]map[shardKey][]HintTimeRange{
			0: {
				shard0: {{Start: at(0), End: at(10)}},
				shard1: {{Start: at(5), End: at(15)}},
			},
			1: {
				shard1: {{Start: at(20), End: at(30)}},
			},
		})
		require.Equal(t, []HintTimeRange{
			{Start: at(5), End: at(10)},
			{Start: at(20), End: at(30)},
		}, withoutSources(got))
	})

	t.Run("an empty shard annihilates only its own conjunction", func(t *testing.T) {
		got := aggregateConjunctionRanges(map[int]map[shardKey][]HintTimeRange{
			0: {
				shard0: {{Start: at(0), End: at(10)}},
				shard1: {},
			},
			1: {
				shard0: {{Start: at(20), End: at(30)}},
			},
		})
		require.Equal(t, []HintTimeRange{{Start: at(20), End: at(30)}}, withoutSources(got))
	})

	t.Run("overlapping conjunctions merge", func(t *testing.T) {
		got := aggregateConjunctionRanges(map[int]map[shardKey][]HintTimeRange{
			0: {shard0: {{Start: at(0), End: at(10)}}},
			1: {shard0: {{Start: at(5), End: at(15)}}},
		})
		require.Equal(t, []HintTimeRange{{Start: at(0), End: at(15)}}, withoutSources(got))
	})
}

// Once a conjunction misses a term on a reader, its other terms there are not
// looked up. The other conjunction keeps going.
func TestLoglineHintProvider_ExecuteQuery_PerConjunctionEarlyTermination(t *testing.T) {
	provider, counter, active, docRange := newCountingProvider(t, map[string][]uint32{
		"ABCDEF": {0}, "BCDEFG": {0}, "CDEFGH": {0},
	})
	var reasons []string
	provider.queryMultipleObserver = func(reason string, _ int) { reasons = append(reasons, reason) }

	// QRSTUVWXYZ has five terms, none of them in the index.
	needles := needle.Or(64, needle.Lit("QRSTUVWXYZ"), needle.Lit("ABCDEFGH"))
	byConj, err := provider.executeQuery(context.Background(), needles, active, NewQueryStats(), 6, 1)
	require.NoError(t, err)

	require.Equal(t, []HintTimeRange{docRange}, withoutSources(aggregateConjunctionRanges(byConj)))
	require.ElementsMatch(t, []string{"term_miss", "positive"}, reasons)
	// Three terms for ABCDEFGH and the first miss for QRSTUVWXYZ.
	require.Equal(t, 4, counter.total(counter.findTerm))
}

// A term shared by two conjunctions is looked up and read once.
func TestLoglineHintProvider_ExecuteQuery_ReadsSharedTermsOnce(t *testing.T) {
	provider, counter, active, docRange := newCountingProvider(t, map[string][]uint32{
		"ABCDEF": {0}, "BCDEFG": {0}, "CDEFGH": {0},
	})
	var reasons []string
	provider.queryMultipleObserver = func(reason string, _ int) { reasons = append(reasons, reason) }

	needles := needle.Or(64, needle.Lit("ABCDEFG"), needle.Lit("BCDEFGH"))
	byConj, err := provider.executeQuery(context.Background(), needles, active, NewQueryStats(), 6, 64)
	require.NoError(t, err)

	require.Equal(t, []HintTimeRange{docRange}, withoutSources(aggregateConjunctionRanges(byConj)))
	require.ElementsMatch(t, []string{"positive", "positive"}, reasons)
	require.Equal(t, map[string]int{"ABCDEF": 1, "BCDEFG": 1, "CDEFGH": 1}, counter.findTerm)
	require.Equal(t, 3, counter.total(counter.getBitmap))
}

// The bitmap of a shared term is ANDed into two conjunctions. A later AND in
// one of them must not change the other.
func TestLoglineHintProvider_ExecuteQuery_SharedBitmapIsNotAliased(t *testing.T) {
	for _, lit := range []string{"AAAAAA", "BBBBBB", "CCCCCC"} {
		terms, err := ExtractQueryNgrams(lit, 6, logline.CurrentVersion)
		require.NoError(t, err)
		require.Equal(t, []string{lit}, terms)
	}

	t0 := time.Date(2026, 2, 26, 10, 0, 0, 0, time.UTC)
	docs := []format.DocumentMetadata{
		{ID: 0, MinTimeUnix: t0.UnixMilli(), MaxTimeUnix: t0.Add(time.Second).UnixMilli()},
		{ID: 1, MinTimeUnix: t0.Add(2 * time.Second).UnixMilli(), MaxTimeUnix: t0.Add(3 * time.Second).UnixMilli()},
	}
	indexStore := newTestStore(t)
	writeTermTestIndex(t, indexStore, logline.CurrentVersion, "1111111111111111", docs, map[string][]uint32{
		"AAAAAA": {0, 1},
		"BBBBBB": {0},
		"CCCCCC": {1},
	}, 0, 0)
	provider, err := NewLoglineHintProvider(indexStore, 6, 0, 0, nil, log.NewNopLogger(), nil)
	require.NoError(t, err)

	// One worker, so AAAAAA is applied to both conjunctions first, then
	// BBBBBB to the first and CCCCCC to the second.
	needles := needle.Or(64, needle.AllOf("AAAAAA", "BBBBBB"), needle.AllOf("AAAAAA", "CCCCCC"))
	byConj, err := provider.executeQuery(context.Background(), needles, indexStore.Snapshot().Active(), NewQueryStats(), 6, 1)
	require.NoError(t, err)
	require.Equal(t, []HintTimeRange{
		{Start: t0, End: t0.Add(time.Second)},
		{Start: t0.Add(2 * time.Second), End: t0.Add(3 * time.Second)},
	}, withoutSources(aggregateConjunctionRanges(byConj)))
}

// newCountingProvider writes one unsharded index with one document holding
// postings, and returns a provider that counts lookups on it.
func newCountingProvider(t *testing.T, postings map[string][]uint32) (*LoglineHintProvider, *lookupCounter, []store.Meta, HintTimeRange) {
	t.Helper()
	t0 := time.Date(2026, 2, 26, 10, 0, 0, 0, time.UTC)
	docs := []format.DocumentMetadata{
		{ID: 0, MinTimeUnix: t0.UnixMilli(), MaxTimeUnix: t0.Add(time.Second).UnixMilli()},
	}
	indexStore := newTestStore(t)
	writeTermTestIndex(t, indexStore, logline.CurrentVersion, "1111111111111111", docs, postings, 0, 0)

	provider, err := NewLoglineHintProvider(indexStore, 6, 0, 0, nil, log.NewNopLogger(), nil)
	require.NoError(t, err)
	counter := &lookupCounter{findTerm: map[string]int{}, getBitmap: map[string]int{}}
	provider.wrapReader = func(r logline.Reader) logline.Reader {
		return &countingReader{Reader: r, counter: counter}
	}
	return provider, counter, indexStore.Snapshot().Active(), HintTimeRange{Start: t0, End: t0.Add(time.Second)}
}

type lookupCounter struct {
	mu        sync.Mutex
	findTerm  map[string]int
	getBitmap map[string]int
	// terms maps a term index back to its term, for getBitmap.
	terms map[int]string
}

func (c *lookupCounter) total(m map[string]int) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, v := range m {
		n += v
	}
	return n
}

type countingReader struct {
	logline.Reader
	counter *lookupCounter
}

func (r *countingReader) FindTerm(term string) (int, error) {
	idx, err := r.Reader.FindTerm(term)
	r.counter.mu.Lock()
	defer r.counter.mu.Unlock()
	r.counter.findTerm[term]++
	if r.counter.terms == nil {
		r.counter.terms = map[int]string{}
	}
	r.counter.terms[idx] = term
	return idx, err
}

func (r *countingReader) GetBitmap(termIndex int) (format.Bitmap, error) {
	r.counter.mu.Lock()
	r.counter.getBitmap[r.counter.terms[termIndex]]++
	r.counter.mu.Unlock()
	return r.Reader.GetBitmap(termIndex)
}

func withoutSources(ranges []HintTimeRange) []HintTimeRange {
	out := make([]HintTimeRange, len(ranges))
	for i, r := range ranges {
		out[i] = HintTimeRange{Start: r.Start, End: r.End}
	}
	return out
}
