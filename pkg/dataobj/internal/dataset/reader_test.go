package dataset

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/memory"
)

var readerTestStrings = []string{
	"reader string 1",
	"reader string 2",
	"",
	"reader string 4",
	"reader string 5",
	"reader string 1",
	"reader string 2",
	"",
	"reader string 4",
	"reader string 5",
	"reader string 1",
	"reader string 2",
	"",
	"reader string 4",
	"reader string 5",
}

func buildReaderTestColumn(t testing.TB, name string, values []string) *MemColumn {
	t.Helper()

	builder, err := NewColumnBuilder("", BuilderOptions{
		PageSizeHint: 2 * 1024 * 1024,
		Type:         ColumnType{Physical: datasetmd.PHYSICAL_TYPE_BINARY, Logical: name},
		Compression:  datasetmd.COMPRESSION_TYPE_ZSTD,
		Encoding:     datasetmd.ENCODING_TYPE_PLAIN,
	})
	require.NoError(t, err)

	for i, v := range values {
		require.NoError(t, builder.Append(i, BinaryValue([]byte(v))))
	}

	col, err := builder.Flush()
	require.NoError(t, err)
	return col
}

func TestReader_ReadNotInitialized(t *testing.T) {
	alloc := memory.NewAllocator(nil)
	col := buildReaderTestColumn(t, "data", readerTestStrings)

	r := NewReader(ReaderOptions{
		Columns:   []Column{col},
		BatchSize: 4,
		Allocator: alloc,
	})

	_, err := r.Read(context.Background(), alloc, 4)
	require.Error(t, err, "Read on unopened reader should fail")
}

func TestReader_OpenIdempotent(t *testing.T) {
	alloc := memory.NewAllocator(nil)
	col := buildReaderTestColumn(t, "data", readerTestStrings)

	r := NewReader(ReaderOptions{
		Columns:   []Column{col},
		BatchSize: 4,
		Allocator: alloc,
	})

	require.NoError(t, r.Open(context.Background()))
	require.NoError(t, r.Open(context.Background()), "second Open should be a no-op")
}

func TestReader_ReadSingleBatch(t *testing.T) {
	alloc := memory.NewAllocator(nil)
	col := buildReaderTestColumn(t, "data", readerTestStrings)

	r := NewReader(ReaderOptions{
		Columns:   []Column{col},
		BatchSize: len(readerTestStrings),
		Allocator: alloc,
	})
	require.NoError(t, r.Open(context.Background()))

	batch, err := r.Read(context.Background(), alloc, len(readerTestStrings))
	require.NoError(t, err)
	require.NotNil(t, batch)
	require.Equal(t, int64(1), batch.NumCols())
	require.Greater(t, batch.NumRows(), int64(0))
}

func TestReader_ReadMultipleBatches(t *testing.T) {
	alloc := memory.NewAllocator(nil)
	col := buildReaderTestColumn(t, "data", readerTestStrings)

	batchSize := 4
	r := NewReader(ReaderOptions{
		Columns:   []Column{col},
		BatchSize: batchSize,
		Allocator: alloc,
	})
	require.NoError(t, r.Open(context.Background()))

	var totalRows int64
	for {
		batch, err := r.Read(context.Background(), alloc, batchSize)
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		require.NotNil(t, batch)
		totalRows += batch.NumRows()
	}
	require.Equal(t, int64(len(readerTestStrings)), totalRows)
}

func TestReader_ReadMultipleColumns(t *testing.T) {
	alloc := memory.NewAllocator(nil)

	col1 := buildReaderTestColumn(t, "data1", readerTestStrings)
	col2 := buildReaderTestColumn(t, "data2", readerTestStrings)

	r := NewReader(ReaderOptions{
		Columns:   []Column{col1, col2},
		BatchSize: len(readerTestStrings),
		Allocator: alloc,
	})
	require.NoError(t, r.Open(context.Background()))

	batch, err := r.Read(context.Background(), alloc, len(readerTestStrings))
	require.NoError(t, err)
	require.NotNil(t, batch)
	require.Equal(t, int64(2), batch.NumCols())
}

func TestReader_ReadCountSmallerThanBatch(t *testing.T) {
	alloc := memory.NewAllocator(nil)
	col := buildReaderTestColumn(t, "data", readerTestStrings)

	r := NewReader(ReaderOptions{
		Columns:   []Column{col},
		BatchSize: len(readerTestStrings),
		Allocator: alloc,
	})
	require.NoError(t, r.Open(context.Background()))

	batch, err := r.Read(context.Background(), alloc, 2)
	require.NoError(t, err)
	require.NotNil(t, batch)
	require.LessOrEqual(t, batch.NumRows(), int64(2))
}

