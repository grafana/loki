package dataset

import (
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
)

func Test_pageBuilder_WriteRead(t *testing.T) {
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
		PageSizeHint: 1024,
		Value:        datasetmd.VALUE_TYPE_STRING,
		Compression:  datasetmd.COMPRESSION_TYPE_ZSTD,
		Encoding:     datasetmd.ENCODING_TYPE_PLAIN,
	}
	b, err := newPageBuilder(opts)
	require.NoError(t, err)

	for _, s := range in {
		require.True(t, b.Append(StringValue(s)))
	}

	page, err := b.Flush()
	require.NoError(t, err)
	require.Equal(t, len(in), page.Info.RowCount)
	require.Equal(t, len(in)-2, page.Info.ValuesCount) // -2 for the empty strings

	t.Log("Uncompressed size: ", page.Info.UncompressedSize)
	t.Log("Compressed size: ", page.Info.CompressedSize)

	var actual []string
	for result := range iterMemPage(page, opts.Value, opts.Compression) {
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

func Test_pageBuilder_Fill(t *testing.T) {
	opts := BuilderOptions{
		PageSizeHint: 1_500_000,
		Value:        datasetmd.VALUE_TYPE_INT64,
		Compression:  datasetmd.COMPRESSION_TYPE_NONE,
		Encoding:     datasetmd.ENCODING_TYPE_DELTA,
	}
	buf, err := newPageBuilder(opts)
	require.NoError(t, err)

	ts := time.Now().UTC()
	for buf.Append(Int64Value(ts.UnixNano())) {
		ts = ts.Add(time.Duration(rand.Intn(5000)) * time.Millisecond)
	}

	page, err := buf.Flush()
	require.NoError(t, err)
	require.Equal(t, page.Info.UncompressedSize, page.Info.CompressedSize)

	t.Log("Uncompressed size: ", page.Info.UncompressedSize)
	t.Log("Compressed size: ", page.Info.CompressedSize)
	t.Log("Row count: ", page.Info.RowCount)
}
