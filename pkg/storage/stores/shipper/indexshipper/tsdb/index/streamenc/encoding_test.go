// SPDX-License-Identifier: AGPL-3.0-only
// Provenance-includes-location: https://github.com/grafana/mimir/blob/main/pkg/storage/indexheader/encoding/encoding_test.go
// Provenance-includes-license: AGPL-3.0-only
// Provenance-includes-copyright: The Grafana Mimir Authors.

package encoding

import (
	"context"
	"fmt"
	"hash/crc32"
	"os"
	"path"
	"strconv"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	promencoding "github.com/prometheus/prometheus/tsdb/encoding"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"
	"github.com/thanos-io/objstore/providers/filesystem"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index/streamenc/filepool"
	"github.com/grafana/mimir/pkg/util/test"
)

func TestDecbuf_Be32HappyPath(t *testing.T) {
	cases := []uint32{
		0,
		1,
		0xFFFF_FFFF,
	}

	for _, c := range cases {
		tb := test.NewTB(t)
		caseName := strconv.FormatInt(int64(c), 10)

		enc := promencoding.Encbuf{}
		enc.PutBE32(c)

		runAllBufReaderTypes(tb, caseName, enc.Get(), func(tb test.TB, dec Decbuf) {
			require.Equal(t, 4, dec.Len())
			require.Equal(t, 0, dec.Offset())

			actual := dec.Be32()
			require.NoError(t, dec.Err())
			require.Equal(t, c, actual)
			require.Equal(t, 0, dec.Len())
			require.Equal(t, 4, dec.Offset())
		})
	}
}

func TestDecbuf_Be32InsufficientBuffer(t *testing.T) {
	tb := test.NewTB(t)

	enc := promencoding.Encbuf{}
	enc.PutBE32(0xFFFF_FFFF)

	runAllBufReaderTypes(tb, "", enc.Get()[:2], func(tb test.TB, dec Decbuf) {
		_ = dec.Be32()
		require.ErrorIs(t, dec.Err(), ErrInvalidSize)
	})
}

func FuzzDecbuf_Be32(f *testing.F) {
	f.Add(uint32(0))
	f.Add(uint32(1))
	f.Add(uint32(0xFFFF_FFFF))

	f.Fuzz(func(t *testing.T, n uint32) {
		tb := test.NewTB(t)

		enc := promencoding.Encbuf{}
		enc.PutBE32(n)

		runAllBufReaderTypes(tb, "", enc.Get(), func(tb test.TB, dec Decbuf) {
			require.NoError(t, dec.Err())
			require.Equal(t, 4, dec.Len())
			require.Equal(t, 0, dec.Offset())

			actual := dec.Be32()
			require.NoError(t, dec.Err())
			require.Equal(t, n, actual)
			require.Equal(t, 0, dec.Len())
			require.Equal(t, 4, dec.Offset())
		})
	})
}

func BenchmarkDecbuf_Be32(b *testing.B) {
	tb := test.NewTB(b)

	enc := promencoding.Encbuf{}
	enc.PutBE32(uint32(0))
	enc.PutBE32(uint32(1))
	enc.PutBE32(uint32(0xFFFF_FFFF))

	bytes := enc.Get()
	bytesCopy := append([]byte(nil), bytes...)

	runAllBufReaderTypes(tb, "", bytesCopy, func(tb test.TB, dec Decbuf) {
		tb.ResetTimer()

		for i := 0; i < tb.N(); i++ {
			v1 := dec.Be32()
			v2 := dec.Be32()
			v3 := dec.Be32()

			if err := dec.Err(); err != nil {
				require.NoError(tb, err)
			}

			if v1 != 0 || v2 != 0 || v3 != 0xFFFF_FFFF {
				require.Equal(tb, uint32(0), v1)
				require.Equal(tb, uint32(1), v2)
				require.Equal(tb, uint32(0xFFFF_FFFF), v3)
			}

			dec.ResetAt(0)
		}
	})
}

