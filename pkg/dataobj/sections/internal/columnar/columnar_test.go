package columnar_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/format"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/internal/columnar"
)

var testColumnOptions = dataset.BuilderOptions{
	PageSizeHint: 1024,
	Type: dataset.ColumnType{
		Physical: datasetmd.PHYSICAL_TYPE_BINARY,
		Logical:  "my-data",
	},
	Encoding:    datasetmd.ENCODING_TYPE_PLAIN,
	Compression: datasetmd.COMPRESSION_TYPE_NONE,
}

// Test runs a basic end-to-end test for columnar encoding. More involved tests
// are currently delegated to section packages.
func Test(t *testing.T) {
	values := []dataset.Value{
		dataset.BinaryValue([]byte("Hello, world!")),
		dataset.BinaryValue([]byte("Goodbye, world!")),
	}
	column := buildColumn(t, "test-column", testColumnOptions, values)

	obj := buildObject(t, []*dataset.MemColumn{column})

	expectRows := []dataset.Row{
		{Index: 0, Values: []dataset.Value{values[0]}},
		{Index: 1, Values: []dataset.Value{values[1]}},
	}
	rows, err := readDataset(t, obj)
	require.NoError(t, err)
	require.Equal(t, expectRows, rows)
}

func TestOpen(t *testing.T) {
	raw, section := serializeObject(t)
	withoutMetadata := len(raw) - len(format.FileMagic) - int(section.DataSize()) - int(section.MetadataSize())

	t.Run("returns an unexpected EOF error when the metadata region is missing", func(t *testing.T) {
		_, err := readDataset(t, truncateObject(t, raw, withoutMetadata))

		require.ErrorContains(t, err, "reading section metadata: read 0 of")
		require.ErrorIs(t, err, io.ErrUnexpectedEOF)
		require.NotErrorIs(t, err, io.EOF)
	})
}

func TestRowReader_Read(t *testing.T) {
	raw, section := serializeObject(t)
	withoutData := len(raw) - len(format.FileMagic) - int(section.DataSize())

	t.Run("returns an unexpected EOF error when the data region is missing", func(t *testing.T) {
		_, err := readDataset(t, truncateObject(t, raw, withoutData))

		require.ErrorContains(t, err, fmt.Sprintf("reading column 0: read 0 of %d bytes at offset 0: unexpected EOF", section.DataSize()))
		require.ErrorIs(t, err, io.ErrUnexpectedEOF)
		require.NotErrorIs(t, err, io.EOF)
	})
}

func TestDecoder_ReadPages(t *testing.T) {
	column := buildColumn(t, "test-column", testColumnOptions, []dataset.Value{dataset.BinaryValue([]byte("Hello, world!"))})
	reader := buildObject(t, []*dataset.MemColumn{column}).Sections()[0].Reader
	dataSize := uint64(reader.DataSize())

	readPages := func(t *testing.T, reader dataobj.SectionReader, pages ...*datasetmd.PageDesc) ([]dataset.PageData, error) {
		dec, err := columnar.NewDecoder(reader, columnar.FormatVersion)
		require.NoError(t, err)

		return result.Collect(dec.ReadPages(t.Context(), pages))
	}

	t.Run("returns the bytes of a page that ends at the end of the data region", func(t *testing.T) {
		got, err := readPages(t, reader, &datasetmd.PageDesc{DataOffset: dataSize - 1, DataSize: 1})

		require.NoError(t, err)
		rc, err := reader.DataRange(t.Context(), dataobj.RelativeByteRange{Offset: int64(dataSize) - 1, Length: 1})
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, rc.Close()) })
		want, err := io.ReadAll(rc)
		require.NoError(t, err)
		require.Equal(t, []dataset.PageData{want}, got)
	})

	t.Run("returns an error when a page is not inside the data region", func(t *testing.T) {
		tests := []struct {
			name  string
			pages []*datasetmd.PageDesc
			want  string
		}{
			{
				name:  "starts after the region",
				pages: []*datasetmd.PageDesc{{DataOffset: dataSize + 1, DataSize: 0}},
				want:  fmt.Sprintf("page 0 is outside the data region: offset=%d size=0 region length=%d", dataSize+1, dataSize),
			},
			{
				name:  "has an end that overflows",
				pages: []*datasetmd.PageDesc{{DataOffset: 1, DataSize: math.MaxUint64}},
				want:  fmt.Sprintf("page 0 is outside the data region: offset=1 size=%d region length=%d", uint64(math.MaxUint64), dataSize),
			},
			{
				name:  "ends one byte after the region and follows a page that is inside it",
				pages: []*datasetmd.PageDesc{{DataOffset: 0, DataSize: 1}, {DataOffset: dataSize - 1, DataSize: 2}},
				want:  fmt.Sprintf("page 1 is outside the data region: offset=%d size=2 region length=%d", dataSize-1, dataSize),
			},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				got, err := readPages(t, reader, tc.pages...)

				require.EqualError(t, err, tc.want)
				require.Empty(t, got)
			})
		}
	})

	t.Run("returns an error when the size of the data region is negative", func(t *testing.T) {
		got, err := readPages(t, negativeDataSizeReader{reader}, &datasetmd.PageDesc{DataOffset: 0, DataSize: 1})

		require.EqualError(t, err, "page 0 is outside the data region: offset=0 size=1 region length=-1")
		require.Empty(t, got)
	})
}

