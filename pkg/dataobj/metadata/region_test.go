package metadata

import (
	"bytes"
	"encoding/binary"
	"math"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/format"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/filemd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/protocodec"
)

// The type references of the sections that fileMetadataWithSections describes.
const (
	logsTypeRef    = 1
	streamsTypeRef = 2
)

func TestSplitMetadataRegionToGroups(t *testing.T) {
	t.Run("splits a logs section and a non-logs section into groups, sorted by offset and joined where they touch", func(t *testing.T) {
		const (
			streamIDRef = 1 + iota
			timestampRef
			messageRef
			metadataRef
			levelRef
			traceIDRef
		)

		// placed is data that place put in the region.
		type placed struct {
			offset int64
			data   []byte
		}
		var region []byte
		place := func(data string) placed {
			p := placed{offset: int64(len(region)), data: []byte(data)}
			region = append(region, data...)
			return p
		}
		header := place("headerfm")
		startOffset := int64(len(header.data))
		nonLogs := place("nlogs")
		logsOffset := int64(len(region))
		streamID := place("sid")
		timestamp := place("tsx")
		message := place("msg!")
		level := place("lv")
		traceID := place("ti")

		column := func(logicalRef, tagRef uint32, p placed) *datasetmd.ColumnDesc {
			return &datasetmd.ColumnDesc{
				Type:                 &datasetmd.ColumnType{LogicalRef: logicalRef},
				TagRef:               tagRef,
				ColumnMetadataOffset: uint64(p.offset - logsOffset),
				ColumnMetadataLength: uint64(len(p.data)),
			}
		}
		var sectionMetadata bytes.Buffer
		require.NoError(t, protocodec.Encode(&sectionMetadata, &datasetmd.SectionMetadata{
			Dictionary: []string{"", format.LogsColumnStreamID, format.LogsColumnTimestamp, format.LogsColumnMessage, format.LogsColumnMetadata, "level", "trace_id"},
			Columns: []*datasetmd.ColumnDesc{
				column(messageRef, 0, message),
				column(streamIDRef, 0, streamID),
				column(metadataRef, traceIDRef, traceID),
				column(timestampRef, 0, timestamp),
				column(metadataRef, levelRef, level),
			},
		}))
		sectionMetadataOffset := place(sectionMetadata.String()).offset

		var extension bytes.Buffer
		require.NoError(t, protocodec.Encode(&extension, &datasetmd.SectionInfoExtension{
			SectionMetadataOffset: uint64(sectionMetadataOffset - logsOffset),
			SectionMetadataLength: uint64(sectionMetadata.Len()),
		}))
		md := fileMetadataWithSections(
			&filemd.SectionInfo{TypeRef: streamsTypeRef, Layout: metadataLayout(uint64(nonLogs.offset-startOffset), uint64(len(nonLogs.data)))},
			&filemd.SectionInfo{TypeRef: logsTypeRef, ExtensionData: extension.Bytes(), Layout: metadataLayout(uint64(logsOffset-startOffset), uint64(int64(len(region))-logsOffset))},
		)

		got, err := splitMetadataRegionToGroups(md, startOffset, region)

		require.NoError(t, err)
		require.Equal(t, map[prefetchedRangesID]PrefetchedRanges{
			{Kind: kindFileMetadata}: {{Offset: header.offset, Data: header.data}},
			{Kind: kindCore}: {
				{Offset: nonLogs.offset, Data: slices.Concat(nonLogs.data, streamID.data, timestamp.data)},
				{Offset: sectionMetadataOffset, Data: sectionMetadata.Bytes()},
			},
			{Kind: kindMessage}:                        {{Offset: message.offset, Data: message.data}},
			{Kind: kindColumn, ColumnName: "level"}:    {{Offset: level.offset, Data: level.data}},
			{Kind: kindColumn, ColumnName: "trace_id"}: {{Offset: traceID.offset, Data: traceID.data}},
		}, got.Groups)
		require.Equal(t, map[string]struct{}{"level": {}, "trace_id": {}}, got.ColumnNames)
		for id, ranges := range got.Groups {
			for _, r := range ranges {
				require.Same(t, &region[r.Offset], &r.Data[0], "the range of %v must alias the region", id)
			}
		}
	})

	t.Run("sorts the ranges of the core group by offset when the sections are out of order", func(t *testing.T) {
		md := fileMetadataWithSections(
			&filemd.SectionInfo{TypeRef: streamsTypeRef, Layout: metadataLayout(3, 2)},
			&filemd.SectionInfo{TypeRef: streamsTypeRef, Layout: metadataLayout(0, 3)},
		)

		got, err := splitMetadataRegionToGroups(md, 8, []byte("headerfmBBBAA"))

		require.NoError(t, err)
		require.Equal(t, PrefetchedRanges{{Offset: 8, Data: []byte("BBBAA")}}, got.Groups[prefetchedRangesID{Kind: kindCore}])
	})

	t.Run("returns the file metadata group and empty message and core groups for an object without sections", func(t *testing.T) {
		got, err := splitMetadataRegionToGroups(fileMetadataWithSections(), 8, []byte("headerfm"))

		require.NoError(t, err)
		require.Equal(t, map[prefetchedRangesID]PrefetchedRanges{
			{Kind: kindFileMetadata}: {{Offset: 0, Data: []byte("headerfm")}},
			{Kind: kindMessage}:      nil,
			{Kind: kindCore}:         nil,
		}, got.Groups)
		require.Empty(t, got.ColumnNames)
	})

	// The bounds checks read only the length of the region, so zeroed bytes are enough.
	region := make([]byte, 200)

	tests := map[string]datasetmd.SectionInfoExtension{
		"a section metadata range that wraps around int64":      {SectionMetadataOffset: math.MaxUint64 - 4, SectionMetadataLength: 10},
		"a section metadata offset at the largest int64":        {SectionMetadataOffset: math.MaxInt64, SectionMetadataLength: 10},
		"a section metadata length that does not fit in int64":  {SectionMetadataOffset: 0, SectionMetadataLength: math.MaxUint64},
		"a section metadata range that ends after the region":   {SectionMetadataOffset: 100, SectionMetadataLength: 200},
		"a section metadata range that starts after the region": {SectionMetadataOffset: 300, SectionMetadataLength: 1},
	}
	for name, ext := range tests {
		t.Run("rejects "+name+" without panicking", func(t *testing.T) {
			md := logsSectionMetadata(t, &ext)

			got, err := splitMetadataRegionToGroups(md, 8, region)

			require.Error(t, err)
			require.Equal(t, prefetchedRangesGroups{}, got)
		})
	}

	t.Run("rejects a section with a type reference out of bounds", func(t *testing.T) {
		md := logsSectionMetadata(t, &datasetmd.SectionInfoExtension{})
		md.Sections[0].TypeRef = 9

		got, err := splitMetadataRegionToGroups(md, 8, region)

		require.Error(t, err)
		require.Equal(t, prefetchedRangesGroups{}, got)
	})

	t.Run("rejects a column of a type it does not know", func(t *testing.T) {
		md, logsRegion := logsSectionWithColumn(t, &datasetmd.ColumnDesc{Type: &datasetmd.ColumnType{LogicalRef: 1}}, "future_type")

		got, err := splitMetadataRegionToGroups(md, 8, logsRegion)

		require.EqualError(t, err, `section 0: column 0 has an unknown type "future_type"`)
		require.ErrorAs(t, err, new(unsupportedError), "a newer writer can add a column type")
		require.Equal(t, prefetchedRangesGroups{}, got)
	})

	t.Run("rejects a column whose type reference is out of bounds", func(t *testing.T) {
		md, logsRegion := logsSectionWithColumn(t, &datasetmd.ColumnDesc{Type: &datasetmd.ColumnType{LogicalRef: 9}}, format.LogsColumnMessage)

		got, err := splitMetadataRegionToGroups(md, 8, logsRegion)

		require.EqualError(t, err, `section 0: column 0 has an unknown type ""`)
		require.Equal(t, prefetchedRangesGroups{}, got)
	})

	t.Run("rejects a column without a type", func(t *testing.T) {
		md, logsRegion := logsSectionWithColumn(t, &datasetmd.ColumnDesc{}, format.LogsColumnMessage)

		got, err := splitMetadataRegionToGroups(md, 8, logsRegion)

		require.EqualError(t, err, `section 0: column 0 has an unknown type ""`)
		require.Equal(t, prefetchedRangesGroups{}, got)
	})
}

