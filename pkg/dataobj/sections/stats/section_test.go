package stats

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/internal/columnar"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

// rawColumnsSectionBuilder builds a stats section with one row in a column of
// each of the given logical types. A type that this code does not know stands
// in for a column of a newer writer.
type rawColumnsSectionBuilder struct {
	logicalTypes []string
}

func (rawColumnsSectionBuilder) Type() dataobj.SectionType { return sectionType }

func (rawColumnsSectionBuilder) Reset() {}

func (b rawColumnsSectionBuilder) Flush(w dataobj.SectionWriter) (int64, error) {
	enc := columnar.NewEncoder()
	defer enc.Reset()
	for _, logicalType := range b.logicalTypes {
		builder, err := dataset.NewColumnBuilder(logicalType, dataset.BuilderOptions{
			PageMaxRowCount: 100,
			Type:            dataset.ColumnType{Physical: datasetmd.PHYSICAL_TYPE_BINARY, Logical: logicalType},
			Encoding:        datasetmd.ENCODING_TYPE_PLAIN,
			Compression:     datasetmd.COMPRESSION_TYPE_NONE,
		})
		if err != nil {
			return 0, err
		}
		if err := builder.Append(0, dataset.BinaryValue([]byte("objects/a"))); err != nil {
			return 0, err
		}
		if err := encodeColumn(enc, ColumnTypeInvalid, builder); err != nil {
			return 0, err
		}
	}
	enc.SetTenant("acme")
	return enc.Flush(w)
}

// openOnlySection opens the only section of the object that builder builds.
func openOnlySection(t *testing.T, builder dataobj.SectionBuilder) *Section {
	t.Helper()
	objBuilder := dataobj.NewBuilder(nil)
	require.NoError(t, objBuilder.Append(builder))
	obj, closer, err := objBuilder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, closer.Close()) })
	sections := obj.Sections()
	require.Len(t, sections, 1)
	sec, err := Open(t.Context(), sections[0])
	require.NoError(t, err)
	return sec
}

// statsWithRows returns a stats builder with one row.
func statsWithRows() *Builder {
	layout := streams.NewLayout(streams.DefaultShardLayout(), []string{"label:service_name"})
	b := NewBuilder(nil, ColumnarSectionEncoder(1024*1024, 10000), layout)
	b.SetTenant("acme")
	b.Append(Stat{ObjectPath: "objects/a", SortSchema: "label:service_name", Labels: map[string]string{"service_name": "svc1"}})
	return b
}

func TestSection_Empty(t *testing.T) {
	t.Run("reports true for a section of a layout section builder", func(t *testing.T) {
		sec := openOnlySection(t, NewLayoutSectionBuilder("acme", streams.NewLayout(streams.DefaultShardLayout(), nil)))
		require.True(t, sec.Empty())
	})

	t.Run("reports false for a section with rows", func(t *testing.T) {
		require.False(t, openOnlySection(t, statsWithRows()).Empty())
	})

	t.Run("reports false for a section whose only column has a type that this code does not know", func(t *testing.T) {
		sec := openOnlySection(t, rawColumnsSectionBuilder{logicalTypes: []string{"future_field"}})
		require.Empty(t, sec.Columns())
		require.False(t, sec.Empty())
	})
}

func TestSection_ObjectPathColumn(t *testing.T) {
	t.Run("returns the object_path column of a section with rows", func(t *testing.T) {
		column, ok, err := openOnlySection(t, statsWithRows()).ObjectPathColumn()
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, ColumnTypeObjectPath, column.Type)
	})

	t.Run("returns no column and no error for an empty section", func(t *testing.T) {
		sec := openOnlySection(t, NewLayoutSectionBuilder("acme", streams.NewLayout(streams.DefaultShardLayout(), nil)))
		column, ok, err := sec.ObjectPathColumn()
		require.NoError(t, err)
		require.False(t, ok)
		require.Nil(t, column)
	})

	t.Run("returns an error for a section whose only column has a type that this code does not know", func(t *testing.T) {
		_, _, err := openOnlySection(t, rawColumnsSectionBuilder{logicalTypes: []string{"future_field"}}).ObjectPathColumn()
		require.ErrorContains(t, err, "no object_path column")
	})

	t.Run("returns an error for a section with known columns and no object_path column", func(t *testing.T) {
		_, _, err := openOnlySection(t, rawColumnsSectionBuilder{logicalTypes: []string{ColumnTypeSortSchema.String()}}).ObjectPathColumn()
		require.ErrorContains(t, err, "no object_path column")
	})

	t.Run("returns an error for a section with two object_path columns", func(t *testing.T) {
		objectPath := ColumnTypeObjectPath.String()
		_, _, err := openOnlySection(t, rawColumnsSectionBuilder{logicalTypes: []string{objectPath, objectPath}}).ObjectPathColumn()
		require.ErrorContains(t, err, "more than one object_path column")
	})
}
