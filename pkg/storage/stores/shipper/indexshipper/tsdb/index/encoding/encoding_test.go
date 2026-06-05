// SPDX-License-Identifier: AGPL-3.0-only
// Copied from: https://github.com/grafana/mimir/blob/main/pkg/storage/indexheader/encoding/encoding_test.go
// Bucket-based reader tests omitted. Mimir's internal test.TB abstraction replaced with
// standard testing.T/B; benchmarks use newBenchDecbuf directly instead of the helper.

package encoding

import (
	"bytes"
	"fmt"
	"hash/crc32"
	"os"
	"path"
	"strconv"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	promencoding "github.com/prometheus/prometheus/tsdb/encoding"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index/filepool"
)

func TestDecbuf_Be32HappyPath(t *testing.T) {
	cases := []uint32{
		0,
		1,
		0xFFFF_FFFF,
	}

	for _, c := range cases {
		caseName := strconv.FormatInt(int64(c), 10)

		enc := promencoding.Encbuf{}
		enc.PutBE32(c)

		runAllBufReaderTypes(t, caseName, enc.Get(), func(t *testing.T, dec Decbuf) {
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
	enc := promencoding.Encbuf{}
	enc.PutBE32(0xFFFF_FFFF)

	runAllBufReaderTypes(t, "", enc.Get()[:2], func(t *testing.T, dec Decbuf) {
		_ = dec.Be32()
		require.ErrorIs(t, dec.Err(), ErrInvalidSize)
	})
}

func FuzzDecbuf_Be32(f *testing.F) {
	f.Add(uint32(0))
	f.Add(uint32(1))
	f.Add(uint32(0xFFFF_FFFF))

	f.Fuzz(func(t *testing.T, n uint32) {
		enc := promencoding.Encbuf{}
		enc.PutBE32(n)

		runAllBufReaderTypes(t, "", enc.Get(), func(t *testing.T, dec Decbuf) {
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
	enc := promencoding.Encbuf{}
	enc.PutBE32(uint32(0))
	enc.PutBE32(uint32(1))
	enc.PutBE32(uint32(0xFFFF_FFFF))

	data := append([]byte(nil), enc.Get()...)
	dec := newBenchDecbuf(b, data)
	defer dec.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v1 := dec.Be32()
		v2 := dec.Be32()
		v3 := dec.Be32()

		if err := dec.Err(); err != nil {
			require.NoError(b, err)
		}

		if v1 != 0 || v2 != 0 || v3 != 0xFFFF_FFFF {
			require.Equal(b, uint32(0), v1)
			require.Equal(b, uint32(1), v2)
			require.Equal(b, uint32(0xFFFF_FFFF), v3)
		}

		dec.ResetAt(0)
	}
}

func TestDecbuf_Be32intHappyPath(t *testing.T) {
	cases := []int{
		0,
		1,
		0xFFFF_FFFF,
	}

	for _, c := range cases {
		caseName := strconv.FormatInt(int64(c), 10)

		enc := promencoding.Encbuf{}
		enc.PutBE32int(c)

		runAllBufReaderTypes(t, caseName, enc.Get(), func(t *testing.T, dec Decbuf) {
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
	enc := promencoding.Encbuf{}
	enc.PutBE32int(0xFFFF_FFFF)

	runAllBufReaderTypes(t, "", enc.Get()[:2], func(t *testing.T, dec Decbuf) {
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

		enc := promencoding.Encbuf{}
		enc.PutBE32int(n)

		runAllBufReaderTypes(t, "", enc.Get(), func(t *testing.T, dec Decbuf) {
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
		caseName := strconv.FormatUint(c, 10)

		enc := promencoding.Encbuf{}
		enc.PutBE64(c)

		runAllBufReaderTypes(t, caseName, enc.Get(), func(t *testing.T, dec Decbuf) {
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
	runAllBufReaderTypes(t, "", []byte{0x01}, func(t *testing.T, dec Decbuf) {
		_ = dec.Be64()
		require.ErrorIs(t, dec.Err(), ErrInvalidSize)
	})
}

func FuzzDecbuf_Be64(f *testing.F) {
	f.Add(uint64(0))
	f.Add(uint64(1))
	f.Add(uint64(0xFFFF_FFFF_FFFF_FFFF))

	f.Fuzz(func(t *testing.T, n uint64) {
		enc := promencoding.Encbuf{}
		enc.PutBE64(n)

		runAllBufReaderTypes(t, "", enc.Get(), func(t *testing.T, dec Decbuf) {
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

func BenchmarkDecbuf_Be64(b *testing.B) {
	enc := promencoding.Encbuf{}
	enc.PutBE64(uint64(0))
	enc.PutBE64(uint64(1))
	enc.PutBE64(uint64(0xFFFF_FFFF_FFFF_FFFF))

	data := append([]byte(nil), enc.Get()...)
	dec := newBenchDecbuf(b, data)
	defer dec.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v1 := dec.Be64()
		v2 := dec.Be64()
		v3 := dec.Be64()

		if err := dec.Err(); err != nil {
			require.NoError(b, err)
		}

		if v1 != 0 || v2 != 0 || v3 != 0xFFFF_FFFF_FFFF_FFFF {
			require.Equal(b, uint64(0), v1)
			require.Equal(b, uint64(1), v2)
			require.Equal(b, uint64(0xFFFF_FFFF_FFFF_FFFF), v3)
		}

		dec.ResetAt(0)
	}
}

func TestDecbuf_SkipHappyPath(t *testing.T) {
	expected := uint32(0x12345678)

	enc := promencoding.Encbuf{}
	enc.PutBE32(0xFFFF_FFFF)
	enc.PutBE32(expected)

	runAllBufReaderTypes(t, "", enc.Get(), func(t *testing.T, dec Decbuf) {
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
	// The underlying FileReader buffers the file 4k bytes at a time. Ensure
	// that we can skip multiple 4k chunks without ending up with a short read.
	data := make([]byte, 4096*5)
	for i := range data {
		data[i] = 0x01
	}

	runAllBufReaderTypes(t, "", data, func(t *testing.T, dec Decbuf) {
		dec.Skip(4096 * 4)

		require.NoError(t, dec.Err())
		require.Equal(t, dec.Len(), 4096)
		require.Equal(t, 4096*4, dec.Offset())
		require.Equal(t, byte(0x01), dec.Byte())
	})
}

func TestDecbuf_SkipInsufficientBuffer(t *testing.T) {
	runAllBufReaderTypes(t, "", []byte{0x01}, func(t *testing.T, dec Decbuf) {
		dec.Skip(2)
		require.ErrorIs(t, dec.Err(), ErrInvalidSize)
	})
}

func TestDecbuf_SkipUvarintBytesHappyPath(t *testing.T) {
	expected := uint32(0x567890AB)
	enc := promencoding.Encbuf{}
	enc.PutUvarintBytes([]byte{0x12, 0x34})
	enc.PutBE32(expected)

	runAllBufReaderTypes(t, "", enc.Get(), func(t *testing.T, dec Decbuf) {
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
	enc := promencoding.Encbuf{}
	enc.PutBE32(0x12345678)

	runAllBufReaderTypes(t, "", enc.Get(), func(t *testing.T, dec Decbuf) {
		dec.Be32()
		require.NoError(t, dec.Err())
		require.Equal(t, 0, dec.Len())
		require.Equal(t, 4, dec.Offset())

		dec.SkipUvarintBytes()
		require.ErrorIs(t, dec.Err(), ErrInvalidSize)
	})
}

func TestDecbuf_SkipUvarintBytesOnlyHaveLength(t *testing.T) {
	enc := promencoding.Encbuf{}
	enc.PutUvarintBytes([]byte{0x12, 0x34})

	data := enc.Get()

	runAllBufReaderTypes(t, "", data[:len(data)-2], func(t *testing.T, dec Decbuf) {
		require.Equal(t, 1, dec.Len())
		require.Equal(t, 0, dec.Offset())

		dec.SkipUvarintBytes()
		require.ErrorIs(t, dec.Err(), ErrInvalidSize)
	})
}

func TestDecbuf_SkipUvarintBytesPartialValue(t *testing.T) {
	enc := promencoding.Encbuf{}
	enc.PutUvarintBytes([]byte{0x12, 0x34})

	data := enc.Get()

	runAllBufReaderTypes(t, "", data[:len(data)-1], func(t *testing.T, dec Decbuf) {
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
		caseName := strconv.Itoa(c.value)

		enc := promencoding.Encbuf{}
		enc.PutUvarint(c.value)

		runAllBufReaderTypes(t, caseName, enc.Get(), func(t *testing.T, dec Decbuf) {
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
	enc := promencoding.Encbuf{}
	enc.PutUvarint(0xFFFF_FFFF)

	runAllBufReaderTypes(t, "", enc.Get()[:2], func(t *testing.T, dec Decbuf) {
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

		enc := promencoding.Encbuf{}
		enc.PutUvarint(n)

		runAllBufReaderTypes(t, "", enc.Get(), func(t *testing.T, dec Decbuf) {
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
		caseName := strconv.FormatUint(c.value, 10)

		enc := promencoding.Encbuf{}
		enc.PutUvarint64(c.value)

		runAllBufReaderTypes(t, caseName, enc.Get(), func(t *testing.T, dec Decbuf) {
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
	enc := promencoding.Encbuf{}
	enc.PutUvarint64(0xFFFF_FFFF)

	runAllBufReaderTypes(t, "", enc.Get()[:2], func(t *testing.T, dec Decbuf) {
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
		enc := promencoding.Encbuf{}
		enc.PutUvarint64(n)

		runAllBufReaderTypes(t, "", enc.Get(), func(t *testing.T, dec Decbuf) {
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
		enc := promencoding.Encbuf{}
		enc.PutUvarintBytes(c.value)

		runAllBufReaderTypes(t, c.name, enc.Get(), func(t *testing.T, dec Decbuf) {
			size := c.encodedSizeLength + len(c.value)
			require.Equal(t, size, dec.Len())
			require.Equal(t, 0, dec.Offset())

			actual := dec.UnsafeUvarintBytes()
			require.NoError(t, dec.Err())
			require.True(t, bytes.Equal(c.value, actual), "%#v != %#v", c.value, actual)
			require.Equal(t, 0, dec.Len())
			require.Equal(t, size, dec.Offset())
		})
	}
}

func TestDecbuf_UnsafeUvarintBytesInsufficientBuffer(t *testing.T) {
	enc := promencoding.Encbuf{}
	enc.PutUvarintBytes([]byte("123456"))

	runAllBufReaderTypes(t, "", enc.Get()[:2], func(t *testing.T, dec Decbuf) {
		_ = dec.UnsafeUvarintBytes()
		require.ErrorIs(t, dec.Err(), ErrInvalidSize)
	})
}

func TestDecbuf_UnsafeUvarintBytesSkipDoesNotCauseBufferFill(t *testing.T) {
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
		var chunk []byte
		for i := 0; i < expectedBytes; i++ {
			chunk = append(chunk, byte(base))
		}

		enc.PutUvarintBytes(chunk)
	}

	runAllBufReaderTypes(t, "", enc.Get(), func(t *testing.T, dec Decbuf) {
		for base := 0; base < expectedSlices; base++ {
			b := dec.UnsafeUvarintBytes()

			require.NoError(t, dec.Err())
			require.Len(t, b, expectedBytes)

			for _, v := range b {
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
		enc := promencoding.Encbuf{}
		enc.PutUvarintBytes(b)

		runAllBufReaderTypes(t, "", enc.Get(), func(t *testing.T, dec Decbuf) {
			require.NoError(t, dec.Err())
			actual := dec.UnsafeUvarintBytes()
			require.NoError(t, dec.Err())
			require.True(t, bytes.Equal(b, actual), "%#v != %#v", b, actual)
			require.Equal(t, 0, dec.Len())
		})
	})
}

func BenchmarkDecbuf_UnsafeUvarintBytes(b *testing.B) {
	// 127 bytes, the varint size will be 1 byte.
	val := []byte("1234567890123456789012345678901234567890123456789012345678901234567890123456789012345678901234567890123456789012345678901234567")
	enc := promencoding.Encbuf{}
	enc.PutUvarintBytes(val)

	data := append([]byte(nil), enc.Get()...)
	dec := newBenchDecbuf(b, data)
	defer dec.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := dec.UnsafeUvarintBytes()
		if err := dec.Err(); err != nil {
			require.NoError(b, err)
		}

		if len(result) != len(val) {
			require.Len(b, result, len(val))
		}

		dec.ResetAt(0)
		if err := dec.Err(); err != nil {
			require.NoError(b, err)
		}
	}
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
		enc := promencoding.Encbuf{}
		enc.PutUvarintStr(c.value)

		runAllBufReaderTypes(t, c.name, enc.Get(), func(t *testing.T, dec Decbuf) {
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
	enc := promencoding.Encbuf{}
	enc.PutUvarintStr("123456")

	runAllBufReaderTypes(t, "", enc.Get()[:2], func(t *testing.T, dec Decbuf) {
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
		enc := promencoding.Encbuf{}
		enc.PutUvarintStr(s)

		runAllBufReaderTypes(t, "", enc.Get(), func(t *testing.T, dec Decbuf) {
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
		data := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x4f, 0x4d, 0xfb, 0xab}
		runAllBufReaderTypes(t, "", data, func(t *testing.T, dec Decbuf) {
			dec.CheckCrc32(table)
			require.NoError(t, dec.Err())
		})
	})

	t.Run("matches checksum (buffer larger than single read)", func(t *testing.T) {
		enc := promencoding.Encbuf{}

		bufferSize := 4*1024*1024 + 1
		for enc.Len() < bufferSize {
			enc.PutByte(0x01)
		}
		enc.PutHash(crc32.New(crc32.MakeTable(crc32.Castagnoli)))

		runAllBufReaderTypes(t, "", enc.Get(), func(t *testing.T, dec Decbuf) {
			dec.CheckCrc32(table)
			require.NoError(t, dec.Err())
		})
	})

	t.Run("does not match checksum (small buffer)", func(t *testing.T) {
		data := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x4f, 0x4d, 0xfb, 0xff}
		runAllBufReaderTypes(t, "", data, func(t *testing.T, dec Decbuf) {
			dec.CheckCrc32(table)
			require.ErrorIs(t, dec.Err(), ErrInvalidChecksum)
		})
	})

	t.Run("does not match checksum (buffer larger than single read)", func(t *testing.T) {
		enc := promencoding.Encbuf{}

		bufferSize := 4*1024*1024 + 1
		for enc.Len() < bufferSize {
			enc.PutByte(0x01)
		}

		data := enc.Get()
		data = append(data, 0x00, 0x01, 0x02, 0x03)

		runAllBufReaderTypes(t, "", data, func(t *testing.T, dec Decbuf) {
			dec.CheckCrc32(table)
			require.ErrorIs(t, dec.Err(), ErrInvalidChecksum)
		})
	})

	t.Run("buffer only contains checksum", func(t *testing.T) {
		data := []byte{0x4f, 0x4d, 0xfb, 0xab}
		runAllBufReaderTypes(t, "", data, func(t *testing.T, dec Decbuf) {
			dec.CheckCrc32(table)
			require.ErrorIs(t, dec.Err(), ErrInvalidSize)
		})
	})

	t.Run("buffer too short for checksum", func(t *testing.T) {
		data := []byte{0x4f, 0x4d, 0xfb}
		runAllBufReaderTypes(t, "", data, func(t *testing.T, dec Decbuf) {
			dec.CheckCrc32(table)
			require.ErrorIs(t, dec.Err(), ErrInvalidSize)
		})
	})
}

// runAllBufReaderTypes creates a disk-backed Decbuf from data and runs testFn against it.
// Named to match Mimir's original helper, which also tested bucket-backed readers.
func runAllBufReaderTypes(t *testing.T, caseName string, data []byte, testFn func(t *testing.T, dec Decbuf)) {
	dir := t.TempDir()
	filePath := path.Join(dir, "test-file")
	require.NoError(t, os.WriteFile(filePath, data, 0700))

	reg := prometheus.NewRegistry()
	factory := NewFilePoolDecbufFactory(filePath, 0, filepool.NewFilePoolMetrics(reg))
	t.Cleanup(func() { _ = factory.Close() })

	name := fmt.Sprintf("BufReader=disk")
	if caseName != "" {
		name = caseName + "/" + name
	}

	t.Run(name, func(t *testing.T) {
		dec := factory.NewRawDecbuf()
		require.NoError(t, dec.Err())
		t.Cleanup(func() { _ = dec.Close() })
		testFn(t, dec)
	})
}

// newBenchDecbuf creates a disk-backed raw Decbuf for use in benchmarks.
func newBenchDecbuf(b *testing.B, data []byte) Decbuf {
	dir := b.TempDir()
	filePath := path.Join(dir, "bench-file")
	require.NoError(b, os.WriteFile(filePath, data, 0700))

	reg := prometheus.NewRegistry()
	factory := NewFilePoolDecbufFactory(filePath, 0, filepool.NewFilePoolMetrics(reg))
	b.Cleanup(func() { _ = factory.Close() })

	dec := factory.NewRawDecbuf()
	require.NoError(b, dec.Err())
	return dec
}
