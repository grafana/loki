package dataset

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/grafana/loki/v3/pkg/dataset/encoding"
)

const (
	fileMagicSize    = 4
	fileVersion      = byte(0x01)
	fileLengthSize   = 4
	fileTrailerSize  = fileMagicSize
	fileMinimumSize  = fileMagicSize + 1 + fileLengthSize + fileLengthSize + fileTrailerSize
	fileHeaderPrefix = fileMagicSize + 1
)

var fileMagic = [fileMagicSize]byte{'D', 'S', 'E', 'T'}

type fileHeader struct {
	metadata []byte
	index    []byte
}

func marshalFileHeader(header fileHeader) ([]byte, error) {
	var (
		buf bytes.Buffer

		metadataSize = len(header.metadata)
		indexSize    = len(header.index)
		size         = fileHeaderPrefix + fileLengthSize
	)

	if uint64(metadataSize) > math.MaxUint32 {
		return nil, fmt.Errorf("metadata is too large: %d bytes", metadataSize)
	} else if uint64(indexSize) > math.MaxUint32 {
		return nil, fmt.Errorf("buffer index is too large: %d bytes", indexSize)
	}

	if metadataSize > math.MaxInt-size-fileLengthSize {
		return nil, fmt.Errorf("file header size overflows int")
	}
	size += metadataSize + fileLengthSize

	if indexSize > math.MaxInt-size {
		return nil, fmt.Errorf("file header size overflows int")
	}
	size += indexSize

	buf.Grow(size)

	_, _ = buf.Write(fileMagic[:])
	_ = buf.WriteByte(fileVersion)

	if err := binary.Write(&buf, binary.LittleEndian, uint32(metadataSize)); err != nil {
		return nil, fmt.Errorf("writing metadata size: %w", err)
	}
	_, _ = buf.Write(header.metadata)

	if err := binary.Write(&buf, binary.LittleEndian, uint32(indexSize)); err != nil {
		return nil, fmt.Errorf("writing buffer index size: %w", err)
	}
	_, _ = buf.Write(header.index)

	return buf.Bytes(), nil
}

func readFileHeader(ctx context.Context, r encoding.RangeReader) (header fileHeader, bodyOffset, bodyLength int64, err error) {
	size := r.Len()
	if size < fileMinimumSize {
		return fileHeader{}, 0, 0, fmt.Errorf("file too small: %d bytes", size)
	}

	var trailer [fileTrailerSize]byte
	if err := readFullRange(ctx, r, trailer[:], size-fileTrailerSize); err != nil {
		return fileHeader{}, 0, 0, fmt.Errorf("reading trailing magic: %w", err)
	}
	if trailer != fileMagic {
		return fileHeader{}, 0, 0, fmt.Errorf("invalid trailing magic: %x", trailer)
	}

	var prefix [fileHeaderPrefix]byte
	if err := readFullRange(ctx, r, prefix[:], 0); err != nil {
		return fileHeader{}, 0, 0, fmt.Errorf("reading leading magic and version: %w", err)
	}
	if [len(fileMagic)]byte(prefix[:len(fileMagic)]) != fileMagic {
		return fileHeader{}, 0, 0, fmt.Errorf("invalid leading magic: %x", prefix[:len(fileMagic)])
	}
	if prefix[len(fileMagic)] != fileVersion {
		return fileHeader{}, 0, 0, fmt.Errorf("unsupported version: %d", prefix[len(fileMagic)])
	}

	var (
		bodyEnd = size - fileTrailerSize
		off     = int64(fileHeaderPrefix)
	)

	metadata, off, err := readFileHeaderBlock(ctx, r, off, bodyEnd, fileLengthSize, "metadata")
	if err != nil {
		return fileHeader{}, 0, 0, err
	}

	index, off, err := readFileHeaderBlock(ctx, r, off, bodyEnd, 0, "buffer index")
	if err != nil {
		return fileHeader{}, 0, 0, err
	}

	return fileHeader{metadata: metadata, index: index}, off, bodyEnd - off, nil
}

func readFileHeaderBlock(ctx context.Context, r encoding.RangeReader, off, limit, reserved int64, name string) ([]byte, int64, error) {
	if off > limit || limit-off < fileLengthSize+reserved {
		return nil, 0, fmt.Errorf("reading %s size: file is truncated", name)
	}

	var sizeBytes [fileLengthSize]byte
	if err := readFullRange(ctx, r, sizeBytes[:], off); err != nil {
		return nil, 0, fmt.Errorf("reading %s size: %w", name, err)
	}
	off += fileLengthSize

	size := int64(binary.LittleEndian.Uint32(sizeBytes[:]))
	if size > limit-off-reserved {
		return nil, 0, fmt.Errorf("%s size %d exceeds remaining file size %d", name, size, limit-off-reserved)
	}
	if size > int64(math.MaxInt) {
		return nil, 0, fmt.Errorf("%s is too large to read: %d bytes", name, size)
	}

	data := make([]byte, int(size))
	if err := readFullRange(ctx, r, data, off); err != nil {
		return nil, 0, fmt.Errorf("reading %s: %w", name, err)
	}
	return data, off + size, nil
}

func readFullRange(ctx context.Context, r encoding.RangeReader, p []byte, off int64) error {
	n, err := r.ReadRange(ctx, p, off)
	if err != nil && (!errors.Is(err, io.EOF) || n != len(p)) {
		return err
	}
	if n != len(p) {
		return io.ErrUnexpectedEOF
	}
	return nil
}
