package postings

import "sort"

// sortLabelEntries sorts label entries into the section's physical write order.
// It delegates to [CompareRows] so the encoder and any merge over sections stay
// in lockstep. All entries share Kind == KindLabel here, so the Kind term is a
// no-op; the remaining fields decide the order.
func sortLabelEntries(entries []LabelEntry) {
	sort.Slice(entries, func(i, j int) bool {
		return CompareRows(entries[i].Row(), entries[j].Row()) < 0
	})
}

// sortBloomEntries sorts bloom entries into the section's physical write order.
// It delegates to [CompareRows] so the encoder and any merge over sections stay
// in lockstep. All entries share Kind == KindBloom here, so both the Kind and
// LabelValue terms are no-ops; the remaining fields decide the order.
func sortBloomEntries(entries []BloomEntry) {
	sort.Slice(entries, func(i, j int) bool {
		return CompareRows(entries[i].Row(), entries[j].Row()) < 0
	})
}
