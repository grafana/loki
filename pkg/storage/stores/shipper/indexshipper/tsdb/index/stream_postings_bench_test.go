package index

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// BenchmarkPostingsBatch reproduces many concurrent queries against the same
// index file, each asking for a subset of the postings.
// All the queried postings lists contain a single element, so the cost is
// dominated by per-list overhead.
// Run with -cpu to vary contention.
func BenchmarkPostingsBatch(b *testing.B) {
	const (
		numSeries = 100_000
		labelName = "pod"
	)
	path := writeBenchmarkFixture(b, numSeries, 1)
	r, err := NewStreamFileReader(path, DefaultStreamOptions())
	require.NoError(b, err)
	b.Cleanup(func() { _ = r.Close() })

	allValues, err := r.LabelValues(labelName)
	require.NoError(b, err)
	require.Len(b, allValues, numSeries)

	// spread picks n values evenly across the label, so their postings
	// lists are far apart in the file.
	spread := func(n int) []string {
		out := make([]string, 0, n)
		for i := range n {
			out = append(out, allValues[i*(len(allValues)/n)])
		}
		return out
	}
	// consecutive picks a run of n adjacent values, as a prefix regex
	// would, so their postings lists are adjacent in the file.
	consecutive := func(n int) []string {
		start := len(allValues) / 3
		return allValues[start : start+n]
	}

	for _, tc := range []struct {
		name   string
		values []string
	}{
		{"spread-64", spread(64)},
		{"consecutive-64", consecutive(64)},
		{"consecutive-1024", consecutive(1024)},
	} {
		b.Run(tc.name, func(b *testing.B) {
			want := len(tc.values)
			b.ReportAllocs()
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					postings, err := r.Postings(labelName, nil, tc.values...)
					if err != nil {
						b.Error(err)
						return
					}
					n := 0
					for postings.Next() {
						n++
					}
					if err := postings.Err(); err != nil {
						b.Error(err)
						return
					}
					if n != want {
						b.Errorf("got %d postings, want %d", n, want)
						return
					}
				}
			})
		})
	}
}
