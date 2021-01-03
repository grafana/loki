package log

import (
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/stretchr/testify/require"
	"math/rand"
	"testing"
)

var (
	devLabels = &labels.Labels{
		{Name: "namespace", Value: "dev"},
		{Name: "cluster", Value: "us-central1"},
		{Name: "role", Value: "dev"},
	}
	qaLabels = &labels.Labels{
		{Name: "namespace", Value: "dev"},
		{Name: "cluster", Value: "us-central1"},
		{Name: "role", Value: "qa"},
	}
	prodLabels = &labels.Labels{
		{Name: "namespace", Value: "prod"},
		{Name: "cluster", Value: "us-central1"},
	}

	data = &testData{
		Dev:  newLineLabelMap(devLabels, 5),
		QA:   newLineLabelMap(qaLabels, 2),
		Prod: newLineLabelMap(prodLabels, 5),
	}

	entries = data.allEntries()
)

func Test_Deduplication(t *testing.T) {

	tests := []struct {
		name         string
		wantLines    [][]byte
		labelFilters []string
		inverted     bool
	}{
		{
			name: "dedup by (namespace)",
			labelFilters: []string{"namespace"},
			inverted:     false,
			wantLines: [][]byte{
				data.Dev.first(),  // first for devLabels (qaLabels has same "namespace")
				data.Prod.first(), // first for prodLabels
			},
		},
		{
			name: "dedup by (role)",
			labelFilters: []string{"role"},
			inverted:     false,
			wantLines: append(
				[][]byte{data.Dev.first(), data.QA.first()}, // only first entries with different "role" label
				data.Prod.all()...), 						 // include all of prodLabels since it has no "role" label
		},
		{
			name: "dedup without (role)",
			labelFilters: []string{"role"},
			inverted:     true,
			// dedup without (x) will dedup by all labels that are not "x",
			// so this will dedup by "namespace" and "cluster"
			wantLines:    [][]byte{data.Dev.first(), data.Prod.first()},
		},
		{
			name: "dedup without (unknown)",
			labelFilters: []string{"unknown"},
			inverted:     true,
			// dedup without a label missing to all entries will result in a dedup by all present labels
			wantLines:    [][]byte{data.Dev.first(), data.QA.first(), data.Prod.first()},
		},
		{
			name: "dedup by (unknown)",
			labelFilters: []string{"unknown"},
			inverted:     false,
			// dedup by a label missing to all entries is effectively a noop
			wantLines:    data.allLines(),
		},
		{
			name: "dedup by (cluster, namespace)",
			labelFilters: []string{"cluster", "namespace"},
			inverted:     false,
			// dedup by multiple labels
			wantLines:    [][]byte{data.Dev.first(), data.Prod.first()},
		},
		{
			name: "dedup without (role, namespace)",
			labelFilters: []string{"role", "namespace"},
			inverted:     true,
			// dedup without multiple labels
			// - all lines have the same "cluster" label so only the first is returned
			wantLines:    [][]byte{data.Dev.first()},
		},
		{
			name: "dedup by ()",
			labelFilters: nil,
			inverted:     false,
			// dedup without labels is effectively a noop
			wantLines:    data.allLines(),
		},
		{
			name: "dedup without ()",
			labelFilters: nil,
			inverted:     true,
			// dedup without labels will result in a dedup by all present labels
			wantLines:    [][]byte{data.Dev.first(), data.QA.first(), data.Prod.first()},
		},
	}

	for _, tt := range tests {
		d := NewLineDedupFilter(tt.labelFilters, tt.inverted)

		t.Run(tt.name, func(t *testing.T) {
			var outLines [][]byte

			for _, entry := range entries {
				b := NewBaseLabelsBuilder().ForLabels(*entry.lbs, entry.lbs.Hash())
				b.Reset()

				if line, ok := d.Process(entry.line, b); ok {
					outLines = append(outLines, line)
				}
			}

			require.ElementsMatch(t, tt.wantLines, outLines)
		})
	}
}

func Benchmark_Deduplication(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	d := NewLineDedupFilter([]string{"cluster"}, false)

	l := labels.Labels{
		{Name: "cluster", Value: "us-central1"},
	}

	lbs := NewBaseLabelsBuilder().ForLabels(l, l.Hash())
	lbs.Reset()

	line := randomLine(10)

	var outLines [][]byte
	for n := 0; n < b.N; n++ {
		if out, ok := d.Process(line, lbs); ok {
			outLines = append(outLines, out)
		}
	}

	require.Len(b, outLines, 1)
}

type entry struct {
	lbs  *labels.Labels
	line []byte
}

type lineLabelMap struct {
	labelSet *labels.Labels
	lines    [][]byte
}

type testData struct {
	Dev  *lineLabelMap
	QA   *lineLabelMap
	Prod *lineLabelMap
}

func (t *testData) allEntries() []entry {
	return append(append(t.Dev.toEntries(), t.Prod.toEntries()...), t.QA.toEntries()...)
}

func (t *testData) allLines() [][]byte {
	return append(append(t.Dev.lines, t.Prod.lines...), t.QA.lines...)
}

func newLineLabelMap(labelSet *labels.Labels, lineCount int) *lineLabelMap {
	var lines [][]byte
	for i := 0; i < lineCount; i++ {
		lines = append(lines, randomLine(10))
	}

	return &lineLabelMap{
		lines:    lines,
		labelSet: labelSet,
	}
}

func (l *lineLabelMap) first() []byte {
	return l.lines[0]
}

func (l *lineLabelMap) all() [][]byte {
	return l.lines
}

func (l *lineLabelMap) toEntries() []entry {
	var entries []entry

	for _, line := range l.lines {
		entries = append(entries, entry{line: line, lbs: l.labelSet})
	}

	return entries
}

func randomLine(n int) []byte {
	letters := []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ .;")

	b := make([]rune, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}

	return []byte(string(b))
}
