package postings

import "sort"

// sortLabelEntries sorts label entries by
// [ColumnName, LabelValue, MinTimestamp, MaxTimestamp, ObjectPath, SectionIndex].
func sortLabelEntries(entries []LabelEntry) {
	sort.Slice(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.ColumnName != b.ColumnName {
			return a.ColumnName < b.ColumnName
		}
		if a.LabelValue != b.LabelValue {
			return a.LabelValue < b.LabelValue
		}
		if a.MinTimestamp != b.MinTimestamp {
			return a.MinTimestamp < b.MinTimestamp
		}
		if a.MaxTimestamp != b.MaxTimestamp {
			return a.MaxTimestamp < b.MaxTimestamp
		}
		if a.ObjectPath != b.ObjectPath {
			return a.ObjectPath < b.ObjectPath
		}
		return a.SectionIndex < b.SectionIndex
	})
}

// sortBloomEntries sorts bloom entries by
// [ColumnName, MinTimestamp, MaxTimestamp, ObjectPath, SectionIndex].
func sortBloomEntries(entries []BloomEntry) {
	sort.Slice(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.ColumnName != b.ColumnName {
			return a.ColumnName < b.ColumnName
		}
		if a.MinTimestamp != b.MinTimestamp {
			return a.MinTimestamp < b.MinTimestamp
		}
		if a.MaxTimestamp != b.MaxTimestamp {
			return a.MaxTimestamp < b.MaxTimestamp
		}
		if a.ObjectPath != b.ObjectPath {
			return a.ObjectPath < b.ObjectPath
		}
		return a.SectionIndex < b.SectionIndex
	})
}
