package executor

import (
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
)

// comparePostingsRow compares two postings rows using the sort order:
// (Kind, ObjectPath, SectionIndex, ColumnName, LabelValue).
func comparePostingsRow(a, b postings.Row) int {
	if a.Kind != b.Kind {
		if a.Kind < b.Kind {
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
	return 0
}
