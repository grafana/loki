package metadata

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"

	"github.com/gogo/protobuf/proto"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/format"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/filemd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/protocodec"
)

// errHeadTooShort means the head is too short to hold the header and the file metadata.
var errHeadTooShort = errors.New("the head is too short")

// decodeHeaderAndFileMetadata decodes the header and the file metadata from the first bytes of an
// object. startOffset is the offset where the section offsets start: the size of the header and the
// file metadata. It returns an error that wraps errHeadTooShort when head is too short to hold them.
func decodeHeaderAndFileMetadata(head []byte) (md *filemd.Metadata, startOffset int64, err error) {
	if len(head) < format.HeaderSize {
		return nil, 0, fmt.Errorf("%w: it has %d bytes, the header needs %d", errHeadTooShort, len(head), format.HeaderSize)
	}
	if magic := head[:len(format.FileMagic)]; string(magic) != format.FileMagic {
		return nil, 0, fmt.Errorf("unexpected magic %q", magic)
	}

	startOffset = format.HeaderSize + int64(binary.LittleEndian.Uint32(head[len(format.FileMagic):format.HeaderSize]))
	if int64(len(head)) < startOffset {
		return nil, 0, fmt.Errorf("%w: it has %d bytes, the header and the file metadata need %d", errHeadTooShort, len(head), startOffset)
	}

	body := head[format.HeaderSize:startOffset]
	version, n := binary.Uvarint(body)
	if n <= 0 {
		return nil, 0, errors.New("reading file format version: the file metadata is truncated")
	}
	if version != format.FileFormatVersion {
		return nil, 0, fmt.Errorf("unexpected file format version %d", version)
	}

	md = &filemd.Metadata{}
	if err := decodeProto(body[n:], md); err != nil {
		return nil, 0, fmt.Errorf("decoding file metadata: %w", err)
	}
	return md, startOffset, nil
}

// metadataRegionSize computes the size of the region from the file metadata alone: the bytes from
// offset 0 to the end of the metadata of all sections. The region holds no section data, because a
// section can have a lot of it. metadataRegionSize returns an error that wraps ErrNotPrefetchable
// when a layout is invalid.
func metadataRegionSize(md *filemd.Metadata, startOffset int64) (int64, error) {
	size := startOffset
	for i, sec := range md.Sections {
		metadataOffset, metadataLength, err := sectionRegionRange(sec.GetLayout().GetMetadata(), startOffset)
		if err != nil {
			return 0, fmt.Errorf("%w: section %d metadata: %w", ErrNotPrefetchable, i, err)
		}
		size = max(size, metadataOffset+metadataLength)
	}
	return size, nil
}

// decodeProto decodes a message that protocodec encoded at the start of b. It first checks that the
// size prefix fits in b, because protocodec.Decode allocates the size that the prefix names.
func decodeProto(b []byte, pb proto.Message) error {
	size, n := binary.Uvarint(b)
	if n <= 0 || size > uint64(len(b)-n) {
		return errors.New("the message is truncated")
	}
	return protocodec.Decode(bytes.NewReader(b), pb)
}

// sectionRegionRange returns the absolute offset and the length of a region of a section.
func sectionRegionRange(region *filemd.Region, startOffset int64) (offset, length int64, err error) {
	if region == nil {
		return 0, 0, errors.New("the section has no layout")
	}
	if region.Offset > math.MaxInt64 || region.Length > math.MaxInt64 {
		return 0, 0, errors.New("the region is too large")
	}
	offset = startOffset + int64(region.Offset)
	length = int64(region.Length)
	if offset < startOffset || offset+length < offset {
		return 0, 0, errors.New("the region overflows")
	}
	return offset, length, nil
}
