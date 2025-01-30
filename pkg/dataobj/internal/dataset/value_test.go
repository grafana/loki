package dataset_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
)

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
		require.Equal(t, datasetmd.VALUE_TYPE_INT64, expect.Type())

		b, err := expect.MarshalBinary()
		require.NoError(t, err)

		var actual dataset.Value
		require.NoError(t, actual.UnmarshalBinary(b))
		require.Equal(t, datasetmd.VALUE_TYPE_INT64, actual.Type())
		require.Equal(t, expect.Int64(), actual.Int64())
	})

	t.Run("Uint64Value", func(t *testing.T) {
		expect := dataset.Uint64Value(1234)
		require.Equal(t, datasetmd.VALUE_TYPE_UINT64, expect.Type())

		b, err := expect.MarshalBinary()
		require.NoError(t, err)

		var actual dataset.Value
		require.NoError(t, actual.UnmarshalBinary(b))
		require.Equal(t, datasetmd.VALUE_TYPE_UINT64, actual.Type())
		require.Equal(t, expect.Uint64(), actual.Uint64())
	})

	t.Run("StringValue", func(t *testing.T) {
		t.Run("Empty", func(t *testing.T) {
			expect := dataset.StringValue("")
			require.Equal(t, datasetmd.VALUE_TYPE_STRING, expect.Type())

			b, err := expect.MarshalBinary()
			require.NoError(t, err)

			var actual dataset.Value
			require.NoError(t, actual.UnmarshalBinary(b))
			require.Equal(t, datasetmd.VALUE_TYPE_STRING, actual.Type())
			require.Equal(t, expect.String(), actual.String())
		})

		t.Run("Non-empty", func(t *testing.T) {
			expect := dataset.StringValue("hello, world!")
			require.Equal(t, datasetmd.VALUE_TYPE_STRING, expect.Type())

			b, err := expect.MarshalBinary()
			require.NoError(t, err)

			var actual dataset.Value
			require.NoError(t, actual.UnmarshalBinary(b))
			require.Equal(t, datasetmd.VALUE_TYPE_STRING, actual.Type())
			require.Equal(t, expect.String(), actual.String())
		})
	})
}