func TestMetadataRegionSize(t *testing.T) {
	logsTypes := func(sections ...*filemd.SectionInfo) *filemd.Metadata {
		return &filemd.Metadata{
			Dictionary: []string{"", format.SectionNamespace, format.LogsSectionKind},
			Types:      []*filemd.SectionType{{}, {NameRef: &filemd.SectionType_NameRef{NamespaceRef: 1, KindRef: 2}}},
			Sections:   sections,
		}
	}

	tests := map[string]*filemd.Metadata{
		"a section without a layout": logsTypes(&filemd.SectionInfo{TypeRef: 1}),
		"a metadata region that wraps around int64": logsTypes(&filemd.SectionInfo{
			TypeRef: 1,
			Layout:  &filemd.SectionLayout{Metadata: &filemd.Region{Offset: math.MaxInt64, Length: 10}},
		}),
	}
	for name, md := range tests {
		t.Run("rejects "+name+" as not prefetchable", func(t *testing.T) {
			_, err := metadataRegionSize(md, 8)

			require.ErrorIs(t, err, ErrNotPrefetchable)
		})
	}

	t.Run("returns the end of the metadata of the section that ends last, whatever the section order", func(t *testing.T) {
		md := logsTypes(
			&filemd.SectionInfo{TypeRef: 1, Layout: &filemd.SectionLayout{Metadata: &filemd.Region{Offset: 500, Length: 100}}},
			&filemd.SectionInfo{TypeRef: 1, Layout: &filemd.SectionLayout{Metadata: &filemd.Region{Offset: 0, Length: 50}}},
		)

		size, err := metadataRegionSize(md, 8)

		require.NoError(t, err)
		require.EqualValues(t, 8+500+100, size)
	})

	t.Run("returns the size of the header and the file metadata when there is no section", func(t *testing.T) {
		size, err := metadataRegionSize(logsTypes(), 8)

		require.NoError(t, err)
		require.EqualValues(t, 8, size)
	})
}

