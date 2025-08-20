package dataset_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
)

func BenchmarkValue_Type(b *testing.B) {
	tt := []struct {
		name  string
		value dataset.Value
	}{
		{"Null", dataset.Value{}},
		{"Int64Value", dataset.Int64Value(-1234)},
		{"Uint64Value", dataset.Uint64Value(1234)},
		{"ByteArrayValue", dataset.BinaryValue([]byte("hello, world!"))},
	}

	for _, tc := range tt {
		b.Run(tc.name, func(b *testing.B) {
			for b.Loop() {
				tc.value.Type()
			}
		})
	}
}

func BenchmarkValue_Create(b *testing.B) {
	b.Run("Null", func(b *testing.B) {
		for b.Loop() {
			_ = dataset.Value{}
		}
	})

	b.Run("Int64Value", func(b *testing.B) {
		for b.Loop() {
			_ = dataset.Int64Value(-1234)
		}
	})

	b.Run("Uint64Value", func(b *testing.B) {
		for b.Loop() {
			_ = dataset.Uint64Value(1234)
		}
	})

	b.Run("ByteArrayValue", func(b *testing.B) {
		for b.Loop() {
			_ = dataset.BinaryValue([]byte("hello, world!"))
		}
	})
}

func BenchmarkValue_Int64(b *testing.B) {
	v := dataset.Int64Value(-1234)
	for b.Loop() {
		v.Int64()
	}
}

func BenchmarkValue_Uint64(b *testing.B) {
	v := dataset.Uint64Value(1234)
	for b.Loop() {
		v.Uint64()
	}
}

func BenchmarkValue_ByteArray(b *testing.B) {
	v := dataset.BinaryValue([]byte("hello, world!"))
	for b.Loop() {
		v.Binary()
	}
}

func BenchmarkCompareValues(b *testing.B) {
	tt := []struct {
		name string
		a, b dataset.Value
	}{
		{"two nulls", dataset.Value{}, dataset.Value{}},
		{"int64 < int64", dataset.Int64Value(-1234), dataset.Int64Value(1234)},
		{"int64 == int64", dataset.Int64Value(1234), dataset.Int64Value(1234)},
		{"int64 > int64", dataset.Int64Value(1234), dataset.Int64Value(-1234)},
		{"uint64 < uint64", dataset.Uint64Value(1234), dataset.Uint64Value(5678)},
		{"uint64 == uint64", dataset.Uint64Value(1234), dataset.Uint64Value(1234)},
		{"uint64 > uint64", dataset.Uint64Value(5678), dataset.Uint64Value(1234)},
		{"bytearray < bytearray", dataset.BinaryValue([]byte("abc")), dataset.BinaryValue([]byte("def"))},
		{"bytearray == bytearray", dataset.BinaryValue([]byte("abc")), dataset.BinaryValue([]byte("abc"))},
		{"bytearray > bytearray", dataset.BinaryValue([]byte("def")), dataset.BinaryValue([]byte("abc"))},
	}

	for _, tc := range tt {
		b.Run(tc.name, func(b *testing.B) {
			for b.Loop() {
				dataset.CompareValues(&tc.a, &tc.b)
			}
		})
	}
}

func TestValue_MarshalBinary(t *testing.T) {
	t.Run("Null", func(t *testing.T) {
		var expect dataset.Value
		require.True(t, expect.IsNil())

		b, err := expect.MarshalBinary()
		require.NoError(t, err)

		var actual dataset.Value
		require.NoError(t, actual.UnmarshalBinary(b))
		require.True(t, actual.IsNil())
	})

	t.Run("Int64Value", func(t *testing.T) {
		expect := dataset.Int64Value(-1234)
		require.Equal(t, datasetmd.PHYSICAL_TYPE_INT64, expect.Type())

		b, err := expect.MarshalBinary()
		require.NoError(t, err)

		var actual dataset.Value
		require.NoError(t, actual.UnmarshalBinary(b))
		require.Equal(t, datasetmd.PHYSICAL_TYPE_INT64, actual.Type())
		require.Equal(t, expect.Int64(), actual.Int64())
	})

	t.Run("Uint64Value", func(t *testing.T) {
		expect := dataset.Uint64Value(1234)
		require.Equal(t, datasetmd.PHYSICAL_TYPE_UINT64, expect.Type())

		b, err := expect.MarshalBinary()
		require.NoError(t, err)

		var actual dataset.Value
		require.NoError(t, actual.UnmarshalBinary(b))
		require.Equal(t, datasetmd.PHYSICAL_TYPE_UINT64, actual.Type())
		require.Equal(t, expect.Uint64(), actual.Uint64())
	})

	t.Run("ByteArrayValue", func(t *testing.T) {
		t.Run("Empty", func(t *testing.T) {
			expect := dataset.BinaryValue([]byte{})
			require.Equal(t, datasetmd.PHYSICAL_TYPE_BINARY, expect.Type())

			b, err := expect.MarshalBinary()
			require.NoError(t, err)

			var actual dataset.Value
			require.NoError(t, actual.UnmarshalBinary(b))
			require.Equal(t, datasetmd.PHYSICAL_TYPE_BINARY, actual.Type())
			require.Equal(t, expect.Binary(), actual.Binary())
		})

		t.Run("Non-empty", func(t *testing.T) {
			expect := dataset.BinaryValue([]byte("hello, world!"))
			require.Equal(t, datasetmd.PHYSICAL_TYPE_BINARY, expect.Type())

			b, err := expect.MarshalBinary()
			require.NoError(t, err)

			var actual dataset.Value
			require.NoError(t, actual.UnmarshalBinary(b))
			require.Equal(t, datasetmd.PHYSICAL_TYPE_BINARY, actual.Type())
			require.Equal(t, expect.Binary(), actual.Binary())
		})
	})
}
