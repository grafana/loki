package columnar_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"math"
	"strings"
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

var (
	supportedVersions   = []uint32{columnar.FormatVersionDescriptorChecksum, columnar.FormatVersionPageChecksum}
	unsupportedVersions = []uint32{0, 1, 3, 5}
)

// Test runs a basic end-to-end test for columnar encoding. More involved tests
// are currently delegated to section packages.
func Test(t *testing.T) {
	values := []dataset.Value{
		dataset.BinaryValue([]byte("Hello, world!")),
		dataset.BinaryValue([]byte("Goodbye, world!")),
	}
	column := buildColumn(t, "test-column", testColumnOptions, values)

	expectRows := []dataset.Row{
		{Index: 0, Values: []dataset.Value{values[0]}},
		{Index: 1, Values: []dataset.Value{values[1]}},
	}

	for _, version := range supportedVersions {
		t.Run(fmt.Sprintf("reads the rows of a section of format version %d", version), func(t *testing.T) {
			obj := buildObject(t, version, []*dataset.MemColumn{column})

			rows, err := readDataset(t, obj)

			require.NoError(t, err)
			require.Equal(t, expectRows, rows)
		})
	}

	t.Run("returns an error when a byte of a page of format version 4 changes", func(t *testing.T) {
		raw := objectBytes(t, buildObject(t, columnar.FormatVersionPageChecksum, []*dataset.MemColumn{column}))
		raw[bytes.Index(raw, []byte("Hello"))] ^= 0xff

		_, err := readDataset(t, truncateObject(t, raw, len(raw)))

		require.ErrorContains(t, err, "invalid CRC32 checksum")
	})
}

func TestNewDecoder(t *testing.T) {
	for _, version := range unsupportedVersions {
		t.Run(fmt.Sprintf("returns an error for format version %d", version), func(t *testing.T) {
			_, err := columnar.NewDecoder(nil, version)

			require.EqualError(t, err, fmt.Sprintf("unsupported format version %d", version))
		})
	}
}

