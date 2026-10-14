package dataset_test

import (
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
)

// namedPredicate stands in for any predicate that names a column.
var namedPredicate = dataset.EqualPredicate{Value: dataset.Int64Value(1)}

func TestIsConstPredicate(t *testing.T) {
	for _, tc := range []struct {
		name      string
		predicate dataset.Predicate
		wantKeep  bool
		wantOK    bool
	}{
		{"true", dataset.TruePredicate{}, true, true},
		{"false", dataset.FalsePredicate{}, false, true},
		{"nil keeps every row", nil, true, true},
		{"a column predicate is not constant", namedPredicate, false, false},
		{"a composite is not constant", dataset.AndPredicate{Left: namedPredicate, Right: namedPredicate}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			keep, ok := dataset.IsConstPredicate(tc.predicate)
			require.Equal(t, tc.wantOK, ok)
			require.Equal(t, tc.wantKeep, keep)
		})
	}
}

func TestNewConstPredicate(t *testing.T) {
	require.Equal(t, dataset.TruePredicate{}, dataset.NewConstPredicate(true))
	require.Equal(t, dataset.FalsePredicate{}, dataset.NewConstPredicate(false))
}

func TestInt64ValueSet(t *testing.T) {
	implementations := []struct {
		name   string
		newSet func(members ...int64) dataset.ValueSet
	}{
		{"plain", func(members ...int64) dataset.ValueSet { return dataset.NewInt64ValueSetOf(members...) }},
		{"memoized", func(members ...int64) dataset.ValueSet { return dataset.NewMemoizedInt64ValueSetOf(members...) }},
	}

	for _, impl := range implementations {
		t.Run(impl.name, func(t *testing.T) {
			t.Run("Size() counts a duplicate member once", func(t *testing.T) {
				set := impl.newSet(1, 3, 3, 7)
				require.Equal(t, 3, set.Size())
			})

			t.Run("Contains() returns true for every member, including the extreme values", func(t *testing.T) {
				members := []int64{math.MinInt64, -1, 0, 1, 3, math.MaxInt64}
				set := impl.newSet(members...)
				for _, member := range members {
					require.True(t, set.Contains(dataset.Int64Value(member)), "member %d", member)
				}
			})

			t.Run("Contains() returns false for a value that is not a member", func(t *testing.T) {
				set := impl.newSet(1, 3, 7)
				for _, value := range []int64{math.MinInt64, -1, 0, 2, 4, 8, math.MaxInt64} {
					require.False(t, set.Contains(dataset.Int64Value(value)), "non-member %d", value)
				}
			})

			t.Run("Iter() yields every member once", func(t *testing.T) {
				set := impl.newSet(7, 1, 3, 3)

				var got []int64
				for value := range set.Iter() {
					got = append(got, value.Int64())
				}
				slices.Sort(got)
				require.Equal(t, []int64{1, 3, 7}, got)
			})

			t.Run("Size() is 0 and Contains() returns false on an empty set, zero included", func(t *testing.T) {
				set := impl.newSet()
				require.Equal(t, 0, set.Size())
				require.False(t, set.Contains(dataset.Int64Value(0)))
				require.False(t, set.Contains(dataset.Int64Value(5)))
			})

			t.Run("Contains() returns true for zero on the first lookup when the set holds zero", func(t *testing.T) {
				set := impl.newSet(0)
				require.True(t, set.Contains(dataset.Int64Value(0)))
			})

			t.Run("Contains() returns false for zero on the first lookup when the set lacks zero", func(t *testing.T) {
				set := impl.newSet(5)
				require.False(t, set.Contains(dataset.Int64Value(0)))
			})
		})
	}
}

func TestMemoizedInt64ValueSet(t *testing.T) {
	t.Run("Contains() returns the same answers as the plain set for lookups with runs, alternation and absent keys", func(t *testing.T) {
		plain := dataset.NewInt64ValueSetOf(1, 3, 7)
		memoized := dataset.NewMemoizedInt64ValueSetOf(1, 3, 7)

		for i, key := range []int64{7, 7, 7, 2, 2, 2, 1, 3, 1, 3, 0, 0, 7, 9, 9, 7, 7} {
			value := dataset.Int64Value(key)
			require.Equal(t, plain.Contains(value), memoized.Contains(value), "lookup %d of key %d", i, key)
		}
	})

	t.Run("Contains() returns the same answers as the plain set for random lookups in runs of 1 to 100", func(t *testing.T) {
		rng := rand.New(rand.NewPCG(1, 2))

		var members []int64
		for range 200 {
			members = append(members, rng.Int64N(500))
		}
		plain := dataset.NewInt64ValueSetOf(members...)
		memoized := dataset.NewMemoizedInt64ValueSetOf(members...)

		for lookups := 0; lookups < 100_000; {
			value := dataset.Int64Value(rng.Int64N(1000) - 250)
			for range 1 + rng.IntN(100) {
				require.Equal(t, plain.Contains(value), memoized.Contains(value), "lookup %d of key %d", lookups, value.Int64())
				lookups++
			}
		}
	})

	t.Run("Contains() repeats a negative answer for a repeated absent key", func(t *testing.T) {
		set := dataset.NewMemoizedInt64ValueSetOf(1, 3)
		for range 3 {
			require.False(t, set.Contains(dataset.Int64Value(2)))
		}
	})

	t.Run("Contains() replaces the cached answer when the key changes between a member and a non-member", func(t *testing.T) {
		set := dataset.NewMemoizedInt64ValueSetOf(1, 3)
		require.True(t, set.Contains(dataset.Int64Value(1)))
		require.False(t, set.Contains(dataset.Int64Value(2)))
		require.True(t, set.Contains(dataset.Int64Value(3)))
		require.True(t, set.Contains(dataset.Int64Value(1)))
	})

	t.Run("Iter() and Size() keep every member after Contains() lookups", func(t *testing.T) {
		set := dataset.NewMemoizedInt64ValueSetOf(1, 3)
		set.Contains(dataset.Int64Value(1))
		set.Contains(dataset.Int64Value(2))

		var got []int64
		for value := range set.Iter() {
			got = append(got, value.Int64())
		}
		slices.Sort(got)
		require.Equal(t, []int64{1, 3}, got)
		require.Equal(t, 2, set.Size())
	})
}

func BenchmarkInt64ValueSet_Contains(b *testing.B) {
	const (
		setSize     = 200
		lookupCount = 4096
	)

	// The members are the even numbers below twice the set size. Lookups cover every number
	// below that bound, so half of them hit.
	members := make([]int64, 0, setSize)
	for i := range setSize {
		members = append(members, int64(2*i))
	}

	sets := []struct {
		name string
		set  dataset.ValueSet
	}{
		{"plain", dataset.NewInt64ValueSetOf(members...)},
		{"memoized", dataset.NewMemoizedInt64ValueSetOf(members...)},
	}

	for _, runLength := range []int{1, 2, 8, 64, 1000} {
		lookups := make([]dataset.Value, lookupCount)
		for i := range lookups {
			lookups[i] = dataset.Int64Value(int64((i / runLength) % (2 * setSize)))
		}

		for _, s := range sets {
			b.Run(fmt.Sprintf("%s/run=%d", s.name, runLength), func(b *testing.B) {
				var hits int
				for b.Loop() {
					for _, value := range lookups {
						if s.set.Contains(value) {
							hits++
						}
					}
				}
				b.ReportMetric(float64(hits)/float64(b.N), "hits/op")
			})
		}
	}
}