func TestMakeDatasetPage(t *testing.T) {
	t.Run("takes the compressed size from the data size", func(t *testing.T) {
		page := columnar.MakeDatasetPage(nil, &datasetmd.PageDesc{DataSize: 1234})

		require.Equal(t, 1234, page.PageDesc().CompressedSize)
	})
}

func buildColumn(t *testing.T, tag string, opts dataset.BuilderOptions, values []dataset.Value) *dataset.MemColumn {
	t.Helper()

	columnBuilder, err := dataset.NewColumnBuilder(tag, opts)
	require.NoError(t, err)

	for i, value := range values {
		require.NoError(t, columnBuilder.Append(i, value))
	}

	column, err := columnBuilder.Flush()
	require.NoError(t, err)
	return column
}

func buildObject(t *testing.T, columns []*dataset.MemColumn) *dataobj.Object {
	t.Helper()

	testSectionType := dataobj.SectionType{
		Namespace: "my-namespace",
		Kind:      "my-kind",
		Version:   columnar.FormatVersion,
	}

	objBuilder := dataobj.NewBuilder(nil)
	sectionBuilder := newColumnarBuilder(t, testSectionType, columns)

	err := objBuilder.Append(sectionBuilder)
	require.NoError(t, err)

	obj, closer, err := objBuilder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })

	require.Len(t, obj.Sections(), 1, "expected exactly one section")
	require.Equal(t, testSectionType, obj.Sections()[0].Type)
	return obj
}

// serializeObject returns the bytes of an object with one section, and the reader of that section.
// The bytes are, in order: the header, the file metadata, the metadata region, the data region and
// the magic.
func serializeObject(t *testing.T) ([]byte, dataobj.SectionReader) {
	t.Helper()

	column := buildColumn(t, "test-column", testColumnOptions, []dataset.Value{
		dataset.BinaryValue([]byte("Hello, world!")),
		dataset.BinaryValue([]byte("Goodbye, world!")),
	})
	obj := buildObject(t, []*dataset.MemColumn{column})

	rc, err := obj.Reader(t.Context())
	require.NoError(t, err)
	raw, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.NoError(t, rc.Close())

	return raw, obj.Sections()[0].Reader
}

// truncateObject returns the object made of the first size bytes of raw.
func truncateObject(t *testing.T, raw []byte, size int) *dataobj.Object {
	t.Helper()

	obj, err := dataobj.FromReaderAt(bytes.NewReader(raw[:size]), int64(size))
	require.NoError(t, err)
	return obj
}

// readDataset reads the rows of the first section of obj. It returns the rows that it read before
// the first error.
func readDataset(t *testing.T, obj *dataobj.Object) ([]dataset.Row, error) {
	t.Helper()

	// The timeout makes a read that retries forever fail the test instead of hanging it.
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	rawSection := obj.Sections()[0]

	dec, err := columnar.NewDecoder(rawSection.Reader, rawSection.Type.Version)
	require.NoError(t, err)

	sec, err := columnar.Open(ctx, rawSection.Tenant, dec)
	if err != nil {
		return nil, err
	}
	require.Len(t, sec.Columns(), 1, "expected exactly one column")

	// Convert our section into a dataset and read all rows.
	dset, err := columnar.MakeDataset(sec, sec.Columns())
	require.NoError(t, err)

	reader := dataset.NewRowReader(dataset.RowReaderOptions{
		Dataset:          dset,
		ProjectedColumns: dset.Columns(),
	})
	if err := reader.Open(ctx); err != nil {
		return nil, err
	}

	var rows []dataset.Row
	for {
		buf := make([]dataset.Row, 128)
		n, err := reader.Read(ctx, buf)
		rows = append(rows, buf[:n]...)

		if errors.Is(err, io.EOF) {
			return rows, nil
		} else if err != nil {
			return rows, err
		}
	}
}

// columnarBuilder is a [dataset.SectionBuilder] which builder a columnar
// section from a set of static in-memory columns.
type columnarBuilder struct {
	sectionType dataobj.SectionType
	columns     []*dataset.MemColumn
	t           *testing.T
}

func newColumnarBuilder(t *testing.T, secType dataobj.SectionType, columns []*dataset.MemColumn) *columnarBuilder {
	t.Helper()

	return &columnarBuilder{
		sectionType: secType,
		columns:     columns,
		t:           t,
	}
}

func (b *columnarBuilder) Type() dataobj.SectionType { return b.sectionType }

func (b *columnarBuilder) Flush(w dataobj.SectionWriter) (int64, error) {
	var enc columnar.Encoder

	for _, column := range b.columns {
		columnEnc, err := enc.OpenColumn(column.ColumnDesc())
		require.NoError(b.t, err)

		for _, page := range column.Pages {
			require.NoError(b.t, columnEnc.AppendPage(page))
		}
		require.NoError(b.t, columnEnc.Commit())
	}

	return enc.Flush(w)
}

func (b *columnarBuilder) Reset() {
	// No-op
}

// negativeDataSizeReader returns a negative DataSize. A real section reader does this for a data
// region larger than math.MaxInt64 bytes.
type negativeDataSizeReader struct{ dataobj.SectionReader }

func (negativeDataSizeReader) DataSize() int64 { return -1 }