func TestReader_Reset(t *testing.T) {
	alloc := memory.NewAllocator(nil)
	col := buildReaderTestColumn(t, "data", readerTestStrings)

	r := NewReader(ReaderOptions{
		Columns:   []Column{col},
		BatchSize: 4,
		Allocator: alloc,
	})
	require.NoError(t, r.Open(context.Background()))

	newCol := buildReaderTestColumn(t, "data", readerTestStrings[:5])
	r.Reset(ReaderOptions{
		Columns:   []Column{newCol},
		BatchSize: 8,
		Allocator: alloc,
	})

	_, err := r.Read(context.Background(), alloc, 4)
	require.Error(t, err, "Read after Reset without Open should fail")

	require.NoError(t, r.Open(context.Background()))

	batch, err := r.Read(context.Background(), alloc, 8)
	require.NoError(t, err)
	require.NotNil(t, batch)
}

func TestReader_SchemaMatchesColumns(t *testing.T) {
	alloc := memory.NewAllocator(nil)
	col1 := buildReaderTestColumn(t, "data1", readerTestStrings)

	builder2, err := NewColumnBuilder("tag2", BuilderOptions{
		PageSizeHint: 128,
		Type:         ColumnType{Physical: datasetmd.PHYSICAL_TYPE_BINARY, Logical: "label"},
		Compression:  datasetmd.COMPRESSION_TYPE_SNAPPY,
		Encoding:     datasetmd.ENCODING_TYPE_PLAIN,
	})
	require.NoError(t, err)
	for i, v := range readerTestStrings {
		require.NoError(t, builder2.Append(i, BinaryValue([]byte(v))))
	}
	col2, err := builder2.Flush()
	require.NoError(t, err)

	r := NewReader(ReaderOptions{
		Columns:   []Column{col1, col2},
		BatchSize: len(readerTestStrings),
		Allocator: alloc,
	})
	require.NoError(t, r.Open(context.Background()))

	schema := r.schema
	require.Equal(t, 2, schema.NumColumns())
	require.Equal(t, "data1/", schema.Column(0).Name)
	require.Equal(t, "label/tag2", schema.Column(1).Name)
}

func TestReader_ReadEOFOnEmpty(t *testing.T) {
	alloc := memory.NewAllocator(nil)

	builder, err := NewColumnBuilder("", BuilderOptions{
		PageSizeHint: 128,
		Type:         ColumnType{Physical: datasetmd.PHYSICAL_TYPE_BINARY, Logical: "data"},
		Compression:  datasetmd.COMPRESSION_TYPE_SNAPPY,
		Encoding:     datasetmd.ENCODING_TYPE_PLAIN,
	})
	require.NoError(t, err)

	// Append a single value so we have a valid column, then read past it.
	require.NoError(t, builder.Append(0, BinaryValue([]byte("only"))))
	col, err := builder.Flush()
	require.NoError(t, err)

	r := NewReader(ReaderOptions{
		Columns:   []Column{col},
		BatchSize: 10,
		Allocator: alloc,
	})
	require.NoError(t, r.Open(context.Background()))

	// First read should succeed.
	_, err = r.Read(context.Background(), alloc, 10)
	require.NoError(t, err)

	// Second read should EOF.
	_, err = r.Read(context.Background(), alloc, 10)
	require.ErrorIs(t, err, io.EOF)
}

func BenchmarkReader_Read(b *testing.B) {
	totalValues := 64 * 1024
	values := make([]string, totalValues)
	for i := range values {
		values[i] = fmt.Sprintf("value %d [%s]", i, strings.Repeat("A", 100))
	}

	col := buildReaderTestColumn(b, "data", values)

	alloc := memory.NewAllocator(nil)
	r := NewReader(ReaderOptions{
		Columns:   []Column{col},
		BatchSize: totalValues / 2,
		Allocator: alloc,
	})

	readerAlloc := memory.NewAllocator(alloc)
	for b.Loop() {
		r.Reset(ReaderOptions{
			Columns:   []Column{col},
			BatchSize: totalValues / 2,
			Allocator: alloc,
		})
		_ = r.Open(context.Background())
		for {
			_, err := r.Read(context.Background(), readerAlloc, 8192)
			if err == io.EOF {
				break
			}
			readerAlloc.Reclaim()
		}
		alloc.Reclaim()
	}
	b.SetBytes(int64(col.Desc.UncompressedSize))
	b.ReportMetric(float64(totalValues*b.N)/b.Elapsed().Seconds(), "values/s")
}
