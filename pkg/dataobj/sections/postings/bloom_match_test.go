package postings_test

import (
	"errors"
	"io"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
)

// TestMatchSections_AND_Semantics is the canonical anchor for the AND-across-
// matchers contract (mirrors metastore.readMatchedSectionKeys line 857
// invariant: `len(matchedPredicates) == len(r.predicates)`). Fixture: three
// (objectPath, sectionIndex) pairs; each pair gets two bloom columns ("env"
// and "app") with controlled values. Only the section whose env bloom
// contains "prod" AND app bloom contains "foo" should appear in the result.
func TestMatchSections_AND_Semantics(t *testing.T) {
	fx := buildBloomFixture(t, []bloomFixtureEntry{
		// Section A: env={prod}, app={foo} — matches BOTH (kept).
		{objectPath: "/objA", sectionIndex: 0, columnName: "env", values: []string{"prod"}},
		{objectPath: "/objA", sectionIndex: 0, columnName: "app", values: []string{"foo"}},
		// Section B: env={prod}, app={baz} — matches only env (dropped).
		{objectPath: "/objB", sectionIndex: 0, columnName: "env", values: []string{"prod"}},
		{objectPath: "/objB", sectionIndex: 0, columnName: "app", values: []string{"baz"}},
		// Section C: env={dev}, app={foo} — matches only app (dropped).
		{objectPath: "/objC", sectionIndex: 0, columnName: "env", values: []string{"dev"}},
		{objectPath: "/objC", sectionIndex: 0, columnName: "app", values: []string{"foo"}},
	})

	batches := readAllBloomBatches(t, fx.sec)

	result, err := postings.MatchSections(t.Context(), batches, []*labels.Matcher{
		equalMatcher(t, "env", "prod"),
		equalMatcher(t, "app", "foo"),
	})
	require.NoError(t, err)

	require.Len(t, result, 1, "exactly one section should match both env=prod AND app=foo")
	_, ok := result[postings.Key{ObjectPath: "/objA", SectionIndex: 0}]
	require.True(t, ok, "section A (/objA, 0) should be the sole match")
}

// TestMatchSections_EqualMatcherOnly_FilterApplied asserts that non-Equal
// matchers (Regex / NotEqual / NotRegex) are silently filtered out — only
// Equal matchers participate in the bloom-membership test. Same observable
// behaviour as metastore.index_sections_reader.go:86-91.
func TestMatchSections_EqualMatcherOnly_FilterApplied(t *testing.T) {
	fx := buildBloomFixture(t, []bloomFixtureEntry{
		{objectPath: "/obj", sectionIndex: 0, columnName: "env", values: []string{"prod"}},
	})

	batches := readAllBloomBatches(t, fx.sec)

	// Pass an Equal matcher AND a Regex matcher. The Regex matcher is
	// silently dropped — the result should be the same as if only the
	// Equal matcher had been passed.
	result, err := postings.MatchSections(t.Context(), batches, []*labels.Matcher{
		equalMatcher(t, "env", "prod"),
		regexMatcher(t, "app", ".*"),
	})
	require.NoError(t, err)

	require.Len(t, result, 1, "Equal matcher alone should match the section; Regex matcher is filtered out")
	_, ok := result[postings.Key{ObjectPath: "/obj", SectionIndex: 0}]
	require.True(t, ok)
}

// ---------------------------------------------------------------------------
// Test helpers (postings_test scope)
// ---------------------------------------------------------------------------

// bloomFixtureEntry describes one bloom column to register in a fixture.
type bloomFixtureEntry struct {
	objectPath   string
	sectionIndex int64
	columnName   string
	// values are the strings added to the bloom filter under this column.
	// They are also used as the bloom-row "value" via ObserveBloomPosting —
	// the resulting BloomFilter.Test will return true for each.
	values []string
}

type bloomFixture struct {
	sec *postings.Section
}

// buildBloomFixture builds a single dataobj.Object containing a postings
// section with one KindBloom posting per fixture entry. Each entry is a
// (objectPath, sectionIndex, columnName) tuple plus a list of values to
// observe — bloomAggregator aggregates all values for the same key into
// one bloom row whose bloom filter Test-positives all listed values.
//
// Uses Open (not OpenWithObject) — ReadBloomRows does not require a parent
// back-pointer, and avoiding OpenWithObject keeps the fixture minimal.
func buildBloomFixture(t *testing.T, entries []bloomFixtureEntry) bloomFixture {
	t.Helper()

	pb := postings.NewBuilder(nil, 0, 0)

	ts := time.Unix(0, 1000).UTC()
	for _, e := range entries {
		pb.PrepareBloomColumn(e.objectPath, e.sectionIndex, e.columnName, uint(len(e.values)))
		for streamIdx, v := range e.values {
			require.NoError(t, pb.ObserveBloomPosting(postings.BloomObservation{
				ObjectPath:       e.objectPath,
				SectionIndex:     e.sectionIndex,
				ColumnName:       e.columnName,
				Value:            v,
				StreamID:         int64(streamIdx + 1),
				Timestamp:        ts,
				UncompressedSize: 1,
			}))
		}
	}

	objBuilder := dataobj.NewBuilder(nil)
	require.NoError(t, objBuilder.Append(pb))
	obj, closer, err := objBuilder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })

	var fx bloomFixture
	for _, sec := range obj.Sections() {
		if !postings.CheckSection(sec) {
			continue
		}
		s, err := postings.Open(t.Context(), sec)
		require.NoError(t, err)
		fx.sec = s
		break
	}
	require.NotNil(t, fx.sec, "postings section missing from fixture")
	return fx
}

// readAllBloomBatches opens a postings.Reader over sec and drains
// ReadBloomRows in a loop until io.EOF. ReadBloomRows returns a
// single accumulated batch (same shape as ReadPointers) — the loop is
// still present so a future streaming variant doesn't break the helper.
func readAllBloomBatches(t *testing.T, sec *postings.Section) []arrow.RecordBatch {
	t.Helper()

	r := postings.NewReader(postings.ReaderOptions{
		Columns:   sec.Columns(),
		Allocator: memory.DefaultAllocator,
	})
	require.NoError(t, r.Open(t.Context()))
	t.Cleanup(func() { _ = r.Close() })

	var batches []arrow.RecordBatch
	for {
		rb, err := r.ReadBloomRows(t.Context())
		if rb != nil && rb.NumRows() > 0 {
			batches = append(batches, rb)
		}
		if err == nil {
			// single-batch contract — one call returns the full
			// result with err==nil. Break to avoid the looping that would
			// be needed for a future streaming variant.
			break
		}
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
	}
	return batches
}

func equalMatcher(t *testing.T, name, value string) *labels.Matcher {
	t.Helper()
	m, err := labels.NewMatcher(labels.MatchEqual, name, value)
	require.NoError(t, err)
	return m
}

func regexMatcher(t *testing.T, name, value string) *labels.Matcher {
	t.Helper()
	m, err := labels.NewMatcher(labels.MatchRegexp, name, value)
	require.NoError(t, err)
	return m
}
