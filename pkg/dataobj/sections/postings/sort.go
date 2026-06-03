package postings

import "sort"

// sortLabelEntries sorts label entries by
// [objectPath, sectionIndex, columnName, labelValue]. The sort is stable so
// callers that rely on insertion order for equal keys keep that order.
func sortLabelEntries(entries []LabelEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.ObjectPath != b.ObjectPath {
			return a.ObjectPath < b.ObjectPath
		}
		if a.SectionIndex != b.SectionIndex {
			return a.SectionIndex < b.SectionIndex
		}
		if a.ColumnName != b.ColumnName {
			return a.ColumnName < b.ColumnName
		}
		return a.LabelValue < b.LabelValue
	})
}

// sortBloomEntries sorts bloom entries by
// [objectPath, sectionIndex, columnName]. The sort is stable so callers that
// rely on insertion order for equal keys keep that order.
func sortBloomEntries(entries []BloomEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.ObjectPath != b.ObjectPath {
			return a.ObjectPath < b.ObjectPath
		}
		if a.SectionIndex != b.SectionIndex {
			return a.SectionIndex < b.SectionIndex
		}
		return a.ColumnName < b.ColumnName
	})
}