func TestDecbuf_Be32intHappyPath(t *testing.T) {
	cases := []int{
		0,
		1,
		0xFFFF_FFFF,
	}

	for _, c := range cases {
		tb := test.NewTB(t)
		caseName := strconv.FormatInt(int64(c), 10)

		enc := promencoding.Encbuf{}
		enc.PutBE32int(c)

		runAllBufReaderTypes(tb, caseName, enc.Get(), func(tb test.TB, dec Decbuf) {
			require.Equal(t, 4, dec.Len())
			require.Equal(t, 0, dec.Offset())

			actual := dec.Be32int()
			require.NoError(t, dec.Err())
			require.Equal(t, c, actual)
			require.Equal(t, 0, dec.Len())
			require.Equal(t, 4, dec.Offset())
		})
	}
}

func TestDecbuf_Be32intInsufficientBuffer(t *testing.T) {
	tb := test.NewTB(t)

	enc := promencoding.Encbuf{}
	enc.PutBE32int(0xFFFF_FFFF)

	runAllBufReaderTypes(tb, "", enc.Get()[:2], func(tb test.TB, dec Decbuf) {
		_ = dec.Be32int()
		require.ErrorIs(t, dec.Err(), ErrInvalidSize)
	})
}

func FuzzDecbuf_Be32int(f *testing.F) {
	f.Add(0)
	f.Add(1)
	f.Add(0xFFFF_FFFF)

	f.Fuzz(func(t *testing.T, n int) {
		if n < 0 || n > 0xFFFF_FFFF {
			t.Skip()
		}

		tb := test.NewTB(t)
		enc := promencoding.Encbuf{}
		enc.PutBE32int(n)

		runAllBufReaderTypes(tb, "", enc.Get(), func(tb test.TB, dec Decbuf) {
			require.NoError(t, dec.Err())
			require.Equal(t, 4, dec.Len())
			require.Equal(t, 0, dec.Offset())

			actual := dec.Be32int()
			require.NoError(t, dec.Err())
			require.Equal(t, n, actual)
			require.Equal(t, 0, dec.Len())
			require.Equal(t, 4, dec.Offset())
		})
	})
}

func TestDecbuf_Be64HappyPath(t *testing.T) {
	cases := []uint64{
		0,
		1,
		0xFFFF_FFFF_FFFF_FFFF,
	}

	for _, c := range cases {
		tb := test.NewTB(t)
		caseName := strconv.FormatUint(c, 10)

		enc := promencoding.Encbuf{}
		enc.PutBE64(c)

		runAllBufReaderTypes(tb, caseName, enc.Get(), func(tb test.TB, dec Decbuf) {
			require.Equal(t, 8, dec.Len())
			require.Equal(t, 0, dec.Offset())

			actual := dec.Be64()
			require.NoError(t, dec.Err())
			require.Equal(t, c, actual)
			require.Equal(t, 0, dec.Len())
			require.Equal(t, 8, dec.Offset())
		})
	}
}

func TestDecbuf_Be64InsufficientBuffer(t *testing.T) {
	tb := test.NewTB(t)
	runAllBufReaderTypes(tb, "", []byte{0x01}, func(tb test.TB, dec Decbuf) {
		_ = dec.Be64()
		require.ErrorIs(t, dec.Err(), ErrInvalidSize)
	})
}

func FuzzDecbuf_Be64(f *testing.F) {
	f.Add(uint64(0))
	f.Add(uint64(1))
	f.Add(uint64(0xFFFF_FFFF_FFFF_FFFF))

	f.Fuzz(func(t *testing.T, n uint64) {
		tb := test.NewTB(t)

		enc := promencoding.Encbuf{}
		enc.PutBE64(n)

		runAllBufReaderTypes(tb, "", enc.Get(), func(tb test.TB, dec Decbuf) {
			require.NoError(t, dec.Err())
			require.Equal(t, 8, dec.Len())
			require.Equal(t, 0, dec.Offset())

			actual := dec.Be64()
			require.NoError(t, dec.Err())
			require.Equal(t, n, actual)
			require.Equal(t, 0, dec.Len())
			require.Equal(t, 8, dec.Offset())
		})
	})
}

