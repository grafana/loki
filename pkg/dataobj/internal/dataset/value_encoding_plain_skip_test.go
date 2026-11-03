package dataset

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/memory"
)

func Test_plainBytesDecoder_Skip(t *testing.T) {
	encode := func(t *testing.T, values ...string) []byte {
		t.Helper()

		var buf bytes.Buffer
		enc := newPlainBytesEncoder(&buf)
		for _, v := range values {
			require.NoError(t, enc.Encode(BinaryValue([]byte(v))))
		}
		return buf.Bytes()
	}

	t.Run("decodes the values after the skipped ones", func(t *testing.T) {
		dec := newPlainBytesDecoder(encode(t, "a", "", "ccc", "dddd", "e"))

		skipped, err := dec.Skip(3)
		require.NoError(t, err)
		require.Equal(t, 3, skipped)

		var alloc memory.Allocator
		arr, err := dec.Decode(&alloc, 2)
		require.NoError(t, err)

		values := arr.(*columnar.UTF8)
		require.Equal(t, 2, values.Len())
		require.Equal(t, "dddd", string(values.Get(0)))
		require.Equal(t, "e", string(values.Get(1)))
	})

	t.Run("skips fewer values when the data ends first", func(t *testing.T) {
		dec := newPlainBytesDecoder(encode(t, "a", "b"))

		skipped, err := dec.Skip(5)
		require.NoError(t, err)
		require.Equal(t, 2, skipped)

		var alloc memory.Allocator
		_, err = dec.Decode(&alloc, 1)
		require.ErrorIs(t, err, io.EOF)
	})

	t.Run("fails when a value overruns the data", func(t *testing.T) {
		data := encode(t, "hello")
		dec := newPlainBytesDecoder(data[:len(data)-2])

		_, err := dec.Skip(1)
		require.ErrorContains(t, err, "overruns")

		var alloc memory.Allocator
		_, err = newPlainBytesDecoder(data[:len(data)-2]).Decode(&alloc, 1)
		require.ErrorContains(t, err, "overruns")
	})

	t.Run("fails on a length that overflows an int", func(t *testing.T) {
		// A uvarint of 2^63, which a signed conversion turns into a negative length.
		data := append(bytes.Repeat([]byte{0x80}, 9), 0x01)
		data = append(data, "abc"...)

		_, err := newPlainBytesDecoder(data).Skip(1)
		require.ErrorContains(t, err, "overruns")

		var alloc memory.Allocator
		_, err = newPlainBytesDecoder(data).Decode(&alloc, 1)
		require.ErrorContains(t, err, "overruns")
	})

	t.Run("fails on a malformed length prefix", func(t *testing.T) {
		// A uvarint that is longer than 10 bytes never ends.
		data := bytes.Repeat([]byte{0xff}, 12)

		_, err := newPlainBytesDecoder(data).Skip(1)
		require.ErrorContains(t, err, "malformed")

		var alloc memory.Allocator
		_, err = newPlainBytesDecoder(data).Decode(&alloc, 1)
		require.ErrorContains(t, err, "malformed")
	})

	t.Run("fails on a length prefix that the data cuts short", func(t *testing.T) {
		// The last byte has its continuation bit set, so the prefix needs another byte.
		data := append(encode(t, "ab"), 0x80)

		dec := newPlainBytesDecoder(data)
		skipped, err := dec.Skip(2)
		require.Equal(t, 1, skipped)
		require.ErrorContains(t, err, "truncated")

		var alloc memory.Allocator
		_, err = newPlainBytesDecoder(data).Decode(&alloc, 2)
		require.ErrorContains(t, err, "truncated")
	})

	t.Run("sizes the values buffer for the decoded values only", func(t *testing.T) {
		long := string(bytes.Repeat([]byte("x"), 10_000))
		dec := newPlainBytesDecoder(encode(t, "a", "b", long))

		var alloc memory.Allocator
		arr, err := dec.Decode(&alloc, 2)
		require.NoError(t, err)
		require.Equal(t, 2, arr.Len())
		require.Less(t, alloc.AllocatedBytes(), 1024, "the buffer holds the two short values, not the long one")
	})

	t.Run("returns the values that exist with EOF when count is larger", func(t *testing.T) {
		dec := newPlainBytesDecoder(encode(t, "a", "bb"))

		var alloc memory.Allocator
		arr, err := dec.Decode(&alloc, 5)
		require.True(t, errors.Is(err, io.EOF))
		require.Equal(t, 2, arr.Len())
	})
}
