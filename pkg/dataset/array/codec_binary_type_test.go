package array_test

import (
	"testing"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/dataset/array"
	"github.com/grafana/loki/v3/pkg/dataset/types"
	"github.com/grafana/loki/v3/pkg/memory"
	"github.com/stretchr/testify/require"
)

// TestBinaryTypeRoundTrip verifies that opaque binary data (including non-UTF-8
// byte sequences) round-trips correctly through the binary codec with
// types.Binary, and that the KindBinary type is preserved in the array metadata.
func TestBinaryTypeRoundTrip(t *testing.T) {
	var alloc memory.Allocator
	var store inMemoryStore

	var (
		spec = &array.SpecBinary{Offsets: &array.SpecPlain{}}
		typ  = &types.Binary{Nullable: false}
	)

	w, err := array.NewWriter(&alloc, spec, typ)
	require.NoError(t, err)

	// Build an array with opaque bytes including non-UTF-8 sequences.
	builder := columnar.NewUTF8Builder(&alloc)
	builder.AppendValue([]byte{0xFF, 0xFE, 0x00})
	builder.AppendValue([]byte("valid ascii"))
	builder.AppendValue([]byte{0x00, 0x01, 0x02, 0x03})
	builder.AppendValue([]byte{})
	input := builder.Build()

	require.NoError(t, w.Append(input))

	result, err := w.Flush(t.Context(), &store)
	require.NoError(t, err)

	// Verify KindBinary is preserved in the array metadata.
	require.Equal(t, types.KindBinary, result.Type.Kind(), "expected KindBinary to be preserved in array metadata")
	require.Equal(t, 4, result.Stats.RowCount)
	require.Equal(t, 0, result.Stats.NullCount)

	r, err := array.NewReader(&alloc, result, &store)
	require.NoError(t, err)

	actual, err := r.Read(t.Context(), &alloc, result.Stats.RowCount)
	require.NoError(t, err)

	utf8Actual, ok := actual.(*columnar.UTF8)
	require.True(t, ok)
	require.Equal(t, 4, utf8Actual.Len())
	require.Equal(t, []byte{0xFF, 0xFE, 0x00}, utf8Actual.Get(0))
	require.Equal(t, []byte("valid ascii"), utf8Actual.Get(1))
	require.Equal(t, []byte{0x00, 0x01, 0x02, 0x03}, utf8Actual.Get(2))
	require.Equal(t, []byte{}, utf8Actual.Get(3))
}

// TestBinaryNullableRoundTrip verifies that nullable binary data round-trips
// correctly, including proper null handling via IsValid().
func TestBinaryNullableRoundTrip(t *testing.T) {
	var alloc memory.Allocator
	var store inMemoryStore

	var (
		spec = &array.SpecBinary{Offsets: &array.SpecPlain{}, Validity: &array.SpecBool{}}
		typ  = &types.Binary{Nullable: true}
	)

	w, err := array.NewWriter(&alloc, spec, typ)
	require.NoError(t, err)

	// Build input with nulls interspersed.
	builder := columnar.NewUTF8Builder(&alloc)
	builder.AppendValue([]byte{0xDE, 0xAD, 0xBE, 0xEF})
	builder.AppendNull()
	builder.AppendValue([]byte("data"))
	builder.AppendNull()
	builder.AppendValue([]byte{0x01})
	input := builder.Build()

	require.NoError(t, w.Append(input))

	result, err := w.Flush(t.Context(), &store)
	require.NoError(t, err)

	// Verify KindBinary is preserved in the array metadata.
	require.Equal(t, types.KindBinary, result.Type.Kind(), "expected KindBinary to be preserved in array metadata")
	require.Equal(t, 5, result.Stats.RowCount)
	require.Equal(t, 2, result.Stats.NullCount)

	r, err := array.NewReader(&alloc, result, &store)
	require.NoError(t, err)

	actual, err := r.Read(t.Context(), &alloc, result.Stats.RowCount)
	require.NoError(t, err)

	utf8Actual, ok := actual.(*columnar.UTF8)
	require.True(t, ok)
	require.Equal(t, 5, utf8Actual.Len())

	// Verify non-null values.
	require.False(t, utf8Actual.IsNull(0))
	require.Equal(t, []byte{0xDE, 0xAD, 0xBE, 0xEF}, utf8Actual.Get(0))

	// Verify null at index 1.
	require.True(t, utf8Actual.IsNull(1))

	require.False(t, utf8Actual.IsNull(2))
	require.Equal(t, []byte("data"), utf8Actual.Get(2))

	// Verify null at index 3.
	require.True(t, utf8Actual.IsNull(3))

	require.False(t, utf8Actual.IsNull(4))
	require.Equal(t, []byte{0x01}, utf8Actual.Get(4))
}