func BenchmarkDecbuf_Be64(t *testing.B) {
	tb := test.NewTB(t)

	enc := promencoding.Encbuf{}
	enc.PutBE64(uint64(0))
	enc.PutBE64(uint64(1))
	enc.PutBE64(uint64(0xFFFF_FFFF_FFFF_FFFF))

	b := enc.Get()
	bCopy := append([]byte(nil), b...)

	runAllBufReaderTypes(tb, "", bCopy, func(tb test.TB, dec Decbuf) {
		t.ResetTimer()

		for i := 0; i < t.N; i++ {
			v1 := dec.Be64()
			v2 := dec.Be64()
			v3 := dec.Be64()

			if err := dec.Err(); err != nil {
				require.NoError(t, err)
			}

			if v1 != 0 || v2 != 0 || v3 != 0xFFFF_FFFF_FFFF_FFFF {
				require.Equal(t, uint64(0), v1)
				require.Equal(t, uint64(1), v2)
				require.Equal(t, uint64(0xFFFF_FFFF_FFFF_FFFF), v3)
			}

			dec.ResetAt(0)
		}
	})
}

func TestDecbuf_SkipHappyPath(t *testing.T) {
	tb := test.NewTB(t)

	expected := uint32(0x12345678)

	enc := promencoding.Encbuf{}
	enc.PutBE32(0xFFFF_FFFF)
	enc.PutBE32(expected)

	runAllBufReaderTypes(tb, "", enc.Get(), func(tb test.TB, dec Decbuf) {
		require.Equal(t, 8, dec.Len())
		require.Equal(t, 0, dec.Offset())

		dec.Skip(4)
		require.NoError(t, dec.Err())
		require.Equal(t, 4, dec.Len())
		require.Equal(t, 4, dec.Offset())

		actual := dec.Be32()
		require.NoError(t, dec.Err())
		require.Equal(t, expected, actual)
		require.Equal(t, 0, dec.Len())
		require.Equal(t, 8, dec.Offset())
	})
}

func TestDecbuf_SkipMultipleBufferReads(t *testing.T) {
	tb := test.NewTB(t)

	// The underlying FileReader buffers the file 4k bytes at a time. Ensure
	// that we can skip multiple 4k chunks without ending up with a short read.
	bytes := make([]byte, 4096*5)
	for i := 0; i < len(bytes); i++ {
		bytes[i] = 0x01
	}

	runAllBufReaderTypes(tb, "", bytes, func(tb test.TB, dec Decbuf) {
		dec.Skip(4096 * 4)

		require.NoError(t, dec.Err())
		require.Equal(t, dec.Len(), 4096)
		require.Equal(t, 4096*4, dec.Offset())
		require.Equal(t, byte(0x01), dec.Byte())
	})
}

func TestDecbuf_SkipInsufficientBuffer(t *testing.T) {
	tb := test.NewTB(t)
	runAllBufReaderTypes(tb, "", []byte{0x01}, func(tb test.TB, dec Decbuf) {
		dec.Skip(2)
		require.ErrorIs(t, dec.Err(), ErrInvalidSize)
	})
}

func TestDecbuf_SkipUvarintBytesHappyPath(t *testing.T) {
	tb := test.NewTB(t)

	expected := uint32(0x567890AB)
	enc := promencoding.Encbuf{}
	enc.PutUvarintBytes([]byte{0x12, 0x34})
	enc.PutBE32(expected)

	runAllBufReaderTypes(tb, "", enc.Get(), func(tb test.TB, dec Decbuf) {
		require.Equal(t, 7, dec.Len())
		require.Equal(t, 0, dec.Offset())

		dec.SkipUvarintBytes()
		require.NoError(t, dec.Err())
		require.Equal(t, 4, dec.Len())
		require.Equal(t, 3, dec.Offset())

		actual := dec.Be32()
		require.NoError(t, dec.Err())
		require.Equal(t, expected, actual)
	})
}

