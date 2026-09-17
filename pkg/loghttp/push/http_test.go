package push

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/golang/snappy"
	"github.com/stretchr/testify/require"
)

func TestReadBody(t *testing.T) {
	t.Run("can read a plain body", func(t *testing.T) {
		data := []byte("the quick brown fox")
		r := newRequest(t, "", bytes.NewReader(data))
		actual, err := readBody(r, 0)
		require.NoError(t, err)
		require.Equal(t, data, actual)
		// Should be false since readBody copies the data.
		require.NotSame(t, &data[0], &actual[0])
	})

	t.Run("can read a nocopy plain body", func(t *testing.T) {
		data := []byte("the quick brown fox")
		buf := bytes.NewBuffer(data)
		r := newRequest(t, "", &wrappedBytesBuffer{Buffer: buf})
		actual, err := readBody(r, 0)
		require.NoError(t, err)
		require.Equal(t, data, actual)
		// Should be true if readyBody returns a pointer to the buf instead of a copy.
		require.Same(t, &data[0], &actual[0])
	})

	t.Run("reads an entire plain body no limit", func(t *testing.T) {
		data := bytes.Repeat([]byte("x"), 1000)
		r := newRequest(t, "", bytes.NewReader(data))
		actual, err := readBody(r, 0)
		require.NoError(t, err)
		require.Equal(t, data, actual)
	})

	t.Run("reads an entire plain body within the limit", func(t *testing.T) {
		data := bytes.Repeat([]byte("x"), 10)
		r := newRequest(t, "", bytes.NewReader(data))
		actual, err := readBody(r, 10)
		require.NoError(t, err)
		require.Equal(t, data, actual)
	})

	t.Run("returns error when plain body over the limit", func(t *testing.T) {
		data := bytes.Repeat([]byte("x"), 11)
		r := newRequest(t, "", bytes.NewReader(data))
		_, err := readBody(r, 10)
		require.Error(t, err)
		require.EqualError(t, err, "content exceeds maximum size: 10")
	})

	t.Run("snappy: returns error when compressed body is over the limit", func(t *testing.T) {
		data := bytes.Repeat([]byte("x"), 100)
		b := snappy.Encode(nil, data)
		r := newRequest(t, "snappy", bytes.NewReader(b))
		limit := int64(len(data) - 1)
		_, err := readBody(r, limit)
		require.EqualError(t, err, fmt.Sprintf("content exceeds maximum size: %d", limit))
	})

	t.Run("snappy: returns error when decompressed body is over the limit", func(t *testing.T) {
		data := bytes.Repeat([]byte("x"), 1000)
		b := snappy.Encode(nil, data)
		r := newRequest(t, "snappy", bytes.NewReader(b))
		limit := int64(len(data) - 1)
		_, err := readBody(r, limit)
		require.EqualError(t, err, fmt.Sprintf("content exceeds maximum size: %d", limit))
	})

	t.Run("unsupported content encoding returns error", func(t *testing.T) {
		r := newRequest(t, "abc", bytes.NewReader([]byte("data")))
		_, err := readBody(r, 0)
		require.Error(t, err)
		require.EqualError(t, err, "unsupported content encoding: abc")
	})
}

// A wrapepdBytesBuffer copies how httpgrpc wraps a bytes.Buffer in its own
// io.nopCloser.
type wrappedBytesBuffer struct {
	*bytes.Buffer
}

// BytesBuffer returns a pointer to the bytes.Buffer.
func (b *wrappedBytesBuffer) BytesBuffer() *bytes.Buffer {
	return b.Buffer
}

// Close implements io.Closer.
func (b *wrappedBytesBuffer) Close() error {
	return nil
}

func TestReadFromNoCopy(t *testing.T) {
	t.Run("should return pointer to buf", func(t *testing.T) {
		buf := bytes.Buffer{}
		_, err := buf.Write([]byte("the quick brown fox"))
		require.NoError(t, err)

		wrappedBuf := wrappedBytesBuffer{Buffer: &buf}
		actual, ok := readFromNoCopy(&wrappedBuf)
		require.True(t, ok)
		require.Equal(t, &buf, actual)
	})

	t.Run("should return false", func(t *testing.T) {
		s := "the quick brown fox"
		actual, ok := readFromNoCopy(strings.NewReader(s))
		require.False(t, ok)
		require.Nil(t, actual)
	})
}

func newRequest(t *testing.T, contentEnc string, body io.Reader) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/loki/api/v1/push", body)
	if contentEnc != "" {
		r.Header.Set(contentEncHeaderKey, contentEnc)
	}
	return r
}
