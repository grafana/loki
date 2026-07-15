package executor

import (
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
)

// comparePostingsRow orders two postings rows using the postings package's
// canonical physical order. Delegating keeps the K-way merge in lockstep with
// the section encoder's write order, which is the single source of truth.
func comparePostingsRow(a, b postings.Row) int {
	return postings.CompareRows(a, b)
}

// samePostingsKey reports whether two rows share the postings builder's identity
// key (Kind, ObjectPath, SectionIndex, ColumnName, LabelValue) — the key the
// index-merge dedup collapses on. It deliberately ignores timestamps and
// bitmaps: two source objects referencing the same physical section/column/label
// are the same posting even if those non-key fields differ, and the builder's
// map is keyed on exactly these fields, so a match here is what would collide.
// This is separate from comparePostingsRow's ordering because ordering must
// track physical (timestamp-bearing) storage order while dedup must not.
func samePostingsKey(a, b postings.Row) bool {
	return a.Kind == b.Kind &&
		a.ObjectPath == b.ObjectPath &&
		a.SectionIndex == b.SectionIndex &&
		a.ColumnName == b.ColumnName &&
		a.LabelValue == b.LabelValue
}