func TestDecbuf_SkipUvarintBytesEndOfFile(t *testing.T) {
	tb := test.NewTB(t)

	enc := promencoding.Encbuf{}
	enc.PutBE32(0x12345678)

	runAllBufReaderTypes(tb, "", enc.Get(), func(tb test.TB, dec Decbuf) {
		dec.Be32()
		require.NoError(t, dec.Err())
		require.Equal(t, 0, dec.Len())
		require.Equal(t, 4, dec.Offset())

		dec.SkipUvarintBytes()
		require.ErrorIs(t, dec.Err(), ErrInvalidSize)
	})
}

func TestDecbuf_SkipUvarintBytesOnlyHaveLength(t *testing.T) {
	tb := test.NewTB(t)

	enc := promencoding.Encbuf{}
	enc.PutUvarintBytes([]byte{0x12, 0x34})

	bytes := enc.Get()

	runAllBufReaderTypes(tb, "", bytes[:len(bytes)-2], func(tb test.TB, dec Decbuf) {
		require.Equal(t, 1, dec.Len())
		require.Equal(t, 0, dec.Offset())

		dec.SkipUvarintBytes()
		require.ErrorIs(t, dec.Err(), ErrInvalidSize)
	})
}

func TestDecbuf_SkipUvarintBytesPartialValue(t *testing.T) {
	tb := test.NewTB(t)

	enc := promencoding.Encbuf{}
	enc.PutUvarintBytes([]byte{0x12, 0x34})

	bytes := enc.Get()

	runAllBufReaderTypes(tb, "", bytes[:len(bytes)-1], func(tb test.TB, dec Decbuf) {
		require.Equal(t, 2, dec.Len())
		require.Equal(t, 0, dec.Offset())

		dec.SkipUvarintBytes()
		require.ErrorIs(t, dec.Err(), ErrInvalidSize)
	})
}

func TestDecbuf_UvarintHappyPath(t *testing.T) {
	cases := []struct {
		value int
		bytes int
	}{
		{value: 0, bytes: 1},
		{value: 1, bytes: 1},
		{value: 127, bytes: 1},
		{value: 128, bytes: 2},
		{value: 0xFFFF_FFFF, bytes: 5},
	}

	for _, c := range cases {
		tb := test.NewTB(t)
		caseName := strconv.Itoa(c.value)

		enc := promencoding.Encbuf{}
		enc.PutUvarint(c.value)

		runAllBufReaderTypes(tb, caseName, enc.Get(), func(tb test.TB, dec Decbuf) {
			require.Equal(t, c.bytes, dec.Len())
			require.Equal(t, 0, dec.Offset())

			actual := dec.Uvarint()
			require.NoError(t, dec.Err())
			require.Equal(t, c.value, actual)
			require.Equal(t, 0, dec.Len())
			require.Equal(t, c.bytes, dec.Offset())
		})
	}
}

func TestDecbuf_UvarintInsufficientBuffer(t *testing.T) {
	tb := test.NewTB(t)

	enc := promencoding.Encbuf{}
	enc.PutUvarint(0xFFFF_FFFF)

	runAllBufReaderTypes(tb, "", enc.Get()[:2], func(tb test.TB, dec Decbuf) {
		_ = dec.Uvarint()
		require.ErrorIs(t, dec.Err(), ErrInvalidSize)
	})
}

