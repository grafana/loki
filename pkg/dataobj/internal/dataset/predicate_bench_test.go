package dataset

import "testing"

// BenchmarkInt64ValueSetContains compares the plain and memoized Int64 value
// sets. The "repeated" input models a sorted stream_id column (long runs ->
// cache hits); the "distinct" input models a column with no runs (every lookup
// misses the cache), the worst case for memoization.
func BenchmarkInt64ValueSetContains(b *testing.B) {
	const setSize = 1024
	ids := make([]Value, setSize)
	for i := range setSize {
		ids[i] = Int64Value(int64(i))
	}
	plain := NewInt64ValueSet(ids)
	memoized := NewMemoizedInt64ValueSet(ids)

	const nRows = 4096
	distinct := make([]Value, nRows)
	repeated := make([]Value, nRows)
	for r := range nRows {
		distinct[r] = Int64Value(int64(r % setSize))
		repeated[r] = Int64Value(7)
	}

	run := func(b *testing.B, values []Value, set ValueSet) {
		var sink bool
		b.ResetTimer()
		for range b.N {
			for i := range values {
				sink = set.Contains(values[i])
			}
		}
		_ = sink
	}

	b.Run("plain_repeated", func(b *testing.B) { run(b, repeated, plain) })
	b.Run("plain_distinct", func(b *testing.B) { run(b, distinct, plain) })
	b.Run("memoized_repeated", func(b *testing.B) { run(b, repeated, memoized) })
	b.Run("memoized_distinct", func(b *testing.B) { run(b, distinct, memoized) })
}
