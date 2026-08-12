package dataset_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/columnar/columnartest"
	"github.com/grafana/loki/v3/pkg/columnar/types"
	"github.com/grafana/loki/v3/pkg/dataset"
	"github.com/grafana/loki/v3/pkg/dataset/array"
	"github.com/grafana/loki/v3/pkg/dataset/buffer"
	datasetencoding "github.com/grafana/loki/v3/pkg/dataset/encoding"
	"github.com/grafana/loki/v3/pkg/dataset/encoding/wirecodec"
	"github.com/grafana/loki/v3/pkg/dataset/layout"
	"github.com/grafana/loki/v3/pkg/expr"
	"github.com/grafana/loki/v3/pkg/memory"
)

func TestBuildOpen(t *testing.T) {
	t.Run("round trips a dataset", func(t *testing.T) {
		var (
			alloc memory.Allocator
			store buffer.MemoryStore
		)

		rows := []columnar.Array{
			buildRecord(t, &alloc, 1000, map[string]string{"service": "web", "env": "prod"}, "hello world"),
			buildRecord(t, &alloc, 2000, map[string]string{"service": "api"}, "request started"),
			buildRecord(t, &alloc, 3000, map[string]string{"env": "dev"}, "debug info"),
		}
		dset := writeDataset(t, &alloc, &store, rows...)
		expected := readAllDataset(t, &alloc, &store, dset)

		actual := roundTripDataset(t, &alloc, &store, dset)

		columnartest.RequireArraysEqual(t, expected, actual, memory.Bitmap{})
	})

	t.Run("round trips an empty dataset", func(t *testing.T) {
		var (
			alloc memory.Allocator
			store buffer.MemoryStore
		)

		dset := writeDataset(t, &alloc, &store)

		actual, _ := reopenDataset(t, &store, dset)

		require.Equal(t, 0, actual.Layout.Len())
		require.Equal(t, dset.Type.Kind(), actual.Type.Kind())
	})

	t.Run("deduplicates shared buffers", func(t *testing.T) {
		var (
			alloc memory.Allocator
			store buffer.MemoryStore
		)

		dset, expected := buildSharedBufferDataset(t, &alloc, &store)

		actual := roundTripDataset(t, &alloc, &store, dset)

		columnartest.RequireArraysEqual(t, expected, actual, memory.Bitmap{})
	})
}

func TestFile_ReadRange(t *testing.T) {
	var (
		file = buildTestFile(t)
		raw  = readFile(t, file)
	)

	t.Run("reads across regions", func(t *testing.T) {
		bodyOffset := fileBodyOffset(t, raw)
		trailerOffset := len(raw) - len("DSET")
		tests := []struct {
			name   string
			offset int
			length int
		}{
			{name: "header", offset: 0, length: 5},
			{name: "header to body", offset: bodyOffset - 2, length: 4},
			{name: "body to trailer", offset: trailerOffset - 2, length: 4},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				actual := make([]byte, tt.length)

				n, err := file.ReadRange(t.Context(), actual, int64(tt.offset))

				require.NoError(t, err)
				require.Equal(t, tt.length, n)
				require.Equal(t, raw[tt.offset:tt.offset+tt.length], actual)
			})
		}
	})

	t.Run("returns a partial read at EOF", func(t *testing.T) {
		actual := make([]byte, 4)

		n, err := file.ReadRange(t.Context(), actual, file.Len()-2)

		require.ErrorIs(t, err, io.EOF)
		require.Equal(t, 2, n)
		require.Equal(t, raw[len(raw)-2:], actual[:n])
	})

	t.Run("returns EOF at the end", func(t *testing.T) {
		actual := make([]byte, 4)

		n, err := file.ReadRange(t.Context(), actual, file.Len())

		require.ErrorIs(t, err, io.EOF)
		require.Zero(t, n)
	})

	t.Run("rejects a negative offset", func(t *testing.T) {
		_, err := file.ReadRange(t.Context(), make([]byte, 4), -1)

		require.ErrorContains(t, err, "invalid offset")
	})
}

