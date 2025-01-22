package logs

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
)

func Test_table_metadataCleanup(t *testing.T) {
	var buf tableBuffer
	initBuffer(&buf)

	_ = buf.Metadata("foo", 1024, dataset.CompressionOptions{})
	_ = buf.Metadata("bar", 1024, dataset.CompressionOptions{})

	table, err := buf.Flush()
	require.NoError(t, err)
	require.Equal(t, 2, len(table.metadatas))

	initBuffer(&buf)
	_ = buf.Metadata("bar", 1024, dataset.CompressionOptions{})

	table, err = buf.Flush()
	require.NoError(t, err)
	require.Equal(t, 1, len(table.metadatas))
	require.Equal(t, "bar", table.metadatas[0].Info.Name)
}

func initBuffer(buf *tableBuffer) {
	buf.StreamID(1024)
	buf.Timestamp(1024)
	buf.Message(1024, dataset.CompressionOptions{})
}
