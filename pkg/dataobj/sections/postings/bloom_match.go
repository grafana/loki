package postings

import (
	"bytes"
	"context"
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/bits-and-blooms/bloom/v3"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/xcap"
)

// Key uniquely identifies a postings bloom-match result by its source
// (object path, section index) tuple. Key is intentionally defined locally
// in the postings package — using metastore.SectionKey would create an
// import cycle since metastore depends (or will depend) on this package.
// Metastore converts Key
// to metastore.SectionKey at the dispatch site.
type Key struct {
	ObjectPath   string
	SectionIndex int64
}

// MatchSections applies AND-semantics bloom-filter membership testing across
// a slice of Equal label matchers and returns the set of (object_path,
// section_index) Keys whose bloom rows passed every matcher.
//
// batches must come from [Reader.ReadBloomRows] — MatchSections relies on
// the 4-column projection contract documented there (column positions: 0 =
// object_path utf8, 1 = section_index int64, 2 = column_name utf8, 3 =
// bloom_filter binary). Passing batches with a different projection is a
// programmer error and will return a typed-column-mismatch error.
//
// Matcher filtering: only [labels.MatchEqual] matchers participate; NotEqual,
// Regex, and NotRegex matchers are silently dropped before the row scan
// (matches today's metastore.index_sections_reader.go:86-91 behaviour).
// Callers that need regex matching must apply it on a separate code path —
// the postings inverted index cannot answer regex queries with bloom
// filters alone. When no Equal matchers remain after filtering, the result
// is an empty map (no sections can match).
//
// AND-semantics: a Key is included in the result only if every Equal
// matcher matched at least one bloom row for that Key (mirrors
// metastore.readMatchedSectionKeys lines 856-860: `len(matchedPredicates) ==
// len(r.predicates)`).
//
// LOAD-BEARING corrupted-bloom behaviour: when a bloom_filter byte payload
// fails to deserialise (either via an err return OR a panic in the
// underlying bitset.ReadFrom for implausibly large length prefixes),
// [bloomFilterMayContain] returns true (i.e. "may contain") rather than
// false. This preserves correctness — a corrupted bloom must NOT cause a
// false negative (a section silently dropped from the result when it might
// in fact contain the value). The err-branch is relocated from
// metastore.bloomFilterMayContain (index_sections_reader.go:866-868); the
// panic-recovery is a hardening over the legacy verbatim, since
// bitset.ReadFrom can panic on hostile input (makeslice OOM). A future
// follow-up may add a debug log / metric; this preserves the
// silent-true contract.
func MatchSections(ctx context.Context, batches []arrow.RecordBatch, matchers []*labels.Matcher) (map[Key]struct{}, error) {
	// Filter to MatchEqual matchers only (same shape as
	// metastore.index_sections_reader.go:86-91). Other matcher types fall
	// through to callers that handle regex / not-equal on separate paths.
	equalMatchers := make([]*labels.Matcher, 0, len(matchers))
	for _, m := range matchers {
		if m != nil && m.Type == labels.MatchEqual {
			equalMatchers = append(equalMatchers, m)
		}
	}
	if len(equalMatchers) == 0 {
		return map[Key]struct{}{}, nil
	}

	ctx, span := xcap.StartSpan(ctx, tracer, "postings.MatchSections")
	defer span.End()
	// Accumulate bloom-deserialize failures locally and record once at the
	// end — keeps the inner per-row loop free of region lookups while still
	// surfacing corruption via xcap.
	var bloomDeserializeFailures int64
	defer func() {
		if bloomDeserializeFailures > 0 {
			xcap.RegionFromContext(ctx).Record(
				xcap.StatPostingsBloomDeserializeFailures.Observe(bloomDeserializeFailures),
			)
		}
	}()

	// predicateIndexesByName indexes Equal matchers by Name so multiple
	// matchers on the same column are all tested per bloom row (verbatim
	// shape from metastore.readMatchedSectionKeys:781-784).
	predicateIndexesByName := make(map[string][]int, len(equalMatchers))
	for i, m := range equalMatchers {
		predicateIndexesByName[m.Name] = append(predicateIndexesByName[m.Name], i)
	}

	// sectionMatches[Key] is the set of equalMatchers indexes that matched
	// at least one bloom row for that Key. AND-semantics: only Keys whose
	// set size equals len(equalMatchers) are kept (line 857 invariant).
	sectionMatches := make(map[Key]map[int]struct{})

	for _, rec := range batches {
		if rec == nil || rec.NumRows() == 0 {
			continue
		}

		// Column-position contract: ReadBloomRows projects exactly
		// [object_path, section_index, column_name, bloom_filter] in that
		// order. Type-assertions are guarded so a projection drift surfaces
		// as a typed error rather than a panic.
		pathCol, ok := rec.Column(0).(*array.String)
		if !ok {
			return nil, fmt.Errorf("ReadBloomRows projection violated: column 0 wrong type %T", rec.Column(0))
		}
		sectionCol, ok := rec.Column(1).(*array.Int64)
		if !ok {
			return nil, fmt.Errorf("ReadBloomRows projection violated: column 1 wrong type %T", rec.Column(1))
		}
		columnNameCol, ok := rec.Column(2).(*array.String)
		if !ok {
			return nil, fmt.Errorf("ReadBloomRows projection violated: column 2 wrong type %T", rec.Column(2))
		}
		bloomCol, ok := rec.Column(3).(*array.Binary)
		if !ok {
			return nil, fmt.Errorf("ReadBloomRows projection violated: column 3 wrong type %T", rec.Column(3))
		}

		for i := 0; i < int(rec.NumRows()); i++ {
			if pathCol.IsNull(i) || sectionCol.IsNull(i) || columnNameCol.IsNull(i) || bloomCol.IsNull(i) {
				continue
			}

			predicateIndexes := predicateIndexesByName[columnNameCol.Value(i)]
			if len(predicateIndexes) == 0 {
				continue
			}

			sectionKey := Key{
				ObjectPath:   pathCol.Value(i),
				SectionIndex: sectionCol.Value(i),
			}

			for _, predicateIndex := range predicateIndexes {
				mayContain, deserializeFailed := bloomFilterMayContainObserved(
					bloomCol.Value(i),
					equalMatchers[predicateIndex].Value,
				)
				if deserializeFailed {
					bloomDeserializeFailures++
				}
				if !mayContain {
					continue
				}
				matchedPredicates := sectionMatches[sectionKey]
				if matchedPredicates == nil {
					matchedPredicates = make(map[int]struct{}, len(equalMatchers))
					sectionMatches[sectionKey] = matchedPredicates
				}
				matchedPredicates[predicateIndex] = struct{}{}
			}
		}
	}

	// AND across predicates (verbatim from metastore.readMatchedSectionKeys
	// lines 856-860): a Key is kept only if every Equal matcher matched at
	// least one bloom row for that Key.
	matchedSectionKeys := make(map[Key]struct{})
	for sectionKey, matchedPredicates := range sectionMatches {
		if len(matchedPredicates) == len(equalMatchers) {
			matchedSectionKeys[sectionKey] = struct{}{}
		}
	}
	return matchedSectionKeys, nil
}

