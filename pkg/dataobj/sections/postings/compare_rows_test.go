package postings

import (
	"math/rand"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCompareRowsMatchesWriteSideSort is the drift guardrail: it proves that
// ordering everything with the merge comparator ([CompareRows]) reproduces the
// exact sequence the section encoder writes to disk. The encoder sorts bloom
// and label entries independently and Flush encodes blooms before labels, so a
// single [CompareRows] pass over the union must yield blooms-then-labels in the
// same relative order. If the two ever diverge, the K-way merge in the executor
// would dedup against the wrong neighbor — the bug this refactor exists to
// prevent.
func TestCompareRowsMatchesWriteSideSort(t *testing.T) {
	rows := diverseRows()
	rand.New(rand.NewSource(1)).Shuffle(len(rows), func(i, j int) {
		rows[i], rows[j] = rows[j], rows[i]
	})

	require.Equal(t, writeSideOrder(rows), mergeOrder(rows))
}

// writeSideOrder reproduces the encoder's physical layout: blooms sorted by
// sortBloomEntries, then labels sorted by sortLabelEntries, blooms first.
func writeSideOrder(rows []Row) []Row {
	var blooms []BloomEntry
	var labels []LabelEntry
	for _, r := range rows {
		switch r.Kind {
		case KindBloom:
			blooms = append(blooms, r.BloomEntry())
		case KindLabel:
			labels = append(labels, r.LabelEntry())
		}
	}
	sortBloomEntries(blooms)
	sortLabelEntries(labels)

	out := make([]Row, 0, len(rows))
	for _, e := range blooms {
		out = append(out, e.Row())
	}
	for _, e := range labels {
		out = append(out, e.Row())
	}
	return out
}

// mergeOrder is what the K-way merge produces: a single ordering by CompareRows.
func mergeOrder(rows []Row) []Row {
	out := slices.Clone(rows)
	slices.SortStableFunc(out, CompareRows)
	return out
}

// diverseRows generates canonical rows covering every combination of the
// ordering fields. Bloom rows carry no label value and label rows carry no
// bloom filter so each row round-trips through its entry type unchanged, which
// lets the guardrail assert full equality rather than an order-only key. Every
// combination is unique, so there are no ties for the non-stable write-side
// sorts to resolve ambiguously.
func diverseRows() []Row {
	columnNames := []string{"", "col_a", "col_b"}
	labelValues := []string{"", "alpha", "beta"}
	timestamps := []int64{0, 1, 2}
	objectPaths := []string{"", "obj_a", "obj_b"}
	sectionIndices := []int64{0, 1}

	var rows []Row
	for _, col := range columnNames {
		for _, minTS := range timestamps {
			for _, maxTS := range timestamps {
				for _, path := range objectPaths {
					for _, sec := range sectionIndices {
						rows = append(rows, Row{
							Kind:         KindBloom,
							ColumnName:   col,
							MinTimestamp: minTS,
							MaxTimestamp: maxTS,
							ObjectPath:   path,
							SectionIndex: sec,
						})
						for _, val := range labelValues {
							rows = append(rows, Row{
								Kind:         KindLabel,
								ColumnName:   col,
								LabelValue:   val,
								MinTimestamp: minTS,
								MaxTimestamp: maxTS,
								ObjectPath:   path,
								SectionIndex: sec,
							})
						}
					}
				}
			}
		}
	}
	return rows
}
