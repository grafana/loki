package columnar_test

import (
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
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
	require.Equal(t, expectRows, readDataset(t, obj))
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

func readDataset(t *testing.T, obj *dataobj.Object) []dataset.Row {
	t.Helper()

	rawSection := obj.Sections()[0]

	dec, err := columnar.NewDecoder(rawSection.Reader, rawSection.Type.Version)
	require.NoError(t, err)

	sec, err := columnar.Open(t.Context(), rawSection.Tenant, dec)
	require.NoError(t, err)
	require.Len(t, sec.Columns(), 1, "expected exactly one column")

	// Convert our section into a dataset and read all rows.
	dset, err := columnar.MakeDataset(sec, sec.Columns())
	require.NoError(t, err)

	reader := dataset.NewReader(dataset.ReaderOptions{
		Dataset: dset,
		Columns: dset.Columns(),
	})

	var rows []dataset.Row
	for {
		buf := make([]dataset.Row, 128)
		n, err := reader.Read(t.Context(), buf)
		rows = append(rows, buf[:n]...)

		if err != nil && errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			require.NoError(t, err)
		}
	}
	return rows
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
