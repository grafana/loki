package logs

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/internal/columnar"
)

func TestOpen(t *testing.T) {
	for _, version := range []uint32{columnar.FormatVersionDescriptorChecksum, legacySchemaSortVersion, columnar.FormatVersionPageChecksum} {
		t.Run(fmt.Sprintf("opens a section of version %d and reads its rows", version), func(t *testing.T) {
			objBuilder := dataobj.NewBuilder(nil)
			require.NoError(t, objBuilder.Append(versionSectionBuilder{version: version}))
			obj, closer, err := objBuilder.Flush()
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, closer.Close()) })

			sec, err := Open(t.Context(), obj.Sections()[0])
			require.NoError(t, err)
			reader := NewRowReader(sec)
			require.NoError(t, reader.Open(t.Context()))
			t.Cleanup(func() { require.NoError(t, reader.Close()) })
			records := make([]Record, 1)
			n, err := reader.Read(t.Context(), records)

			require.NoError(t, err)
			require.Equal(t, 1, n)
			require.Equal(t, int64(1), records[0].StreamID)
			require.Equal(t, time.Unix(0, 1).UTC(), records[0].Timestamp.UTC())
		})
	}

	for _, version := range []uint32{1, 5} {
		t.Run(fmt.Sprintf("returns an error for a section of version %d", version), func(t *testing.T) {
			ty := sectionType
			ty.Version = version

			_, err := Open(t.Context(), &dataobj.Section{Type: ty})

			require.EqualError(t, err, fmt.Sprintf("unsupported section version: got=%d", version))
		})
	}
}

// versionSectionBuilder encodes a logs section with one row, with the given
// section version.
type versionSectionBuilder struct {
	version uint32
}

func (b versionSectionBuilder) Type() dataobj.SectionType {
	ty := sectionType
	ty.Version = b.version
	return ty
}

func (b versionSectionBuilder) Reset() {}

func (b versionSectionBuilder) Flush(w dataobj.SectionWriter) (int64, error) {
	enc := columnar.NewEncoder()
	defer enc.Reset()

	if err := enc.SetFormatVersion(formatVersionOf(b.version)); err != nil {
		return 0, err
	}

	streamIDBuilder := newInt64ColumnBuilder("stream_id", ColumnTypeStreamID.String())
	timestampBuilder := newInt64ColumnBuilder("timestamp", ColumnTypeTimestamp.String())
	if err := streamIDBuilder.Append(0, dataset.Int64Value(1)); err != nil {
		return 0, err
	}
	if err := timestampBuilder.Append(0, dataset.Int64Value(1)); err != nil {
		return 0, err
	}
	if err := encodeTestColumns(enc, streamIDBuilder, timestampBuilder); err != nil {
		return 0, err
	}
	enc.SetTenant("tenant")
	return enc.Flush(w)
}