func FuzzDecbuf_Uvarint(f *testing.F) {
	f.Add(0)
	f.Add(1)
	f.Add(0xFFFF_FFFF)

	f.Fuzz(func(t *testing.T, n int) {
		if n < 0 {
			t.Skip()
		}
		tb := test.NewTB(t)

		enc := promencoding.Encbuf{}
		enc.PutUvarint(n)

		runAllBufReaderTypes(tb, "", enc.Get(), func(tb test.TB, dec Decbuf) {
			require.NoError(t, dec.Err())
			actual := dec.Uvarint()
			require.NoError(t, dec.Err())
			require.Equal(t, n, actual)
			require.Equal(t, 0, dec.Len())
		})
	})
}

func TestDecbuf_Uvarint64HappyPath(t *testing.T) {
	cases := []struct {
		value uint64
		bytes int
	}{
		{value: 0, bytes: 1},
		{value: 1, bytes: 1},
		{value: 127, bytes: 1},
		{value: 128, bytes: 2},
		{value: 0xFFFF_FFFF, bytes: 5},
		{value: 0xFFFF_FFFF_FFFF_FFFF, bytes: 10},
	}

	for _, c := range cases {
		tb := test.NewTB(t)
		caseName := strconv.FormatUint(c.value, 10)

		enc := promencoding.Encbuf{}
		enc.PutUvarint64(c.value)

		runAllBufReaderTypes(tb, caseName, enc.Get(), func(tb test.TB, dec Decbuf) {
			require.Equal(t, c.bytes, dec.Len())
			require.Equal(t, 0, dec.Offset())

			actual := dec.Uvarint64()
			require.NoError(t, dec.Err())
			require.Equal(t, c.value, actual)
			require.Equal(t, 0, dec.Len())
			require.Equal(t, c.bytes, dec.Offset())
		})
	}
}

func TestDecbuf_Uvarint64InsufficientBuffer(t *testing.T) {
	tb := test.NewTB(t)

	enc := promencoding.Encbuf{}
	enc.PutUvarint64(0xFFFF_FFFF)

	runAllBufReaderTypes(tb, "", enc.Get()[:2], func(tb test.TB, dec Decbuf) {
		_ = dec.Uvarint64()
		require.ErrorIs(t, dec.Err(), ErrInvalidSize)
	})
}

func FuzzDecbuf_Uvarint64(f *testing.F) {
	f.Add(uint64(0))
	f.Add(uint64(1))
	f.Add(uint64(127))
	f.Add(uint64(128))
	f.Add(uint64(0xFFFF_FFFF))
	f.Add(uint64(0xFFFF_FFFF_FFFF_FFFF))

	f.Fuzz(func(t *testing.T, n uint64) {
		tb := test.NewTB(t)

		enc := promencoding.Encbuf{}
		enc.PutUvarint64(n)

		runAllBufReaderTypes(tb, "", enc.Get(), func(tb test.TB, dec Decbuf) {
			require.NoError(t, dec.Err())
			actual := dec.Uvarint64()
			require.NoError(t, dec.Err())
			require.Equal(t, n, actual)
			require.Equal(t, 0, dec.Len())
		})
	})
}

func TestDecbuf_UnsafeUvarintBytesHappyPath(t *testing.T) {
	cases := []struct {
		name              string
		value             []byte
		encodedSizeLength int
	}{
		{name: "nil slice", value: []byte(nil), encodedSizeLength: 1},
		{name: "empty slice", value: []byte{}, encodedSizeLength: 1},
		{name: "single byte", value: []byte{0x12}, encodedSizeLength: 1},
		{name: "127 bytes", value: []byte("1234567890123456789012345678901234567890123456789012345678901234567890123456789012345678901234567890123456789012345678901234567"), encodedSizeLength: 1},
		{name: "128 bytes", value: []byte("12345678901234567890123456789012345678901234567890123456789012345678901234567890123456789012345678901234567890123456789012345678"), encodedSizeLength: 2},
	}

	for _, c := range cases {
		tb := test.NewTB(t)

		enc := promencoding.Encbuf{}
		enc.PutUvarintBytes(c.value)

		runAllBufReaderTypes(tb, c.name, enc.Get(), func(tb test.TB, dec Decbuf) {
			size := c.encodedSizeLength + len(c.value)
			require.Equal(t, size, dec.Len())
			require.Equal(t, 0, dec.Offset())

			actual := dec.UnsafeUvarintBytes()
			require.NoError(t, dec.Err())
			require.Condition(t, test.EqualSlices(c.value, actual), "%#v != %#v", c.value, actual)
			require.Equal(t, 0, dec.Len())
			require.Equal(t, size, dec.Offset())
		})
	}
}

