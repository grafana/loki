package dataobj

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/filemd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/streamio"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/protocodec"
)

// decode* methods for metadata shared by Decoder implementations.

// decodeTailer decodes the tailer of the file to retrieve the metadata size
// and the magic value.
func decodeTailer(r streamio.Reader) (metadataSize uint32, err error) {
	if err := binary.Read(r, binary.LittleEndian, &metadataSize); err != nil {
		return 0, fmt.Errorf("read metadata size: %w", err)
	}

	var gotMagic [4]byte
	if _, err := io.ReadFull(r, gotMagic[:]); err != nil {
		return 0, fmt.Errorf("read magic: %w", err)
	} else if string(gotMagic[:]) != string(magic) {
		return 0, fmt.Errorf("unexpected magic: got=%q want=%q", gotMagic, magic)
	}

	return
}

// decodeFileMetadata decodes file metadata from r.
func decodeFileMetadata(r streamio.Reader) (*filemd.Metadata, error) {
	gotVersion, err := streamio.ReadUvarint(r)
	if err != nil {
		return nil, fmt.Errorf("read file format version: %w", err)
	} else if gotVersion != fileFormatVersion {
		return nil, fmt.Errorf("unexpected file format version: got=%d want=%d", gotVersion, fileFormatVersion)
	}

	var md filemd.Metadata
	if err := protocodec.Decode(r, &md); err != nil {
		return nil, fmt.Errorf("file metadata: %w", err)
	}
	return &md, nil
}
