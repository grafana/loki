package dataobj

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/filemd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/streamio"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/protocodec"
	"github.com/grafana/loki/v3/pkg/scratch"
)

func Test_decoder_legacyObject(t *testing.T) {
	fixture := legacyTHORMagicFixture{
		sectionType: SectionType{
			Namespace: "github.com/grafana/loki",
			Kind:      "logs",
			Version:   7,
		},
		tenant:          "tenant-a",
		extensionData:   []byte("legacy-extension"),
		sectionData:     []byte("legacy-section-data"),
		sectionMetadata: []byte("legacy-section-metadata"),
	}

	encodedObject := buildLegacyTHORMagicObject(t, fixture)

	obj, err := FromReaderAt(bytes.NewReader(encodedObject), int64(len(encodedObject)))
	require.NoError(t, err)
	require.Equal(t, int64(len(encodedObject)), obj.Size())

	require.Len(t, obj.Sections(), 1)
	sec := obj.Sections()[0]

	dataReader, err := sec.Reader.DataRange(t.Context(), 0, sec.Reader.DataSize())
	require.NoError(t, err)
	require.Equal(t, fixture.sectionData, readAll(t, dataReader))

	metadataReader, err := sec.Reader.MetadataRange(t.Context(), 0, sec.Reader.MetadataSize())
	require.NoError(t, err)
	require.Equal(t, fixture.sectionMetadata, readAll(t, metadataReader))
}

type legacyTHORMagicFixture struct {
	sectionType SectionType
	tenant      string

	extensionData   []byte
	sectionData     []byte
	sectionMetadata []byte
}

func buildLegacyTHORMagicObject(t *testing.T, fixture legacyTHORMagicFixture) []byte {
	t.Helper()

	fileMetadata := &filemd.Metadata{
		Sections: []*filemd.SectionInfo{
			{
				TypeRef: 1,
				Layout: &filemd.SectionLayout{
					Data:     &filemd.Region{Offset: uint64(len(legacyMagic)), Length: uint64(len(fixture.sectionData))},
					Metadata: &filemd.Region{Offset: uint64(len(legacyMagic) + len(fixture.sectionData)), Length: uint64(len(fixture.sectionMetadata))},
				},
				ExtensionData: fixture.extensionData,
				TenantRef:     3,
			},
		},
		Dictionary: []string{
			"",
			fixture.sectionType.Namespace,
			fixture.sectionType.Kind,
			fixture.tenant,
		},
		Types: []*filemd.SectionType{
			{NameRef: nil}, // Invalid type.
			{
				NameRef: &filemd.SectionType_NameRef{
					NamespaceRef: 1,
					KindRef:      2,
				},
				Version: fixture.sectionType.Version,
			},
		},
	}

	var encodedMetadata bytes.Buffer
	require.NoError(t, streamio.WriteUvarint(&encodedMetadata, fileFormatVersion))
	require.NoError(t, protocodec.Encode(&encodedMetadata, fileMetadata))

	store := scratch.NewMemory()
	snapshot, err := newSnapshot(
		store,
		legacyMagic,
		[]sectionRegion{
			{Handle: store.Put(fixture.sectionData), Size: len(fixture.sectionData)},
			{Handle: store.Put(fixture.sectionMetadata), Size: len(fixture.sectionMetadata)},
			{Handle: store.Put(encodedMetadata.Bytes()), Size: encodedMetadata.Len()},
		},
		buildLegacyTailer(uint32(encodedMetadata.Len())),
	)
	require.NoError(t, err)
	defer func() { require.NoError(t, snapshot.Close()) }()

	encodedObject, err := io.ReadAll(io.NewSectionReader(snapshot, 0, snapshot.Size()))
	require.NoError(t, err)
	return encodedObject
}

func buildLegacyTailer(metadataSize uint32) []byte {
	tailer := make([]byte, 4, 8)
	binary.LittleEndian.PutUint32(tailer, metadataSize)
	tailer = append(tailer, legacyMagic...)
	return tailer
}

func readAll(t *testing.T, rc io.ReadCloser) []byte {
	t.Helper()
	defer func() { require.NoError(t, rc.Close()) }()

	bb, err := io.ReadAll(rc)
	require.NoError(t, err)
	return bb
}