func TestDecbuf_UnsafeUvarintBytesInsufficientBuffer(t *testing.T) {
	tb := test.NewTB(t)

	enc := promencoding.Encbuf{}
	enc.PutUvarintBytes([]byte("123456"))

	runAllBufReaderTypes(tb, "", enc.Get()[:2], func(tb test.TB, dec Decbuf) {
		_ = dec.UnsafeUvarintBytes()
		require.ErrorIs(t, dec.Err(), ErrInvalidSize)
	})
}

func TestDecbuf_UnsafeUvarintBytesSkipDoesNotCauseBufferFill(t *testing.T) {
	tb := test.NewTB(t)
	const (
		expectedSlices = 32
		expectedBytes  = 983
	)

	// This test verifies that when bytes are read in UnsafeUvarintBytes, the Peek(n) and
	// subsequent skip(len(b)) does not cause a read from disk that invalidates the slice
	// returned. It does this by creating multiple uvarint byte slices in the encoding
	// buffer each with different content _and_ by ensuring there are more bytes written
	// in total than the size of the underlying buffer (currently 4k).

	enc := promencoding.Encbuf{}
	for base := 0; base < expectedSlices; base++ {
		var bytes []byte
		for i := 0; i < expectedBytes; i++ {
			bytes = append(bytes, byte(base))
		}

		enc.PutUvarintBytes(bytes)
	}

	runAllBufReaderTypes(tb, "", enc.Get(), func(tb test.TB, dec Decbuf) {
		for base := 0; base < expectedSlices; base++ {
			bytes := dec.UnsafeUvarintBytes()

			require.NoError(t, dec.Err())
			require.Len(t, bytes, expectedBytes)

			for _, v := range bytes {
				require.Equal(t, byte(base), v)
			}
		}
	})
}

func FuzzDecbuf_UnsafeUvarintBytes(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0x12})
	f.Add([]byte("1234567890123456789012345678901234567890123456789012345678901234567890123456789012345678901234567890123456789012345678901234567"))
	f.Add([]byte("12345678901234567890123456789012345678901234567890123456789012345678901234567890123456789012345678901234567890123456789012345678"))

	f.Fuzz(func(t *testing.T, b []byte) {
		tb := test.NewTB(t)

		enc := promencoding.Encbuf{}
		enc.PutUvarintBytes(b)

		runAllBufReaderTypes(tb, "", enc.Get(), func(tb test.TB, dec Decbuf) {
			require.NoError(t, dec.Err())
			actual := dec.UnsafeUvarintBytes()
			require.NoError(t, dec.Err())
			require.Condition(t, test.EqualSlices(b, actual), "%#v != %#v", b, actual)
			require.Equal(t, 0, dec.Len())
		})
	})
}

