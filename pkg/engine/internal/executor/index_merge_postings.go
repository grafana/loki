package executor

import (
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
)

// comparePostingsRow orders two postings rows in the order they are physically
// written into, and read back from, a postings section:
// (Kind, ColumnName, LabelValue, MinTimestamp, MaxTimestamp, ObjectPath,
// SectionIndex).
func comparePostingsRow(a, b postings.Row) int {
	if a.Kind != b.Kind {
		if a.Kind < b.Kind {
			return -1
		}
		return 1
	}
	if a.ColumnName != b.ColumnName {
		if a.ColumnName < b.ColumnName {
			return -1
		}
		return 1
	}
	if a.LabelValue != b.LabelValue {
		if a.LabelValue < b.LabelValue {
			return -1
		}
		return 1
	}
	if a.MinTimestamp != b.MinTimestamp {
		if a.MinTimestamp < b.MinTimestamp {
			return -1
		}
		return 1
	}
	if a.MaxTimestamp != b.MaxTimestamp {
		if a.MaxTimestamp < b.MaxTimestamp {
			return -1
		}
		return 1
	}
	if a.ObjectPath != b.ObjectPath {
		if a.ObjectPath < b.ObjectPath {
			return -1
		}
		return 1
	}
	if a.SectionIndex != b.SectionIndex {
		if a.SectionIndex < b.SectionIndex {
			return -1
		}
		return 1
	}
	return 0
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