// bloomFilterMayContain returns true if value MAY be present in the bloom
// filter encoded by bloomBytes, and false if it is definitely absent. On
// deserialisation failure it returns true (NOT false) — a corrupted bloom
// must not cause a false negative; the caller will fall through to the
// authoritative row-level evaluation. Relocated from
// metastore.bloomFilterMayContain (index_sections_reader.go:864-870); see
// MatchSections doc-comment for the load-bearing rationale.
//
// Hardening over the legacy verbatim: bloom.ReadFrom (via bitset.ReadFrom)
// can panic with "makeslice: len out of range" when the decoded length
// prefix is implausibly large. The legacy metastore helper would crash the
// goroutine in that case; we recover the panic and return true to preserve
// the "no false negative" contract under hostile / corrupted input.
func bloomFilterMayContain(bloomBytes []byte, value string) (mayContain bool) {
	mayContain, _ = bloomFilterMayContainObserved(bloomBytes, value)
	return mayContain
}

// bloomFilterMayContainObserved is the observable variant of
// [bloomFilterMayContain]. It returns the membership result AND a
// boolean indicating whether the bloom payload failed to deserialise
// (either via err or a recovered panic). Callers with a context in scope
// (e.g. [MatchSections]) use this to increment the
// xcap.StatPostingsBloomDeserializeFailures counter so corrupted bloom
// payloads become observable in flight without breaking the
// no-false-negative contract.
//
// Internal helper — callers without an observability context should
// continue to use [bloomFilterMayContain]; the deserializeFailed return
// is otherwise discarded.
func bloomFilterMayContainObserved(bloomBytes []byte, value string) (mayContain, deserializeFailed bool) {
	defer func() {
		if r := recover(); r != nil {
			// LOAD-BEARING: corrupted bloom payload that panics on
			// ReadFrom must yield "may contain" (true), not propagate.
			mayContain = true
			deserializeFailed = true
		}
	}()

	var bf bloom.BloomFilter
	if _, err := bf.ReadFrom(bytes.NewReader(bloomBytes)); err != nil {
		return true, true
	}
	return bf.TestString(value), false
}
