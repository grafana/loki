package util_test

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/util"
)

// errReader is a special reader that replaces io.EOF with a different error.
type errReader struct {
	data  []byte
	onEOF error
}

func (r *errReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, r.onEOF
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}

func TestCountingReader(t *testing.T) {
	t.Run("N starts from zero", func(t *testing.T) {
		cr := util.NewCountingReader(bytes.NewReader([]byte("hello")))
		assert.Equal(t, int64(0), cr.N())
	})

	t.Run("data is not modified, just counted", func(t *testing.T) {
		data := []byte("the quick brown fox jumps over the lazy dog")
		cr := util.NewCountingReader(bytes.NewReader(data))

		actual, err := io.ReadAll(cr)
		require.NoError(t, err)
		assert.Equal(t, data, actual)
		assert.Equal(t, int64(len(data)), cr.N())
	})

	t.Run("N is the sum of all reads", func(t *testing.T) {
		data := []byte("0123456789")
		cr := util.NewCountingReader(bytes.NewReader(data))

		buf := make([]byte, 3)
		total := 0
		for {
			n, err := cr.Read(buf)
			total += n
			if err != nil {
				require.ErrorIs(t, err, io.EOF)
				break
			}
			assert.Equal(t, int64(total), cr.N())
		}
		assert.Equal(t, len(data), total)
		assert.Equal(t, int64(len(data)), cr.N())
	})

	t.Run("N is not incremented on error", func(t *testing.T) {
		expectedErr := errors.New("this is an error")
		cr := util.NewCountingReader(&errReader{data: []byte("abc"), onEOF: expectedErr})

		buf := make([]byte, 16)
		n, err := cr.Read(buf)
		require.NoError(t, err)
		assert.Equal(t, 3, n)
		assert.Equal(t, int64(3), cr.N())

		n, err = cr.Read(buf)
		require.ErrorIs(t, err, expectedErr)
		assert.Equal(t, 0, n)
		assert.Equal(t, int64(3), cr.N())
	})

	t.Run("reset N does not reset the reader", func(t *testing.T) {
		data := []byte("0123456789")
		cr := util.NewCountingReader(bytes.NewReader(data))

		buf := make([]byte, 4)
		n, err := cr.Read(buf)
		require.NoError(t, err)
		require.Equal(t, 4, n)
		require.Equal(t, int64(4), cr.N())

		cr.Reset()
		assert.Equal(t, int64(0), cr.N())

		rest, err := io.ReadAll(cr)
		require.NoError(t, err)
		assert.Equal(t, data[4:], rest)
		assert.Equal(t, int64(len(data)-4), cr.N())
	})
}
