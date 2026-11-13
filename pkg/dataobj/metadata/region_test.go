package metadata

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/format"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/filemd"
)

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