func TestOpen_RejectsMalformedHeaders(t *testing.T) {
	raw := readFile(t, buildTestFile(t))
	tests := []struct {
		name   string
		mutate func([]byte) []byte
		err    string
	}{
		{
			name: "truncated file",
			mutate: func(data []byte) []byte {
				return data[:12]
			},
			err: "file too small",
		},
		{
			name: "invalid leading magic",
			mutate: func(data []byte) []byte {
				data[0] = 'X'
				return data
			},
			err: "invalid leading magic",
		},
		{
			name: "invalid trailing magic",
			mutate: func(data []byte) []byte {
				data[len(data)-1] = 'X'
				return data
			},
			err: "invalid trailing magic",
		},
		{
			name: "unsupported version",
			mutate: func(data []byte) []byte {
				data[4] = 0xff
				return data
			},
			err: "unsupported version",
		},
		{
			name: "oversized metadata",
			mutate: func(data []byte) []byte {
				binary.LittleEndian.PutUint32(data[5:9], ^uint32(0))
				return data
			},
			err: "metadata size 4294967295 exceeds remaining file size",
		},
		{
			name: "oversized buffer index",
			mutate: func(data []byte) []byte {
				indexSizeOffset := 9 + int(binary.LittleEndian.Uint32(data[5:9]))
				binary.LittleEndian.PutUint32(data[indexSizeOffset:indexSizeOffset+4], ^uint32(0))
				return data
			},
			err: "buffer index size 4294967295 exceeds remaining file size",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			corrupted := tt.mutate(bytes.Clone(raw))

			_, _, err := dataset.Open(t.Context(), newRangeReader(corrupted))

			require.ErrorContains(t, err, tt.err)
		})
	}
}

func TestOpen_RejectsBufferOutsideBody(t *testing.T) {
	corrupted := fileWithBufferOutsideBody(t)

	_, _, err := dataset.Open(t.Context(), newRangeReader(corrupted))

	require.ErrorContains(t, err, "exceeds body size")
}

func fileWithBufferOutsideBody(t *testing.T) []byte {
	t.Helper()

	raw := readFile(t, buildTestFile(t))
	metadata, _, body := splitFile(t, raw)

	var manifest wirecodec.Manifest
	require.NoError(t, manifest.UnmarshalBinary(metadata))

	bodyLength := int64(len(body) - len("DSET"))
	postings := []datasetencoding.BufferPosting{
		{BufferID: manifest.DictionaryBuffer, Offset: bodyLength, Length: 1},
		{BufferID: manifest.TypeBuffer, Offset: bodyLength, Length: 1},
		{BufferID: manifest.LayoutBuffer, Offset: bodyLength, Length: 1},
	}

	var alloc memory.Allocator
	index, err := datasetencoding.BuildBufferIndex(t.Context(), &alloc, postings)
	require.NoError(t, err)
	return encodeFile(t, metadata, index, body)
}

func readAllDataset(t *testing.T, alloc *memory.Allocator, source buffer.Source, dset dataset.Dataset) columnar.Array {
	t.Helper()

	result, err := readDataset(t, alloc, source, dataset.ReaderOptions{
		Dataset:    dset,
		Projection: &expr.Identity{},
	})
	require.NoError(t, err)
	return result
}

func roundTripDataset(t *testing.T, alloc *memory.Allocator, store buffer.Store, dset dataset.Dataset) columnar.Array {
	t.Helper()

	reopened, source := reopenDataset(t, store, dset)
	return readAllDataset(t, alloc, source, reopened)
}

func reopenDataset(t *testing.T, store buffer.Store, dset dataset.Dataset) (dataset.Dataset, buffer.Source) {
	t.Helper()

	file, err := dataset.Build(t.Context(), dset, store)
	require.NoError(t, err)

	reopened, source, err := dataset.Open(t.Context(), newRangeReader(readFile(t, file)))
	require.NoError(t, err)
	return reopened, source
}

