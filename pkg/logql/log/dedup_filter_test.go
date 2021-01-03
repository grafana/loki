package log

import (
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/stretchr/testify/require"
	"sort"
	"testing"
)

type entry struct {
	line string
	lbs  *labels.Labels
}

var (
	labelSetA = labels.Labels{
		{Name: "namespace", Value: "dev"},
		{Name: "cluster", Value: "us-central1"},
		{Name: "role", Value: "testing"},
	}
	labelSetB = labels.Labels{
		{Name: "namespace", Value: "prod"},
		{Name: "cluster", Value: "us-central1"},
	}
	labelSetC = labels.Labels{
		{Name: "namespace", Value: "dev"},
		{Name: "cluster", Value: "us-central1"},
		{Name: "role", Value: "qa"},
	}

	refLineA = "this is reference line A"
	refLineB = "this is reference line B"
	refLineC = "this is reference line C"
	refLineD = "this is reference line D"
	refLineE = "this is reference line E"
	refLineF = "this is reference line F"
	refLineG = "this is reference line G"

	labelEntryMap = map[*labels.Labels][]string{
		&labelSetA: {refLineA, refLineC, refLineD, refLineF},
		&labelSetB: {refLineB, refLineE},
		&labelSetC: {refLineG},
	}

	entries = buildEntries(labelEntryMap)
)

func Test_deduplication(t *testing.T) {

	tests := []struct {
		name         string
		wantLines    []string
		entries      []entry
		labelFilters []string
		inverted     bool
	}{
		{
			name: "dedup by (namespace)",
			wantLines: []string{
				refLineA, // first for labelSetA (labelSetC has same "namespace")
				refLineB, // first for labelSetB
			},
			entries:      entries,
			labelFilters: []string{"namespace"},
			inverted:     false,
		},
		{
			name: "dedup by (role)",
			wantLines: append(
				[]string{refLineA, refLineG},  // only first entries with different "role" label
				labelEntryMap[&labelSetB]...), // include all of labelSetB since it has no "role" label
			entries:      entries,
			labelFilters: []string{"role"},
			inverted:     false,
		},
		{
			name: "dedup without (role)",
			wantLines: append(
				[]string{refLineA, refLineG},
				labelEntryMap[&labelSetB]...,
			),
			entries:      entries,
			labelFilters: []string{"role"},
			inverted:     false,
		},
	}

	for _, tt := range tests {
		d := NewLineDedupFilter(tt.labelFilters, tt.inverted)

		t.Run(tt.name, func(t *testing.T) {
			var outLines []string

			for _, entry := range tt.entries {
				b := NewBaseLabelsBuilder().ForLabels(*entry.lbs, entry.lbs.Hash())
				b.Reset()

				//fmt.Printf("%+v | %s\n", entry.lbs, entry.line)
				if line, ok := d.Process([]byte(entry.line), b); ok {
					outLines = append(outLines, string(line))
				}
			}

			sort.Strings(outLines)
			sort.Strings(tt.wantLines)
			require.Equal(t, tt.wantLines, outLines)
		})
	}
}

func buildEntries(mapping map[*labels.Labels][]string) []entry {
	var entries []entry

	for labelSet, lines := range mapping {
		for _, line := range lines {
			entries = append(entries, entry{line: line, lbs: labelSet})
		}
	}

	return entries
}