func BenchmarkDecbuf_UnsafeUvarintBytes(t *testing.B) {
	tb := test.NewTB(t)

	// 127 bytes, the varint size will be 1 byte.
	val := []byte("1234567890123456789012345678901234567890123456789012345678901234567890123456789012345678901234567890123456789012345678901234567")
	enc := promencoding.Encbuf{}
	enc.PutUvarintBytes(val)

	bytes := enc.Get()
	byteCopy := append([]byte(nil), bytes...)

	runAllBufReaderTypes(tb, "", byteCopy, func(tb test.TB, dec Decbuf) {
		t.ResetTimer()

		for i := 0; i < t.N; i++ {
			b := dec.UnsafeUvarintBytes()
			if err := dec.Err(); err != nil {
				require.NoError(t, err)
			}

			if len(b) != len(val) {
				require.Len(t, b, len(val))
			}

			dec.ResetAt(0)
			if err := dec.Err(); err != nil {
				require.NoError(t, err)
			}
		}
	})
}

func TestDecbuf_UvarintStrHappyPath(t *testing.T) {
	cases := []struct {
		name              string
		value             string
		encodedSizeLength int
	}{
		{name: "empty string", value: "", encodedSizeLength: 1},
		{name: "single byte", value: "a", encodedSizeLength: 1},
		{name: "127 bytes", value: "1234567890123456789012345678901234567890123456789012345678901234567890123456789012345678901234567890123456789012345678901234567", encodedSizeLength: 1},
		{name: "128 bytes", value: "12345678901234567890123456789012345678901234567890123456789012345678901234567890123456789012345678901234567890123456789012345678", encodedSizeLength: 2},
	}

	for _, c := range cases {
		tb := test.NewTB(t)

		enc := promencoding.Encbuf{}
		enc.PutUvarintStr(c.value)

		runAllBufReaderTypes(tb, c.name, enc.Get(), func(tb test.TB, dec Decbuf) {
			size := c.encodedSizeLength + len(c.value)
			require.Equal(t, size, dec.Len())
			require.Equal(t, 0, dec.Offset())

			actual := dec.UvarintStr()
			require.NoError(t, dec.Err())
			require.Equal(t, c.value, actual)
			require.Equal(t, 0, dec.Len())
			require.Equal(t, size, dec.Offset())
		})
	}
}

func TestDecbuf_UvarintStrInsufficientBuffer(t *testing.T) {
	tb := test.NewTB(t)

	enc := promencoding.Encbuf{}
	enc.PutUvarintStr("123456")

	runAllBufReaderTypes(tb, "", enc.Get()[:2], func(tb test.TB, dec Decbuf) {
		_ = dec.UvarintStr()
		require.ErrorIs(t, dec.Err(), ErrInvalidSize)
	})
}

func FuzzDecbuf_UvarintStr(f *testing.F) {
	f.Add("")
	f.Add("a")
	f.Add("1234567890123456789012345678901234567890123456789012345678901234567890123456789012345678901234567890123456789012345678901234567")
	f.Add("12345678901234567890123456789012345678901234567890123456789012345678901234567890123456789012345678901234567890123456789012345678")

	f.Fuzz(func(t *testing.T, s string) {
		tb := test.NewTB(t)

		enc := promencoding.Encbuf{}
		enc.PutUvarintStr(s)

		runAllBufReaderTypes(tb, "", enc.Get(), func(tb test.TB, dec Decbuf) {
			require.NoError(t, dec.Err())
			actual := dec.UvarintStr()
			require.NoError(t, dec.Err())
			require.Equal(t, s, actual)
			require.Equal(t, 0, dec.Len())
		})
	})
}

