package dataset

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInt64Set(t *testing.T) {
	testInt64ValueSet(t, func(v []Value) ValueSet { return NewInt64ValueSet(v) })
}

func TestMemoizedInt64Set(t *testing.T) {
	testInt64ValueSet(t, NewMemoizedInt64ValueSet)
}

// testInt64ValueSet runs the [ValueSet] contract against a set built by newSet
// from the given members. It is shared so the plain and memoized Int64 sets are
// held to the same behavior.
func testInt64ValueSet(t *testing.T, newSet func([]Value) ValueSet) {
	t.Helper()

	members := []Value{Int64Value(1), Int64Value(3), Int64Value(7)}
	set := newSet(members)

	t.Run("size", func(t *testing.T) {
		assert.Equal(t, len(members), set.Size())
	})

	t.Run("contains", func(t *testing.T) {
		for _, v := range []int64{1, 3, 7} {
			assert.True(t, set.Contains(Int64Value(v)), "member %d", v)
		}
		for _, v := range []int64{0, 2, 4, 8} {
			assert.False(t, set.Contains(Int64Value(v)), "non-member %d", v)
		}
	})

	t.Run("iter", func(t *testing.T) {
		var got []int64
		for v := range set.Iter() {
			got = append(got, v.Int64())
		}
		slices.Sort(got)
		assert.Equal(t, []int64{1, 3, 7}, got)
	})
}

// TestMemoizedInt64Set_Memoization checks the memoized set against a plain set
// over a sequence with runs and transitions, so cache hits (repeated value),
// cache misses (value changes), and a cached negative (a value absent from the
// set) are all exercised.
func TestMemoizedInt64Set_Memoization(t *testing.T) {
	vals := []Value{Int64Value(1), Int64Value(3)}
	plain := NewInt64ValueSet(vals)
	memoized := NewMemoizedInt64ValueSet(vals)

	for _, v := range []int64{1, 1, 1, 2, 2, 3, 3, 1, 3, 3} {
		value := Int64Value(v)
		assert.Equal(t, plain.Contains(value), memoized.Contains(value), "value %d", v)
	}
}

// TestMemoizedInt64Set_ColdCacheZero exercises the haveLast guard: a set that
// contains 0 must return true on the first lookup, even though the cache's
// zero-value lastKey is also 0.
func TestMemoizedInt64Set_ColdCacheZero(t *testing.T) {
	set := NewMemoizedInt64ValueSet([]Value{Int64Value(0)})
	assert.True(t, set.Contains(Int64Value(0)))
}
