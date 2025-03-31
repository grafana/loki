package dataset

import (
	"context"
	"errors"
	"io"
	"strings"
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

	r := newColumnReader(col)
	for {
		var values [1]Value
		n, err := r.Read(context.Background(), values[:])
		if err != nil && !errors.Is(err, io.EOF) {
			require.NoError(t, err)
		} else if n == 0 && errors.Is(err, io.EOF) {
			break
		} else if n == 0 {
			continue
		}

		val := values[0]
		if val.IsNil() || val.IsZero() {
			actual = append(actual, "")
		} else {
			require.Equal(t, datasetmd.VALUE_TYPE_STRING, val.Type())
			actual = append(actual, val.String())
		}
	}

	require.Equal(t, in, actual)
}

func TestColumnBuilder_MinMax(t *testing.T) {
	var (
		// We include the null string in the test to ensure that it's never
		// considered in min/max ranges.
		nullString = ""

		aString = strings.Repeat("a", 100)
		bString = strings.Repeat("b", 100)
		cString = strings.Repeat("c", 100)

		dString = strings.Repeat("d", 100)
		eString = strings.Repeat("e", 100)
		fString = strings.Repeat("f", 100)
	)

	in := []string{
		nullString,

		// We append strings out-of-order below to ensure that the min/max
		// comparisons are working properly.
		//
		// Strings are grouped by which page they'll be appended to.

		bString,
		cString,
		aString,

		eString,
		fString,
		dString,
	}

	opts := BuilderOptions{
		PageSizeHint: 301, // Slightly larger than the string length of 3 strings per page.
		Value:        datasetmd.VALUE_TYPE_STRING,
		Compression:  datasetmd.COMPRESSION_TYPE_NONE,
		Encoding:     datasetmd.ENCODING_TYPE_PLAIN,

		Statistics: StatisticsOptions{
			StoreRangeStats: true,
		},
	}
	b, err := NewColumnBuilder("", opts)
	require.NoError(t, err)

	for i, s := range in {
		require.NoError(t, b.Append(i, StringValue(s)))
	}

	col, err := b.Flush()
	require.NoError(t, err)
	require.Equal(t, datasetmd.VALUE_TYPE_STRING, col.Info.Type)
	require.NotNil(t, col.Info.Statistics)

	columnMin, columnMax := getMinMax(t, col.Info.Statistics)
	require.Equal(t, aString, columnMin.String())
	require.Equal(t, fString, columnMax.String())

	require.Len(t, col.Pages, 2)
	require.Equal(t, 3, col.Pages[0].Info.ValuesCount)
	require.Equal(t, 3, col.Pages[1].Info.ValuesCount)

	page0Min, page0Max := getMinMax(t, col.Pages[0].Info.Stats)
	require.Equal(t, aString, page0Min.String())
	require.Equal(t, cString, page0Max.String())

	page1Min, page1Max := getMinMax(t, col.Pages[1].Info.Stats)
	require.Equal(t, dString, page1Min.String())
	require.Equal(t, fString, page1Max.String())
}

func TestColumnBuilder_Cardinality(t *testing.T) {
	var (
		// We include the null string in the test to ensure that it's never
		// considered in min/max ranges.
		nullString = ""

		aString = strings.Repeat("a", 100)
		bString = strings.Repeat("b", 100)
		cString = strings.Repeat("c", 100)
	)

	// We store nulls and duplicates (should not be counted in cardinality count)
	in := []string{
		nullString,

		aString,

		bString,
		bString,
		bString,

		cString,
	}

	opts := BuilderOptions{
		PageSizeHint: 301, // Slightly larger than the string length of 3 strings per page.
		Value:        datasetmd.VALUE_TYPE_STRING,
		Compression:  datasetmd.COMPRESSION_TYPE_NONE,
		Encoding:     datasetmd.ENCODING_TYPE_PLAIN,

		Statistics: StatisticsOptions{
			StoreCardinalityStats: true,
		},
	}
	b, err := NewColumnBuilder("", opts)
	require.NoError(t, err)

	for i, s := range in {
		require.NoError(t, b.Append(i, StringValue(s)))
	}

	col, err := b.Flush()
	require.NoError(t, err)
	require.Equal(t, datasetmd.VALUE_TYPE_STRING, col.Info.Type)
	require.NotNil(t, col.Info.Statistics)
	// we use sparse hyperloglog reprs until a certain cardinality is reached,
	// so this should not be approximate at low counts.
	require.Equal(t, uint64(3), col.Info.Statistics.CardinalityCount)
}

func getMinMax(t *testing.T, stats *datasetmd.Statistics) (minVal, maxVal Value) {
	t.Helper()
	require.NotNil(t, stats)

	var minValue, maxValue Value

	require.NoError(t, minValue.UnmarshalBinary(stats.MinValue))
	require.NoError(t, maxValue.UnmarshalBinary(stats.MaxValue))

	return minValue, maxValue
}