func TestDecbuf_Crc32(t *testing.T) {
	table := crc32.MakeTable(crc32.Castagnoli)

	t.Run("matches checksum (small buffer)", func(t *testing.T) {
		tb := test.NewTB(t)
		bytes := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x4f, 0x4d, 0xfb, 0xab}
		runAllBufReaderTypes(tb, "", bytes, func(tb test.TB, dec Decbuf) {
			dec.CheckCrc32(table)
			require.NoError(t, dec.Err())
		})
	})

	t.Run("matches checksum (buffer larger than single read)", func(t *testing.T) {
		tb := test.NewTB(t)

		bufferSize := 4*1024*1024 + 1
		enc := promencoding.Encbuf{}

		for enc.Len() < bufferSize {
			enc.PutByte(0x01)
		}
		enc.PutHash(crc32.New(crc32.MakeTable(crc32.Castagnoli)))

		runAllBufReaderTypes(tb, "", enc.Get(), func(tb test.TB, dec Decbuf) {
			dec.CheckCrc32(table)
			require.NoError(t, dec.Err())
		})
	})

	t.Run("does not match checksum (small buffer)", func(t *testing.T) {
		tb := test.NewTB(t)

		runAllBufReaderTypes(tb, "", []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x4f, 0x4d, 0xfb, 0xff}, func(tb test.TB, dec Decbuf) {
			dec.CheckCrc32(table)
			require.ErrorIs(t, dec.Err(), ErrInvalidChecksum)
		})
	})

	t.Run("does not match checksum (buffer larger than single read)", func(t *testing.T) {
		tb := test.NewTB(t)

		bufferSize := 4*1024*1024 + 1
		enc := promencoding.Encbuf{}

		for enc.Len() < bufferSize {
			enc.PutByte(0x01)
		}

		bytes := enc.Get()
		bytes = append(bytes, 0x00, 0x01, 0x02, 0x03)

		runAllBufReaderTypes(tb, "", bytes, func(tb test.TB, dec Decbuf) {
			dec.CheckCrc32(table)
			require.ErrorIs(t, dec.Err(), ErrInvalidChecksum)
		})
	})

	t.Run("buffer only contains checksum", func(t *testing.T) {
		tb := test.NewTB(t)

		runAllBufReaderTypes(tb, "", []byte{0x4f, 0x4d, 0xfb, 0xab}, func(tb test.TB, dec Decbuf) {
			dec.CheckCrc32(table)
			require.ErrorIs(t, dec.Err(), ErrInvalidSize)
		})
	})

	t.Run("buffer too short for checksum", func(t *testing.T) {
		tb := test.NewTB(t)

		runAllBufReaderTypes(tb, "", []byte{0x4f, 0x4d, 0xfb}, func(tb test.TB, dec Decbuf) {
			dec.CheckCrc32(table)
			require.ErrorIs(t, dec.Err(), ErrInvalidSize)
		})
	})
}

func runAllBufReaderTypes(tb test.TB, caseName string, bytes []byte, testFn func(tb test.TB, dec Decbuf)) {
	dir := tb.TempDir()
	fileName := "test-file"
	filePath := path.Join(dir, fileName)
	require.NoError(tb, os.WriteFile(filePath, bytes, 0700))

	reg := prometheus.WrapRegistererWithPrefix("indexheader_", prometheus.NewPedanticRegistry())
	diskFactory := NewFilePoolDecbufFactory(filePath, 0, filepool.NewFilePoolMetrics(reg))
	tb.Cleanup(func() {
		_ = diskFactory.Close()
	})
	diskDecBuf := diskFactory.NewRawDecbuf(context.Background())
	require.NoError(tb, diskDecBuf.Err())

	bkt, err := filesystem.NewBucket(dir)
	require.NoError(tb, err)
	instBkt := objstore.WithNoopInstr(bkt)
	tb.Cleanup(func() {
		require.NoError(tb, bkt.Close())
	})
	bucketFactory := NewBucketDecbufFactory(instBkt, fileName)
	bucketDecBuf := bucketFactory.NewRawDecbuf(context.Background())
	require.NoError(tb, bucketDecBuf.Err())

	decbufs := map[string]Decbuf{
		"disk":   diskDecBuf,
		"bucket": bucketDecBuf,
	}

	for decbufName, decbuf := range decbufs {
		name := caseName
		if len(name) > 0 {
			name += "/"
		}
		name += fmt.Sprintf("BufReader=%s", decbufName)

		tb.Run(name, func(tb test.TB) {

			testFn(tb, decbuf)
		})
	}
}