func TestDecodeHeaderAndFileMetadata(t *testing.T) {
	header := func(body []byte) []byte {
		head := append([]byte("DOBJ"), binary.LittleEndian.AppendUint32(nil, uint32(len(body)))...)
		return append(head, body...)
	}

	t.Run("rejects a message size above the bytes of the file metadata, without allocating it", func(t *testing.T) {
		body := binary.AppendUvarint(nil, format.FileFormatVersion)
		body = binary.AppendUvarint(body, 1<<50)

		_, _, err := decodeHeaderAndFileMetadata(header(body))

		require.Error(t, err)
	})

	t.Run("says how many bytes it needs when the head ends inside the file metadata", func(t *testing.T) {
		head := header(make([]byte, 100))[:20]

		_, _, err := decodeHeaderAndFileMetadata(head)

		require.EqualError(t, err, "the head is too short: it has 20 bytes, the header and the file metadata need 108")
		require.ErrorIs(t, err, errHeadTooShort)
	})

	t.Run("says how many bytes it needs when the head ends inside the header", func(t *testing.T) {
		_, _, err := decodeHeaderAndFileMetadata([]byte("DOBJ"))

		require.EqualError(t, err, "the head is too short: it has 4 bytes, the header needs 8")
		require.ErrorIs(t, err, errHeadTooShort)
	})
}

// fileMetadataWithSections returns file metadata that describes sections. A section has the type
// reference logsTypeRef or streamsTypeRef.
func fileMetadataWithSections(sections ...*filemd.SectionInfo) *filemd.Metadata {
	return &filemd.Metadata{
		Dictionary: []string{"", format.SectionNamespace, format.LogsSectionKind, format.StreamsSectionKind},
		Types: []*filemd.SectionType{
			{},
			{NameRef: &filemd.SectionType_NameRef{NamespaceRef: 1, KindRef: 2}},
			{NameRef: &filemd.SectionType_NameRef{NamespaceRef: 1, KindRef: 3}},
		},
		Sections: sections,
	}
}

// metadataLayout returns the layout of a section whose metadata region starts offset bytes after
// the file metadata.
func metadataLayout(offset, length uint64) *filemd.SectionLayout {
	return &filemd.SectionLayout{Metadata: &filemd.Region{Offset: offset, Length: length}}
}

// logsSectionWithColumn returns the file metadata of an object with one logs section that has one
// column, and the region of the object. The column has the logical type logicalType, which is the
// first entry after the empty one in the dictionary of the section. The header and the file metadata
// take 8 bytes of the region. The metadata of the section follows them.
func logsSectionWithColumn(t *testing.T, col *datasetmd.ColumnDesc, logicalType string) (*filemd.Metadata, []byte) {
	t.Helper()

	var sectionMetadata bytes.Buffer
	require.NoError(t, protocodec.Encode(&sectionMetadata, &datasetmd.SectionMetadata{
		Dictionary: []string{"", logicalType},
		Columns:    []*datasetmd.ColumnDesc{col},
	}))

	md := logsSectionMetadata(t, &datasetmd.SectionInfoExtension{SectionMetadataOffset: 0, SectionMetadataLength: uint64(sectionMetadata.Len())})
	md.Sections[0].Layout.Metadata.Length = uint64(sectionMetadata.Len())
	return md, append(make([]byte, 8), sectionMetadata.Bytes()...)
}

// logsSectionMetadata returns the file metadata of an object with one logs section.
func logsSectionMetadata(t *testing.T, ext *datasetmd.SectionInfoExtension) *filemd.Metadata {
	t.Helper()

	var extension bytes.Buffer
	require.NoError(t, protocodec.Encode(&extension, ext))

	return &filemd.Metadata{
		Dictionary: []string{"", format.SectionNamespace, format.LogsSectionKind},
		Types:      []*filemd.SectionType{{}, {NameRef: &filemd.SectionType_NameRef{NamespaceRef: 1, KindRef: 2}}},
		Sections: []*filemd.SectionInfo{{
			TypeRef:       1,
			ExtensionData: extension.Bytes(),
			Layout:        &filemd.SectionLayout{Metadata: &filemd.Region{Offset: 0, Length: 100}},
		}},
	}
}
