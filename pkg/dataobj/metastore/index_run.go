package metastore

import (
	"fmt"
	"slices"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
)

// indexRun is the index objects of one run, which a query reads as one index.
// An index object of no run is a run of its own.
//
// A run splits the postings of one logs section across its index objects. A
// stream matches only if each matcher matches one of its labels, and the labels
// can sit in different index objects. So a query must read all index objects of
// a run with one stream selector.
type indexRun struct {
	// entries is never empty. Its entries share TableOfContentsPath and
	// RunID.
	entries []IndexEntry
}

// indexRuns groups entries into index runs. Each entry of no run is a run of
// its own. The runs and their entries keep the order of entries. The order
// does not matter to a query, because the stream selector unions the postings
// of every index object of a run.
func indexRuns(entries []IndexEntry) []indexRun {
	type runKey struct {
		tocPath string
		runID   indexpointers.RunID
	}

	var (
		out  []indexRun
		runs = make(map[runKey]int)
	)
	for _, entry := range entries {
		if entry.RunID == 0 {
			out = append(out, indexRun{entries: []IndexEntry{entry}})
			continue
		}
		key := runKey{tocPath: entry.TableOfContentsPath, runID: entry.RunID}
		if i, ok := runs[key]; ok {
			out[i].entries = append(out[i].entries, entry)
			continue
		}
		runs[key] = len(out)
		out = append(out, indexRun{entries: []IndexEntry{entry}})
	}
	return out
}

// String describes r for logs.
func (r indexRun) String() string {
	first := r.entries[0]
	if first.RunID == 0 {
		return first.Path
	}
	return fmt.Sprintf("run %d of %s (%d index objects)", first.RunID, first.TableOfContentsPath, len(r.entries))
}

// withPostingsFor returns r with only the index objects that can hold a
// posting that matchers and predicates read. ok is false if no index object
// can.
//
// It keeps an index object if its posting key range overlaps a column that a
// matcher or predicate names, whatever the value. So it keeps every index
// object that the stream selector reads a row from:
//
//   - A stream matcher reads the label postings of its name.
//   - An equality predicate reads the bloom postings and the label postings of
//     its name.
//   - Other predicates read no postings.
//
// An index object with unknown posting key bounds can hold any posting, so it
// stays.
func (r indexRun) withPostingsFor(matchers, predicates []*labels.Matcher) (_ indexRun, ok bool) {
	type column struct {
		kind postings.PostingKind
		name string
	}

	var columns []column
	for _, m := range matchers {
		columns = append(columns, column{kind: postings.KindLabel, name: m.Name})
	}
	for _, p := range equalityPredicates(predicates) {
		columns = append(columns,
			column{kind: postings.KindBloom, name: p.Name},
			column{kind: postings.KindLabel, name: p.Name},
		)
	}

	kept := make([]IndexEntry, 0, len(r.entries))
	for _, entry := range r.entries {
		if slices.ContainsFunc(columns, func(c column) bool { return entry.PostingKeys.OverlapsColumn(c.kind, c.name) }) {
			kept = append(kept, entry)
		}
	}
	if len(kept) == 0 {
		return indexRun{}, false
	}
	return indexRun{entries: kept}, true
}

// equalityPredicates returns the equality predicates of predicates, the only
// predicates that the stream selector reads postings for. A bloom filter
// proves only that a value is absent, so only an equality can prune a section.
func equalityPredicates(predicates []*labels.Matcher) []*labels.Matcher {
	var equal []*labels.Matcher
	for _, p := range predicates {
		if p != nil && p.Type == labels.MatchEqual {
			equal = append(equal, p)
		}
	}
	return equal
}
