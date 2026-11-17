package stats

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/internal/columnar"
)

// futureColumnSectionBuilder builds a stats section with one row in a column
// of a type that this code does not know, as a newer writer could.
type futureColumnSectionBuilder struct{}

func (futureColumnSectionBuilder) Type() dataobj.SectionType { return sectionType }

func (futureColumnSectionBuilder) Reset() {}

func (futureColumnSectionBuilder) Flush(w dataobj.SectionWriter) (int64, error) {
	var enc columnar.Encoder
	defer enc.Reset()
	builder, err := dataset.NewColumnBuilder("future_field", dataset.BuilderOptions{
		PageMaxRowCount: 100,
		Type:            dataset.ColumnType{Physical: datasetmd.PHYSICAL_TYPE_INT64, Logical: "future_field"},
		Encoding:        datasetmd.ENCODING_TYPE_DELTA,
		Compression:     datasetmd.COMPRESSION_TYPE_NONE,
	})
	if err != nil {
		return 0, err
	}
	_ = builder.Append(0, dataset.Int64Value(1))
	if err := encodeColumn(&enc, ColumnTypeInvalid, builder); err != nil {
		return 0, err
	}
	enc.SetTenant("acme")
	return enc.Flush(w)
}

func TestSection_Empty(t *testing.T) {
	t.Run("reports false for a section whose only column has a type that this code does not know", func(t *testing.T) {
		objBuilder := dataobj.NewBuilder(nil)
		require.NoError(t, objBuilder.Append(futureColumnSectionBuilder{}))
		obj, closer, err := objBuilder.Flush()
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, closer.Close()) })

		sec, err := Open(t.Context(), obj.Sections()[0])
		require.NoError(t, err)
		require.Empty(t, sec.Columns())
		require.False(t, sec.Empty())
	})
}
