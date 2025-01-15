package encoding

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/gogo/protobuf/proto"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/filemd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/logsmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/streamsmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/streamio"
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
	if err := decodeProto(r, &md); err != nil {
		return nil, fmt.Errorf("file metadata: %w", err)
	}
	return &md, nil
}

// decodeStreamsMetadata decodes stream section metadata from r.
func decodeStreamsMetadata(r streamio.Reader) (*streamsmd.Metadata, error) {
	gotVersion, err := streamio.ReadUvarint(r)
	if err != nil {
		return nil, fmt.Errorf("read streams section format version: %w", err)
	} else if gotVersion != streamsFormatVersion {
		return nil, fmt.Errorf("unexpected streams section format version: got=%d want=%d", gotVersion, streamsFormatVersion)
	}

	var md streamsmd.Metadata
	if err := decodeProto(r, &md); err != nil {
		return nil, fmt.Errorf("streams section metadata: %w", err)
	}
	return &md, nil
}

// decodeStreamsColumnMetadata decodes stream column metadata from r.
func decodeStreamsColumnMetadata(r streamio.Reader) (*streamsmd.ColumnMetadata, error) {
	var metadata streamsmd.ColumnMetadata
	if err := decodeProto(r, &metadata); err != nil {
		return nil, fmt.Errorf("streams column metadata: %w", err)
	}
	return &metadata, nil
}

// decodeLogsMetadata decodes logs section metadata from r.
func decodeLogsMetadata(r streamio.Reader) (*logsmd.Metadata, error) {
	gotVersion, err := streamio.ReadUvarint(r)
	if err != nil {
		return nil, fmt.Errorf("read streams section format version: %w", err)
	} else if gotVersion != streamsFormatVersion {
		return nil, fmt.Errorf("unexpected streams section format version: got=%d want=%d", gotVersion, streamsFormatVersion)
	}

	var md logsmd.Metadata
	if err := decodeProto(r, &md); err != nil {
		return nil, fmt.Errorf("streams section metadata: %w", err)
	}
	return &md, nil
}

// decodeLogsColumnMetadata decodes logs column metadata from r.
func decodeLogsColumnMetadata(r streamio.Reader) (*logsmd.ColumnMetadata, error) {
	var metadata logsmd.ColumnMetadata
	if err := decodeProto(r, &metadata); err != nil {
		return nil, fmt.Errorf("streams column metadata: %w", err)
	}
	return &metadata, nil
}

// decodeProto decodes a proto message from r and stores it in pb. Proto
// messages are expected to be encoded with their size, followed by the proto
// bytes.
func decodeProto(r streamio.Reader, pb proto.Message) error {
	size, err := binary.ReadUvarint(r)
	if err != nil {
		return fmt.Errorf("read proto message size: %w", err)
	}

	buf := bytesBufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer bytesBufferPool.Put(buf)

	n, err := io.Copy(buf, io.LimitReader(r, int64(size)))
	if err != nil {
		return fmt.Errorf("read proto message: %w", err)
	} else if uint64(n) != size {
		return fmt.Errorf("read proto message: got=%d want=%d", n, size)
	}

	if err := proto.Unmarshal(buf.Bytes(), pb); err != nil {
		return fmt.Errorf("unmarshal proto message: %w", err)
	}
	return nil
}