func buildSharedBufferDataset(t *testing.T, alloc *memory.Allocator, store buffer.Store) (dataset.Dataset, columnar.Array) {
	t.Helper()

	writer, err := layout.NewWriter(
		alloc,
		store,
		&layout.SpecArray{Spec: &array.SpecPlain{}},
		&types.Int64{},
	)
	require.NoError(t, err)

	values := columnartest.Array(t, types.KindInt64, alloc, int64(1), int64(2), int64(3))
	require.NoError(t, writer.Append(t.Context(), values))
	shared, err := writer.Flush(t.Context())
	require.NoError(t, err)

	typ := &types.Struct{Fields: []types.StructField{
		{Name: "left", Type: &types.Int64{}},
		{Name: "right", Type: &types.Int64{}},
	}}
	dset := dataset.Dataset{
		Type: typ,
		Layout: &layout.Struct{
			Type:   typ,
			Fields: []layout.Layout{shared, shared},
		},
	}
	expected := columnar.NewStruct(
		columnar.NewSchema([]columnar.Column{{Name: "left"}, {Name: "right"}}),
		[]columnar.Array{values, values},
		values.Len(),
		memory.Bitmap{},
	)
	return dset, expected
}

func buildTestFile(t *testing.T) *dataset.File {
	t.Helper()

	var (
		alloc memory.Allocator
		store buffer.MemoryStore
	)

	dset := writeDataset(t, &alloc, &store,
		buildRecord(t, &alloc, 1000, map[string]string{"service": "web"}, "hello"),
		buildRecord(t, &alloc, 2000, map[string]string{"service": "api"}, "world"),
	)
	file, err := dataset.Build(t.Context(), dset, &store)
	require.NoError(t, err)
	return file
}

func readFile(t *testing.T, file *dataset.File) []byte {
	t.Helper()

	data := make([]byte, file.Len())
	n, err := file.ReadAt(data, 0)
	require.NoError(t, err)
	require.Equal(t, len(data), n)
	return data
}

func fileBodyOffset(t *testing.T, data []byte) int {
	t.Helper()

	var (
		metadataSize    = int(binary.LittleEndian.Uint32(data[5:9]))
		indexSizeOffset = 9 + metadataSize
		indexSize       = int(binary.LittleEndian.Uint32(data[indexSizeOffset : indexSizeOffset+4]))
	)
	return indexSizeOffset + 4 + indexSize
}

func splitFile(t *testing.T, data []byte) (metadata, index, body []byte) {
	t.Helper()

	var (
		metadataSize    = int(binary.LittleEndian.Uint32(data[5:9]))
		indexSizeOffset = 9 + metadataSize
		indexSize       = int(binary.LittleEndian.Uint32(data[indexSizeOffset : indexSizeOffset+4]))
	)

	metadata = data[9 : 9+metadataSize]
	index = data[indexSizeOffset+4 : indexSizeOffset+4+indexSize]
	body = data[indexSizeOffset+4+indexSize:]
	return metadata, index, body
}

func encodeFile(t *testing.T, metadata, index, body []byte) []byte {
	t.Helper()

	var result bytes.Buffer

	_, _ = result.WriteString("DSET")
	_ = result.WriteByte(0x01)

	require.NoError(t, binary.Write(&result, binary.LittleEndian, uint32(len(metadata))))
	_, _ = result.Write(metadata)

	require.NoError(t, binary.Write(&result, binary.LittleEndian, uint32(len(index))))
	_, _ = result.Write(index)

	_, _ = result.Write(body)

	return result.Bytes()
}

type rangeReader struct {
	data []byte
}

func newRangeReader(data []byte) *rangeReader {
	return &rangeReader{data: data}
}

func (r *rangeReader) ReadRange(_ context.Context, p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, fmt.Errorf("invalid offset: %d", off)
	}
	if off >= int64(len(r.data)) {
		if len(p) == 0 {
			return 0, nil
		}
		return 0, io.EOF
	}
	n := copy(p, r.data[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func (r *rangeReader) Len() int64 {
	return int64(len(r.data))
}

var _ datasetencoding.RangeReader = (*rangeReader)(nil)
