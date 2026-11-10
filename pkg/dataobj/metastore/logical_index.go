package metastore

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
)

// logicalIndex is the index objects that a query reads as one index: the index
// objects of one run, or one index object of no run.
//
// A run splits the postings of one logs section across its index objects. A
// stream matches only if each matcher matches one of its labels, and the labels
// can sit in different index objects. So a query must read all index objects of
// a run with one stream selector.
type logicalIndex struct {
	// entries is never empty. Its entries share TableOfContentsPath and
	// RunID.
	entries []IndexEntry
}

// logicalIndexes groups entries into logical indexes. Each entry of no run is a
// logical index of its own. Entries of one run form one logical index. The
// result is in the order of the first path of each logical index.
func logicalIndexes(entries []IndexEntry) []logicalIndex {
	type runKey struct {
		tocPath string
		runID   indexpointers.RunID
	}

	var (
		out  []logicalIndex
		runs = make(map[runKey]int)
	)
	for _, entry := range entries {
		if entry.RunID == 0 {
			out = append(out, logicalIndex{entries: []IndexEntry{entry}})
			continue
		}
		key := runKey{tocPath: entry.TableOfContentsPath, runID: entry.RunID}
		if i, ok := runs[key]; ok {
			out[i].entries = append(out[i].entries, entry)
			continue
		}
		runs[key] = len(out)
		out = append(out, logicalIndex{entries: []IndexEntry{entry}})
	}

	for _, li := range out {
		slices.SortFunc(li.entries, func(a, b IndexEntry) int { return cmp.Compare(a.Path, b.Path) })
	}
	slices.SortFunc(out, func(a, b logicalIndex) int { return cmp.Compare(a.entries[0].Path, b.entries[0].Path) })
	return out
}

// String describes li for logs.
func (li logicalIndex) String() string {
	first := li.entries[0]
	if first.RunID == 0 {
		return first.Path
	}
	return fmt.Sprintf("run %d of %s (%d index objects)", first.RunID, first.TableOfContentsPath, len(li.entries))
}

// withPostingsFor returns li with only the index objects that can hold a
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
func (li logicalIndex) withPostingsFor(matchers, predicates []*labels.Matcher) (_ logicalIndex, ok bool) {
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

	kept := make([]IndexEntry, 0, len(li.entries))
	for _, entry := range li.entries {
		if slices.ContainsFunc(columns, func(c column) bool { return entry.PostingKeys.OverlapsColumn(c.kind, c.name) }) {
			kept = append(kept, entry)
		}
	}
	if len(kept) == 0 {
		return logicalIndex{}, false
	}
	return logicalIndex{entries: kept}, true
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
