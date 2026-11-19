package columnar_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/format"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
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
