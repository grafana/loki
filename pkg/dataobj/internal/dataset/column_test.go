package dataset

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
)

func TestColumnBuilder_ReadWrite(t *testing.T) {
	in := []string{
		"hello, world!",
		"",
		"this is a test of the emergency broadcast system",
		"this is only a test",
		"if this were a real emergency, you would be instructed to panic",
		"but it's not, so don't",
		"",
		"this concludes the test",
		"thank you for your cooperation",
		"goodbye",
	}

	opts := BuilderOptions{
		// Set the size to 0 so each column has exactly one value.
		PageSizeHint: 0,
		Value:        datasetmd.VALUE_TYPE_STRING,
		Compression:  datasetmd.COMPRESSION_TYPE_ZSTD,
		Encoding:     datasetmd.ENCODING_TYPE_PLAIN,
	}
	b, err := NewColumnBuilder("", opts)
	require.NoError(t, err)

	for i, s := range in {
		require.NoError(t, b.Append(i, StringValue(s)))
	}

	col, err := b.Flush()
	require.NoError(t, err)
	require.Equal(t, datasetmd.VALUE_TYPE_STRING, col.Info.Type)
	require.Equal(t, len(in), col.Info.RowsCount)
	require.Equal(t, len(in)-2, col.Info.ValuesCount) // -2 for the empty strings
	require.Greater(t, len(col.Pages), 1)

	t.Log("Uncompressed size: ", col.Info.UncompressedSize)
	t.Log("Compressed size: ", col.Info.CompressedSize)
	t.Log("Pages: ", len(col.Pages))

	var actual []string
	for result := range iterMemColumn(col) {
		val, err := result.Value()
		require.NoError(t, err)

		if val.IsNil() || val.IsZero() {
			actual = append(actual, "")
		} else {
			require.Equal(t, datasetmd.VALUE_TYPE_STRING, val.Type())
			actual = append(actual, val.String())
		}
	}
	require.Equal(t, in, actual)
}
