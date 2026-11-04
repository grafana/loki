package index

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// BenchmarkPostingsBatch reproduces many concurrent queries against the same
// index file, each asking for a subset of the postings.
//   - The "pod" postings lists contain a single element, so those cases are
//     dominated by per-list overhead.
//   - The "a" lists are large.
//   - Some cases are bounded to a single shard, which exercises the case where
//     we don't fully read each postings list.
//
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
		name     string
		label    string
		values   []string
		fpFilter FingerprintFilter
	}{
		{"64-small-postings-lists-spread-through-the-index", labelName, spread(64), nil},
		{"64-small-postings-lists-consecutive-in-the-index", labelName, consecutive(64), nil},
		{"1024-small-postings-lists-consecutive-in-the-index", labelName, consecutive(1024), nil},
		{"1024-small-postings-lists-consecutive-in-the-index-sharded-to-1-16th", labelName, consecutive(1024), NewShard(0, 16)},
		{"2-big-postings-lists", "a", []string{"0", "1"}, nil},
		{"1-value-absent-from-the-index", labelName, []string{"pod-5-absent"}, nil},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					postings, err := r.Postings(tc.label, tc.fpFilter, tc.values...)
					if err != nil {
						b.Error(err)
						return
					}
					for postings.Next() {
						_ = postings.At()
					}
					if err := postings.Err(); err != nil {
						b.Error(err)
						return
					}
				}
			})
		})
	}
}
