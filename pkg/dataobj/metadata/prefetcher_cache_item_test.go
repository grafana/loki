package metadata

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"math"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"
)

func TestCacheItem(t *testing.T) {
	var (
		codec            = CacheItemCodec{Zstd: true, MinRatio: 1.5}
		compressible     = bytes.Repeat([]byte("metadata "), 2000)
		incompress       = randomBytes(t, 8*1024)
		maxRawSize       = int64(1 << 20)
		key              = "dataobj-prefetcher/v1/10:objects/ab#core"
		encoder, decoder = newTestZstd(t)
	)

	// codecOf returns the codec byte of a cache item.
	codecOf := func(stored []byte) byte { return stored[1] }

	t.Run("a cache item below the size threshold stays raw", func(t *testing.T) {
		raw := bytes.Repeat([]byte("a"), minCompressBytes-1)

		stored := encodeCacheItem(raw, key, codec, encoder)

		require.Equal(t, codecNone, codecOf(stored))
		got, err := decodeCacheItem(stored, key, maxRawSize, decoder)
		require.NoError(t, err)
		require.Equal(t, raw, got)
	})

	t.Run("a compressible cache item is stored compressed and decodes to the raw bytes", func(t *testing.T) {
		stored := encodeCacheItem(compressible, key, codec, encoder)

		require.Equal(t, codecZstd, codecOf(stored))
		require.Less(t, len(stored), len(compressible)/2)
		got, err := decodeCacheItem(stored, key, maxRawSize, decoder)
		require.NoError(t, err)
		require.Equal(t, compressible, got)
	})

	t.Run("a cache item that does not reach the ratio stays raw", func(t *testing.T) {
		stored := encodeCacheItem(incompress, key, codec, encoder)

		require.Equal(t, codecNone, codecOf(stored))
	})

	t.Run("a cache item stays raw when the codec is off", func(t *testing.T) {
		stored := encodeCacheItem(compressible, key, CacheItemCodec{}, encoder)

		require.Equal(t, codecNone, codecOf(stored))
	})

	t.Run("a cache item for an empty key decodes for an empty key", func(t *testing.T) {
		stored := encodeCacheItem(compressible, "", codec, encoder)

		got, err := decodeCacheItem(stored, "", maxRawSize, decoder)

		require.NoError(t, err)
		require.Equal(t, compressible, got)
	})

	mismatches := map[string]string{
		"a key that differs in one byte":           key[:len(key)-1] + "x",
		"a key that extends the stored key":        key + "x",
		"a key that is a prefix of the stored key": key[:len(key)-1],
		"an empty key": "",
	}
	for name, askedKey := range mismatches {
		t.Run("returns errKeyMismatch for "+name, func(t *testing.T) {
			stored := encodeCacheItem(compressible, key, codec, encoder)

			_, err := decodeCacheItem(stored, askedKey, maxRawSize, decoder)

			require.ErrorIs(t, err, errKeyMismatch)
		})
	}

	t.Run("returns errVersionMismatch for a cache item of another version", func(t *testing.T) {
		stored := encodeCacheItem(compressible, key, codec, encoder)
		stored[0] = cacheItemVersion + 1

		_, err := decodeCacheItem(stored, key, maxRawSize, decoder)

		require.ErrorIs(t, err, errVersionMismatch)
		require.EqualError(t, err, "cache item version does not match: got 2, want 1")
	})

	corrupt := func(mutate func(stored []byte) []byte) []byte {
		return mutate(encodeCacheItem(compressible, key, codec, encoder))
	}
	keyLengthPastTheEnd := binary.LittleEndian.AppendUint32(binary.AppendUvarint([]byte{cacheItemVersion, codecNone}, 0), 0)
	keyLengthPastTheEnd = binary.AppendUvarint(keyLengthPastTheEnd, math.MaxUint64)
	rejects := map[string]struct {
		stored     []byte
		maxRawSize int64
	}{
		"a cache item with an unknown codec": {
			stored:     corrupt(func(s []byte) []byte { s[1] = 99; return s }),
			maxRawSize: maxRawSize,
		},
		"a cache item with a raw size above the limit": {
			stored:     encodeCacheItem(compressible, key, codec, encoder),
			maxRawSize: int64(len(compressible)) - 1,
		},
		"a cache item with a key length past the end": {
			stored:     keyLengthPastTheEnd,
			maxRawSize: maxRawSize,
		},
		"a cache item with a corrupt payload": {
			stored:     corrupt(func(s []byte) []byte { s[len(s)-1] ^= 0xff; return s }),
			maxRawSize: maxRawSize,
		},
		"a cache item that is cut short": {
			stored:     corrupt(func(s []byte) []byte { return s[:len(s)/2] }),
			maxRawSize: maxRawSize,
		},
		"an empty cache item": {
			stored:     nil,
			maxRawSize: maxRawSize,
		},
	}
	for name, tc := range rejects {
		t.Run("rejects "+name, func(t *testing.T) {
			_, err := decodeCacheItem(tc.stored, key, tc.maxRawSize, decoder)

			require.Error(t, err)
			require.NotErrorIs(t, err, errKeyMismatch)
			require.NotErrorIs(t, err, errVersionMismatch)
		})
	}

	t.Run("rejects a raw cache item with a payload byte that differs from the checksum", func(t *testing.T) {
		raw := bytes.Repeat([]byte("a"), 200)
		stored := encodeCacheItem(raw, key, codec, encoder)
		stored[len(stored)-1] ^= 0xff

		_, err := decodeCacheItem(stored, key, maxRawSize, decoder)

		require.EqualError(t, err, "cache item checksum mismatch")
	})

	t.Run("rejects a raw cache item that is one byte shorter than its raw size", func(t *testing.T) {
		raw := bytes.Repeat([]byte("a"), 200)
		stored := encodeCacheItem(raw, key, codec, encoder)

		_, err := decodeCacheItem(stored[:len(stored)-1], key, maxRawSize, decoder)

		require.EqualError(t, err, "cache item has 199 bytes, want 200")
	})
}

func newTestZstd(t *testing.T) (*zstd.Encoder, *zstd.Decoder) {
	t.Helper()

	encoder, err := newZstdEncoder()
	require.NoError(t, err)
	decoder, err := newZstdDecoder()
	require.NoError(t, err)
	return encoder, decoder
}

// randomBytes returns n bytes that no codec compresses.
func randomBytes(t *testing.T, n int) []byte {
	t.Helper()

	out := make([]byte, n)
	_, err := rand.Read(out)
	require.NoError(t, err)
	return out
}