func TestEncoder(t *testing.T) {
	column := buildColumn(t, "test-column", testColumnOptions, []dataset.Value{
		dataset.BinaryValue([]byte("Hello, world!")),
		dataset.BinaryValue([]byte("Goodbye, world!")),
	})

	t.Run("keeps the checksum of a page in its descriptor in format version 2", func(t *testing.T) {
		obj := buildObject(t, columnar.FormatVersionDescriptorChecksum, []*dataset.MemColumn{column})

		descs, pages := readColumnPages(t, obj)

		require.Len(t, descs, len(column.Pages))
		for i, desc := range descs {
			require.Equal(t, column.Pages[i].Desc.CRC32, desc.DeprecatedCrc32)
			require.Equal(t, uint64(len(column.Pages[i].Data)), desc.DataSize)
			require.Equal(t, column.Pages[i].Data, pages[i])
		}
	})

	t.Run("keeps the checksum of a page in the page in format version 4", func(t *testing.T) {
		obj := buildObject(t, columnar.FormatVersionPageChecksum, []*dataset.MemColumn{column})

		descs, pages := readColumnPages(t, obj)

		require.Len(t, descs, len(column.Pages))
		for i, desc := range descs {
			page := column.Pages[i]
			require.Zero(t, desc.DeprecatedCrc32)
			require.Equal(t, uint64(len(page.Data)+crc32.Size), desc.DataSize)
			require.Equal(t, dataset.PageData(binary.LittleEndian.AppendUint32(bytes.Clone(page.Data), page.Desc.CRC32)), pages[i])
		}
	})

	t.Run("starts the metadata region with the format version in format version 2", func(t *testing.T) {
		obj := buildObject(t, columnar.FormatVersionDescriptorChecksum, []*dataset.MemColumn{column})

		first, err := obj.Sections()[0].Reader.MetadataRanges(t.Context(), []dataobj.RelativeByteRange{{Offset: 0, Length: 1}})

		require.NoError(t, err)
		require.Equal(t, [][]byte{{byte(columnar.FormatVersionDescriptorChecksum)}}, first)
		require.Equal(t, uint64(1), firstColumnMetadataOffset(t, obj))
	})

	t.Run("starts the metadata region with the column metadata in format version 4", func(t *testing.T) {
		obj := buildObject(t, columnar.FormatVersionPageChecksum, []*dataset.MemColumn{column})

		require.Zero(t, firstColumnMetadataOffset(t, obj))
	})

	t.Run("places the pages of format version 4 one after the other", func(t *testing.T) {
		large := strings.Repeat("x", 2*testColumnOptions.PageSizeHint)
		manyPages := buildColumn(t, "test-column", testColumnOptions, []dataset.Value{
			dataset.BinaryValue([]byte(large)),
			dataset.BinaryValue([]byte(large)),
			dataset.BinaryValue([]byte(large)),
		})
		require.Greater(t, len(manyPages.Pages), 1)
		obj := buildObject(t, columnar.FormatVersionPageChecksum, []*dataset.MemColumn{manyPages})

		descs, pages := readColumnPages(t, obj)

		require.Len(t, descs, len(manyPages.Pages))
		var offset uint64
		for i, desc := range descs {
			require.Equal(t, offset, desc.DataOffset)
			require.Len(t, pages[i], len(manyPages.Pages[i].Data)+crc32.Size)
			offset += desc.DataSize
		}
	})

	for _, version := range unsupportedVersions {
		t.Run(fmt.Sprintf("returns an error for format version %d, which it cannot write", version), func(t *testing.T) {
			enc := columnar.NewEncoder()

			err := enc.SetFormatVersion(version)

			require.EqualError(t, err, fmt.Sprintf("unsupported format version %d", version))
		})
	}

	t.Run("returns an error when the format version is set while a column is open", func(t *testing.T) {
		enc := columnar.NewEncoder()
		_, err := enc.OpenColumn(column.ColumnDesc())
		require.NoError(t, err)

		err = enc.SetFormatVersion(columnar.FormatVersionPageChecksum)

		require.EqualError(t, err, "format version set after opening a column")
	})

	t.Run("returns an error when the format version is set after committing a column", func(t *testing.T) {
		enc := columnar.NewEncoder()
		columnEnc, err := enc.OpenColumn(column.ColumnDesc())
		require.NoError(t, err)
		for _, page := range column.Pages {
			require.NoError(t, columnEnc.AppendPage(page))
		}
		require.NoError(t, columnEnc.Commit())

		err = enc.SetFormatVersion(columnar.FormatVersionPageChecksum)

		require.EqualError(t, err, "format version set after opening a column")
	})

	t.Run("returns an error when a page has its checksum in its data", func(t *testing.T) {
		enc := columnar.NewEncoder()
		columnEnc, err := enc.OpenColumn(column.ColumnDesc())
		require.NoError(t, err)

		err = columnEnc.AppendPage(&dataset.MemPage{Desc: dataset.PageDesc{FormatVersion: columnar.FormatVersionPageChecksum}})

		require.EqualError(t, err, "page has its checksum in its data")
	})
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
	reader := buildObject(t, columnar.DefaultFormatVersion, []*dataset.MemColumn{column}).Sections()[0].Reader
	dataSize := uint64(reader.DataSize())

	readPages := func(t *testing.T, reader dataobj.SectionReader, pages ...*datasetmd.PageDesc) ([]dataset.PageData, error) {
		dec, err := columnar.NewDecoder(reader, columnar.DefaultFormatVersion)
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

	t.Run("returns an error when the uncompressed size of a page does not fit in an int", func(t *testing.T) {
		tooLarge := uint64(math.MaxInt) + 1
		got, err := readPages(t, reader, &datasetmd.PageDesc{DataOffset: 0, DataSize: 1, UncompressedSize: tooLarge})

		require.EqualError(t, err, fmt.Sprintf("page 0 has an uncompressed size that does not fit in an int: size=%d", tooLarge))
		require.Empty(t, got)
	})

	t.Run("returns an error when the size of the data region is negative", func(t *testing.T) {
		got, err := readPages(t, negativeDataSizeReader{reader}, &datasetmd.PageDesc{DataOffset: 0, DataSize: 1})

		require.EqualError(t, err, "page 0 is outside the data region: offset=0 size=1 region length=-1")
		require.Empty(t, got)
	})
}

func TestMakeDatasetPage(t *testing.T) {
	newDecoder := func(t *testing.T, version uint32) *columnar.Decoder {
		dec, err := columnar.NewDecoder(nil, version)
		require.NoError(t, err)
		return dec
	}

	for _, version := range supportedVersions {
		t.Run(fmt.Sprintf("takes the compressed size from the data size for format version %d", version), func(t *testing.T) {
			page := columnar.MakeDatasetPage(newDecoder(t, version), &datasetmd.PageDesc{DataSize: 1234})

			require.Equal(t, 1234, page.PageDesc().CompressedSize)
		})
	}

	for _, version := range supportedVersions {
		t.Run(fmt.Sprintf("sets the format version %d of the decoder on the page", version), func(t *testing.T) {
			page := columnar.MakeDatasetPage(newDecoder(t, version), &datasetmd.PageDesc{})

			require.Equal(t, version, page.PageDesc().FormatVersion)
		})
	}

	t.Run("takes the checksum from the descriptor", func(t *testing.T) {
		page := columnar.MakeDatasetPage(newDecoder(t, columnar.FormatVersionDescriptorChecksum), &datasetmd.PageDesc{DeprecatedCrc32: 99})

		require.Equal(t, uint32(99), page.PageDesc().CRC32)
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

func buildObject(t *testing.T, version uint32, columns []*dataset.MemColumn) *dataobj.Object {
	t.Helper()

	testSectionType := dataobj.SectionType{
		Namespace: "my-namespace",
		Kind:      "my-kind",
		Version:   version,
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
	obj := buildObject(t, columnar.DefaultFormatVersion, []*dataset.MemColumn{column})
	return objectBytes(t, obj), obj.Sections()[0].Reader
}

func objectBytes(t *testing.T, obj *dataobj.Object) []byte {
	t.Helper()

	rc, err := obj.Reader(t.Context())
	require.NoError(t, err)
	raw, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.NoError(t, rc.Close())
	return raw
}

// readColumnPages returns the descriptors and the bytes of the pages of the
// first column of the first section of obj.
func readColumnPages(t *testing.T, obj *dataobj.Object) ([]*datasetmd.PageDesc, []dataset.PageData) {
	t.Helper()

	rawSection := obj.Sections()[0]
	dec, err := columnar.NewDecoder(rawSection.Reader, rawSection.Type.Version)
	require.NoError(t, err)

	md, err := dec.SectionMetadata(t.Context())
	require.NoError(t, err)
	pageSets, err := result.Collect(dec.Pages(t.Context(), md.Columns[:1]))
	require.NoError(t, err)
	require.Len(t, pageSets, 1)

	pages, err := result.Collect(dec.ReadPages(t.Context(), pageSets[0]))
	require.NoError(t, err)
	return pageSets[0], pages
}

// firstColumnMetadataOffset returns the offset, in the metadata region, of the
// metadata of the first column of the first section of obj.
func firstColumnMetadataOffset(t *testing.T, obj *dataobj.Object) uint64 {
	t.Helper()

	rawSection := obj.Sections()[0]
	dec, err := columnar.NewDecoder(rawSection.Reader, rawSection.Type.Version)
	require.NoError(t, err)

	md, err := dec.SectionMetadata(t.Context())
	require.NoError(t, err)
	return md.Columns[0].ColumnMetadataOffset
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
	enc := columnar.NewEncoder()
	require.NoError(b.t, enc.SetFormatVersion(b.sectionType.Version))

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
