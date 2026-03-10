package columnar

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	columnarv2 "github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/memory"
)

func TestReaderAdapter_ReadPreservesUTF8Nulls(t *testing.T) {
	builder, err := dataset.NewColumnBuilder("path", dataset.BuilderOptions{
		Type:        dataset.ColumnType{Physical: datasetmd.PHYSICAL_TYPE_BINARY, Logical: "path"},
		Encoding:    datasetmd.ENCODING_TYPE_PLAIN,
		Compression: datasetmd.COMPRESSION_TYPE_NONE,
	})
	require.NoError(t, err)

	require.NoError(t, builder.Append(0, dataset.BinaryValue([]byte("a"))))
	require.NoError(t, builder.Append(1, dataset.Value{}))
	require.NoError(t, builder.Append(2, dataset.BinaryValue([]byte("b"))))

	col, err := builder.Flush()
	require.NoError(t, err)

	dset := dataset.FromMemory([]*dataset.MemColumn{col})
	reader := NewReaderAdapter(dataset.RowReaderOptions{
		Dataset: dset,
		Columns: []dataset.Column{col},
	})
	t.Cleanup(func() { require.NoError(t, reader.Close()) })
	require.NoError(t, reader.Open(context.Background()))

	alloc := memory.NewAllocator(nil)
	defer alloc.Reclaim()

	rb, err := reader.Read(context.Background(), alloc, 3)
	if err != nil && !errors.Is(err, io.EOF) {
		require.NoError(t, err)
	}

	require.Equal(t, int64(3), rb.NumRows())
	arr := rb.Column(0).(*columnarv2.UTF8)
	require.False(t, arr.IsNull(0))
	require.True(t, arr.IsNull(1))
	require.False(t, arr.IsNull(2))
	require.Equal(t, []byte("a"), arr.Get(0))
	require.Equal(t, []byte("b"), arr.Get(2))
}
