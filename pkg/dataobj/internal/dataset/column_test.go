package dataset

import (
	"context"
	"errors"
	"io"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
)

func TestColumnBuilder_ReadWrite(t *testing.T) {
	in := [][]byte{
		[]byte("hello, world!"),
		[]byte(""),
		[]byte("this is a test of the emergency broadcast system"),
		[]byte("this is only a test"),
		[]byte("if this were a real emergency, you would be instructed to panic"),
		[]byte("but it's not, so don't"),
		[]byte(""),
		[]byte("this concludes the test"),
		[]byte("thank you for your cooperation"),
		[]byte("goodbye"),
	}

	// Randomize max number of rows per page
	pageMaxRows := rand.IntN(len(in)/2) + 1
	expectedPages := len(in) / pageMaxRows
	if len(in)%pageMaxRows != 0 {
		expectedPages++
	}

	t.Log("Max rows per page:", pageMaxRows)
	t.Log("Expected pages:", expectedPages)

	opts := BuilderOptions{
		PageSizeHint:    0, // Set the size to 0 so each column has exactly one value.
		PageMaxRowCount: pageMaxRows,
		Type:            ColumnType{Physical: datasetmd.PHYSICAL_TYPE_BINARY, Logical: "data"},
		Compression:     datasetmd.COMPRESSION_TYPE_ZSTD,
		Encoding:        datasetmd.ENCODING_TYPE_PLAIN,
	}
	b, err := NewColumnBuilder("", opts)
	require.NoError(t, err)

	for i, s := range in {
		require.NoError(t, b.Append(i, BinaryValue(s)))
	}

	col, err := b.Flush()
	require.NoError(t, err)
	require.Equal(t, ColumnType{Physical: datasetmd.PHYSICAL_TYPE_BINARY, Logical: "data"}, col.Desc.Type)
	require.Equal(t, len(in), col.Desc.RowsCount)
	require.Equal(t, len(in), col.Desc.ValuesCount)
	require.GreaterOrEqual(t, len(col.Pages), len(in)/pageMaxRows)

	t.Log("Uncompressed size:", col.Desc.UncompressedSize)
	t.Log("Compressed size:", col.Desc.CompressedSize)
	t.Log("Pages:", len(col.Pages))

	var actual [][]byte

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
			actual = append(actual, []byte{})
		} else {
			require.Equal(t, datasetmd.PHYSICAL_TYPE_BINARY, val.Type())
			actual = append(actual, val.Binary())
		}
	}

	require.Equal(t, in, actual)
}

func TestColumnBuilder_MinMax(t *testing.T) {
	var (
		aString = strings.Repeat("a", 100)
		bString = strings.Repeat("b", 100)
		cString = strings.Repeat("c", 100)

		dString = strings.Repeat("d", 100)
		eString = strings.Repeat("e", 100)
		fString = strings.Repeat("f", 100)
	)

	in := []string{
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
		Type:         ColumnType{Physical: datasetmd.PHYSICAL_TYPE_BINARY, Logical: "data"},
		Compression:  datasetmd.COMPRESSION_TYPE_NONE,
		Encoding:     datasetmd.ENCODING_TYPE_PLAIN,

		Statistics: StatisticsOptions{
			StoreRangeStats: true,
		},
	}
	b, err := NewColumnBuilder("", opts)
	require.NoError(t, err)

	for i, s := range in {
		require.NoError(t, b.Append(i, BinaryValue([]byte(s))))
	}

	col, err := b.Flush()
	require.NoError(t, err)
	require.Equal(t, ColumnType{Physical: datasetmd.PHYSICAL_TYPE_BINARY, Logical: "data"}, col.Desc.Type)
	require.NotNil(t, col.Desc.Statistics)

	columnMin, columnMax := getMinMax(t, col.Desc.Statistics)
	require.Equal(t, aString, string(columnMin.Binary()))
	require.Equal(t, fString, string(columnMax.Binary()))

	require.Len(t, col.Pages, 2)
	require.Equal(t, 3, col.Pages[0].Desc.ValuesCount)
	require.Equal(t, 3, col.Pages[1].Desc.ValuesCount)

	page0Min, page0Max := getMinMax(t, col.Pages[0].Desc.Stats)
	require.Equal(t, aString, string(page0Min.Binary()))
	require.Equal(t, cString, string(page0Max.Binary()))

	page1Min, page1Max := getMinMax(t, col.Pages[1].Desc.Stats)
	require.Equal(t, dString, string(page1Min.Binary()))
	require.Equal(t, fString, string(page1Max.Binary()))
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
		Type:         ColumnType{Physical: datasetmd.PHYSICAL_TYPE_BINARY, Logical: "data"},
		Compression:  datasetmd.COMPRESSION_TYPE_NONE,
		Encoding:     datasetmd.ENCODING_TYPE_PLAIN,

		Statistics: StatisticsOptions{
			StoreCardinalityStats: true,
		},
	}
	b, err := NewColumnBuilder("", opts)
	require.NoError(t, err)

	for i, s := range in {
		require.NoError(t, b.Append(i, BinaryValue([]byte(s))))
	}

	col, err := b.Flush()
	require.NoError(t, err)
	require.Equal(t, ColumnType{Physical: datasetmd.PHYSICAL_TYPE_BINARY, Logical: "data"}, col.Desc.Type)
	require.NotNil(t, col.Desc.Statistics)
	// we use sparse hyperloglog reprs until a certain cardinality is reached,
	// so this should not be approximate at low counts.
	require.Equal(t, uint64(3), col.Desc.Statistics.CardinalityCount)
}

func getMinMax(t *testing.T, stats *datasetmd.Statistics) (minVal, maxVal Value) {
	t.Helper()
	require.NotNil(t, stats)

	var minValue, maxValue Value

	require.NoError(t, minValue.UnmarshalBinary(stats.MinValue))
	require.NoError(t, maxValue.UnmarshalBinary(stats.MaxValue))

	return minValue, maxValue
}
