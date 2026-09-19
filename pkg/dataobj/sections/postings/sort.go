package postings

import "sort"

// sortLabelEntries sorts label entries into the section's physical write order.
// It delegates to [CompareRows] so the encoder and any merge over sections stay
// in lockstep.
func sortLabelEntries(entries []LabelEntry) {
	sort.Slice(entries, func(i, j int) bool {
		return CompareRows(entries[i].Row(), entries[j].Row()) < 0
	})
}

// sortBloomEntries sorts bloom entries into the section's physical write order.
// It delegates to [CompareRows] so the encoder and any merge over sections stay
// in lockstep.
func sortBloomEntries(entries []BloomEntry) {
	sort.Slice(entries, func(i, j int) bool {
		return CompareRows(entries[i].Row(), entries[j].Row()) < 0
	})
}
