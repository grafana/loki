package push

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/golang/snappy"
	"github.com/klauspost/compress/zstd"
	"github.com/pierrec/lz4/v4"
)

var (
	ErrContentTooLarge            = errors.New("content exceeds maximum size")
	ErrUnsupportedContentEncoding = errors.New("unsupported content encoding")
)

// readBody reads the full request body from r. If it is encoded (i.e. with
// gzip, snappy, etc) then the decompressed body is returned instead. It
// supports an optional limit that caps the maximum size of the plain and
// the decompressed body, and returns an error if this limit is exceeded.
// A value of 0 means no limit.
func readBody(r *http.Request, limit int64) ([]byte, error) {
	contentEncValue := r.Header.Get(contentEnc)
	switch contentEncValue {
	case "deflate", "gzip", "lz4", "zstd":
		return decompressReader(r.Body, contentEncValue, limit)
	case "snappy":
		// Unlike other content encs, block-compressed snappy cannot be decoded from
		// an io.Reader. It has a special method to handle this.
		return decompressReaderSnappy(r.Body, limit)
	case "":
		return readPlain(r.Body, limit)
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedContentEncoding, contentEncValue)
	}
}

// readPlain reads the plain text from r.
func readPlain(r io.Reader, limit int64) ([]byte, error) {
	// If r is a bytes.Buffer then we don't need to copy it.
	buf, ok := readFromNoCopy(r)
	if !ok {
		if limit > 0 {
			r = io.LimitReader(r, limit+1)
		}
		buf = new(bytes.Buffer)
		_, err := buf.ReadFrom(r)
		if err != nil {
			return nil, err
		}
	}

	if limit > 0 && int64(buf.Len()) > limit {
		return nil, fmt.Errorf("%w: %d", ErrContentTooLarge, limit)
	}

	return buf.Bytes(), nil
}

// decompressReader decompresses the data in r based on the content enc.
// Supported encs include deflate, gzip, lz4, zstd, and "".
func decompressReader(r io.Reader, contentEncValue string, limit int64) ([]byte, error) {
	rc, err := wrapDecoder(r, contentEncValue)
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	if limit > 0 {
		rc = io.NopCloser(io.LimitReader(rc, limit+1))
	}

	buf := bytes.Buffer{}
	_, err = buf.ReadFrom(rc)
	if err != nil {
		return nil, err
	}

	if limit > 0 && int64(buf.Len()) > limit {
		return nil, fmt.Errorf("%w: %d", ErrContentTooLarge, limit)
	}

	return buf.Bytes(), err
}

func decompressReaderSnappy(r io.Reader, limit int64) ([]byte, error) {
	// Unlike other readers, snappy can only decode block-compressed data from an
	// []byte. That means we must read the entire body before we can start to
	// decompress it.
	buf, ok := readFromNoCopy(r)
	if !ok {
		if limit > 0 {
			r = io.LimitReader(r, limit+1)
		}
		buf = new(bytes.Buffer)
		_, err := buf.ReadFrom(r)
		if err != nil {
			return nil, err
		}
	}
	src := buf.Bytes()

	// Check the raw size.
	if limit > 0 && int64(len(src)) > limit {
		return nil, fmt.Errorf("%w: %d", ErrContentTooLarge, limit)
	}

	// Check the decompressed size.
	if limit > 0 {
		if n, err := snappy.DecodedLen(src); err != nil {
			return nil, err
		} else if int64(n) > limit {
			return nil, fmt.Errorf("%w: %d", ErrContentTooLarge, limit)
		}
	}

	return snappy.Decode(nil, src)
}

// wrapDecoder returns an io.ReadCloser that decompresses r.
func wrapDecoder(r io.Reader, contentEncValue string) (io.ReadCloser, error) {
	switch contentEncValue {
	case "deflate":
		return flate.NewReader(r), nil
	case "gzip":
		return gzip.NewReader(r)
	case "lz4":
		return io.NopCloser(lz4.NewReader(r)), nil
	case "zstd":
		zstdReader, err := zstd.NewReader(r)
		if err != nil {
			return nil, err
		}
		return zstdReader.IOReadCloser(), nil
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedContentEncoding, contentEncValue)
	}
}

// readFromNoCopy checks if r is a bytes.Buffer and if it is returns a pointer
// to it. If r is any other type it returns false.
func readFromNoCopy(r io.Reader) (*bytes.Buffer, bool) {
	// If the request came from httpgrpc instead of net/http, r.Body is already
	// buffered. We know if this is the case because r.Body implements the
	// bytesBuffer interface.
	type bytesBuffer interface {
		BytesBuffer() *bytes.Buffer
	}
	if bytesBuf, ok := r.(bytesBuffer); ok {
		return bytesBuf.BytesBuffer(), true
	}
	return nil, false
}
