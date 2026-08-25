package columnar_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/memory"
)

func TestArray_Slice(t *testing.T) {
	t.Run("type=Null", func(t *testing.T) {
		var alloc memory.Allocator

		builder := columnar.NewNullBuilder(&alloc)
		builder.AppendNulls(32)

		orig := builder.Build()
		slice := orig.Slice(3, 10).(*columnar.Null)

		require.Equal(t, 7, slice.Len())
	})

	t.Run("type=Bool", func(t *testing.T) {
		var alloc memory.Allocator

		builder := columnar.NewBoolBuilder(&alloc)
		builder.Grow(32)

		// true every 2 values, with nulls starting at idx=3 and continuing every 3
		for i := range 32 {
			if i > 0 && i%3 == 0 {
				builder.AppendNull()
				continue
			}
			builder.AppendValue(i%2 == 0)
		}

		orig := builder.Build()
		slice := orig.Slice(3, 10).(*columnar.Bool)

		require.Equal(t, 7, slice.Len())

		for i := range slice.Len() {
			require.Equal(t, slice.IsNull(i), orig.IsNull(3+i), "null mismatch at index %d", i)
			if !slice.IsNull(i) {
				// Compare values as well.
				require.Equal(t, slice.Get(i), orig.Get(3+i), "value mismatch at index %d", i)
			}
		}
	})

	t.Run("type=Number", func(t *testing.T) {
		var alloc memory.Allocator

		builder := columnar.NewNumberBuilder[int64](&alloc)
		builder.Grow(32)

		for i := range 32 {
			// Nulls starting at idx=3 and continuing every 3
			if i > 0 && i%3 == 0 {
				builder.AppendNull()
				continue
			}
			builder.AppendValue(int64(i))
		}

		orig := builder.Build()
		slice := orig.Slice(3, 10).(*columnar.Number[int64])

		require.Equal(t, 7, slice.Len())

		for i := range slice.Len() {
			require.Equal(t, slice.IsNull(i), orig.IsNull(3+i), "null mismatch at index %d", i)
			if !slice.IsNull(i) {
				// Compare values as well.
				require.Equal(t, slice.Get(i), orig.Get(3+i), "value mismatch at index %d", i)
			}
		}
	})

	t.Run("type=UTF8", func(t *testing.T) {
		var alloc memory.Allocator

		builder := columnar.NewUTF8Builder(&alloc)
		builder.Grow(32)

		input := []string{
			"!", "a", "hello", "world", "test", "foo", "bar", "baz",
			"alice", "bob", "charlie", "delta", "echo", "foxtrot", "golf", "hotel",
			"india", "juliet", "kilo", "lima", "mike", "november", "oscar", "papa",
			"quebec", "romeo", "sierra", "tango", "uniform", "victor", "whiskey", "xray",
		}

		for i, value := range input {
			// Nulls starting at idx=3 and continuing every 3
			if i > 0 && i%3 == 0 {
				builder.AppendNull()
				continue
			}
			builder.AppendValue([]byte(value))
		}

		orig := builder.Build()
		slice := orig.Slice(3, 10).(*columnar.UTF8)

		require.Equal(t, 7, slice.Len())

		for i := range slice.Len() {
			require.Equal(t, slice.IsNull(i), orig.IsNull(3+i), "null mismatch at index %d", i)
			if !slice.IsNull(i) {
				// Compare values as well.
				require.Equal(t, slice.Get(i), orig.Get(3+i), "value mismatch at index %d", i)
			}
		}
	})
}
