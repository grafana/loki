package push

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/golang/snappy"
)

var (
	ErrContentTooLarge            = errors.New("content exceeds maximum size")
	ErrUnsupportedContentEncoding = errors.New("unsupported content encoding")
)

func readBody(r *http.Request, limit int64) ([]byte, error) {
	contentEnc := r.Header.Get(contentEncHeaderKey)
	switch contentEnc {
	case "snappy":
		return decompressReaderSnappy(r.Body, limit)
	case "":
		return readPlain(r.Body, limit)
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedContentEncoding, contentEnc)
	}
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

// readFromNoCopy checks if r is a bytes.Buffer and if it is returns a pointer
// to it. If r is any other type it returns false.
func readFromNoCopy(r io.Reader) (*bytes.Buffer, bool) {
	// If the request is an httpgrpc request, r.Body is a wrapped buffer that
	// implements the bytesBuffer interface.
	type bytesBuffer interface{ BytesBuffer() *bytes.Buffer }
	if bytesBuf, ok := r.(bytesBuffer); ok {
		return bytesBuf.BytesBuffer(), true
	}
	return nil, false
}
