package sarama

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"slices"
	"sync"

	"github.com/klauspost/compress/gzip"
	snappy "github.com/klauspost/compress/snappy/xerial"
	"github.com/klauspost/compress/zstd"
	"github.com/pierrec/lz4/v4"
)

var (
	lz4ReaderPool = sync.Pool{
		New: func() any {
			return lz4.NewReader(nil)
		},
	}

	gzipReaderPool sync.Pool

	bufferPool = sync.Pool{
		New: func() any {
			return new(bytes.Buffer)
		},
	}

	bytesPool = sync.Pool{
		New: func() any {
			res := make([]byte, 0, 4096)
			return &res
		},
	}
)

func newDecompressedBatchTooLargeError(cc CompressionCodec, limit int) error {
	return fmt.Errorf("%w: %s batch decompressed to more than %d bytes", ErrDecompressedBatchTooLarge, cc, limit)
}

// boundedDecompress reads from a streaming decompressor into a pooled buffer,
// capping the read at limit+1 bytes so an oversized batch is caught before it
// fully inflates. A non-positive limit disables the cap.
func boundedDecompress(cc CompressionCodec, reader io.Reader, limit int) ([]byte, error) {
	buffer := bufferPool.Get().(*bytes.Buffer)
	defer func() {
		buffer.Reset()
		bufferPool.Put(buffer)
	}()

	src := reader
	if limit > 0 {
		src = io.LimitReader(reader, int64(limit)+1)
	}

	n, err := buffer.ReadFrom(src)
	if err != nil {
		return nil, err
	}
	if limit > 0 && n > int64(limit) {
		return nil, newDecompressedBatchTooLargeError(cc, limit)
	}
	return slices.Clone(buffer.Bytes()), nil
}

// boundedSnappyDecode decodes xerial/snappy into a capped buffer grown on
// demand up to limit. DecodeCapped returns ErrDstTooSmall rather than writing
// past the buffer's capacity, which we translate into a too-large error once
// the buffer reaches limit. A non-positive limit decodes in one shot, unbounded.
func boundedSnappyDecode(cc CompressionCodec, data []byte, limit int) ([]byte, error) {
	if limit <= 0 {
		return snappy.Decode(data)
	}

	bufp := bytesPool.Get().(*[]byte)
	buf := *bufp
	defer func() {
		*bufp = buf[:0]
		bytesPool.Put(bufp)
	}()

	size := max(cap(buf), 1024)
	var out []byte
	var err error
	for {
		if size > limit {
			size = limit
		}
		if cap(buf) < size {
			buf = make([]byte, 0, size)
		}
		out, err = snappy.DecodeCapped(buf[:0:size], data)
		if !errors.Is(err, snappy.ErrDstTooSmall) || size >= limit {
			break
		}
		size *= 2
	}

	switch {
	case errors.Is(err, snappy.ErrDstTooSmall):
		return nil, newDecompressedBatchTooLargeError(cc, limit)
	case err != nil:
		return nil, err
	default:
		return slices.Clone(out), nil
	}
}

// boundedZstdDecode decodes into a pooled buffer using a decoder whose max
// memory is capped at limit, so an oversized frame fails without allocating its
// output. ErrDecoderSizeExceeded is translated into a too-large error.
func boundedZstdDecode(cc CompressionCodec, data []byte, limit int) ([]byte, error) {
	buffer := *bytesPool.Get().(*[]byte)
	defer func() {
		buffer = buffer[:0]
		bytesPool.Put(&buffer)
	}()

	buffer, err := zstdDecompress(ZstdDecoderParams{}, buffer, data, limit)
	if errors.Is(err, zstd.ErrDecoderSizeExceeded) {
		return nil, newDecompressedBatchTooLargeError(cc, limit)
	}
	if err != nil {
		return nil, err
	}
	return slices.Clone(buffer), nil
}

func decompress(cc CompressionCodec, data []byte) ([]byte, error) {
	// 0 means unbounded
	limit := int(MaxDecompressedBatchSize)
	switch cc {
	case CompressionNone:
		return data, nil
	case CompressionGZIP:
		var err error
		reader, ok := gzipReaderPool.Get().(*gzip.Reader)
		if !ok {
			reader, err = gzip.NewReader(bytes.NewReader(data))
		} else {
			err = reader.Reset(bytes.NewReader(data))
		}

		if err != nil {
			return nil, err
		}

		res, err := boundedDecompress(cc, reader, limit)
		gzipReaderPool.Put(reader)
		return res, err
	case CompressionSnappy:
		return boundedSnappyDecode(cc, data, limit)
	case CompressionLZ4:
		reader, ok := lz4ReaderPool.Get().(*lz4.Reader)
		if !ok {
			reader = lz4.NewReader(bytes.NewReader(data))
		} else {
			reader.Reset(bytes.NewReader(data))
		}

		res, err := boundedDecompress(cc, reader, limit)
		lz4ReaderPool.Put(reader)
		return res, err
	case CompressionZSTD:
		return boundedZstdDecode(cc, data, limit)
	default:
		return nil, PacketDecodingError{fmt.Sprintf("invalid compression specified (%d)", cc)}
	}
}
