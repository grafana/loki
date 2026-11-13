package metadata

// The prefetcher stores one cache item under each cache key. A cache item wraps a raw payload in the
// envelope below. The envelope does not look at the payload.
//
// A uvarint is encoded like binary.PutUvarint. A fixed size integer is little endian.
//
// Cache item:
//
//	┌──────────────┬────────────┬───────────────────┐
//	│ version <1b> │ codec <1b> │ rawSize <uvarint> │
//	├──────────────┴────────────┴───────────────────┤
//	│ crc32(raw) <4b>                               │
//	├───────────────────────┬───────────────────────┤
//	│ len(key) <uvarint>    │ key <bytes>           │
//	├───────────────────────┴───────────────────────┤
//	│ payload <bytes>                               │
//	└───────────────────────────────────────────────┘
//
//   - version is cacheItemVersion. A reader counts a cache item of another version as a miss.
//   - codec is 0 when the payload is the raw bytes. It is 1 when the payload is one zstd frame of
//     the raw bytes. The frame records its content size, and that size must equal rawSize.
//   - rawSize is the number of raw bytes.
//   - crc32 is the IEEE CRC32 of the raw bytes, not of the payload.
//   - key is the unhashed cache key of the item. The cache stores the item under a hash of the key,
//     and two keys can have the same hash. A reader counts a cache item whose key differs from the key
//     it asked for as a miss.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"

	"github.com/klauspost/compress/zstd"
)

const (
	// cacheItemVersion changes when the layout of a cache item or of prefetched ranges changes. A
	// reader counts a cache item of another version as a miss.
	cacheItemVersion = 1

	codecNone byte = 0
	codecZstd byte = 1

	// versionAndCodecBytes is the size of the version and the codec at the start of a cache item.
	versionAndCodecBytes = 2

	// checksumBytes is the size of the CRC32 of the raw bytes in a cache item.
	checksumBytes = 4

	// minCompressBytes is the smallest raw size that a cache item compresses.
	minCompressBytes = 4096

	// maxZstdDecodedBytes caps the decoded size of one zstd cache item.
	maxZstdDecodedBytes = 512 << 20

	// cacheItemOverheadBytes is the estimated maximum of what a cache item adds to the raw bytes of
	// the ranges of a group: the range headers, the column names and the cache item envelope. The
	// prefetcher adds it to the max region size, and rejects a config where the sum is above the
	// largest cache item of the cache.
	cacheItemOverheadBytes = 64 * 1024
)

// CacheItemCodec sets how the prefetcher encodes a cache item.
type CacheItemCodec struct {
	// Zstd makes the encoder compress the raw bytes of a cache item with zstd.
	Zstd bool

	// MinRatio is the smallest ratio of the raw size to the compressed size at which the encoder keeps
	// the compressed bytes. Below it, the cache item holds the raw bytes.
	MinRatio float64
}

// newZstdEncoder returns the encoder that compresses the cache items. It is safe for concurrent use.
// EncodeAll starts no goroutine, so the encoder needs no Close.
func newZstdEncoder() (*zstd.Encoder, error) {
	return zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedFastest))
}

// newZstdDecoder returns the decoder that decompresses the cache items. It is safe for concurrent
// use. DecodeAll starts no goroutine, so the decoder needs no Close.
func newZstdDecoder() (*zstd.Decoder, error) {
	return zstd.NewReader(nil, zstd.WithDecoderConcurrency(0), zstd.WithDecoderMaxMemory(maxZstdDecodedBytes))
}

// maxCachedBytes returns the largest raw size of a cache item, before compression, for regions of up
// to maxRegionBytes.
func maxCachedBytes(maxRegionBytes int64) int64 {
	return maxRegionBytes + cacheItemOverheadBytes
}

var (
	// errKeyMismatch means a cache item holds another key than the key that the reader asked for. It
	// happens when two keys have the same hash.
	errKeyMismatch = errors.New("cache item key does not match")

	// errVersionMismatch means a cache item has another version than cacheItemVersion. It happens
	// while a rollout changes the version, because the old and the new processes share the cache.
	errVersionMismatch = errors.New("cache item version does not match")
)

// encodeCacheItem wraps raw in a cache item for the unhashed cache key. It compresses raw with
// encoder when the codec asks for it, raw is at least minCompressBytes long, and the ratio reaches
// the minimum.
func encodeCacheItem(raw []byte, key string, codec CacheItemCodec, encoder *zstd.Encoder) []byte {
	payload, codecType := raw, codecNone
	if codec.Zstd && len(raw) >= minCompressBytes {
		if compressed := encoder.EncodeAll(raw, nil); float64(len(raw)) >= codec.MinRatio*float64(len(compressed)) {
			payload, codecType = compressed, codecZstd
		}
	}

	// The raw size and the key length are uvarints. Count each at its longest, so the buffer never grows.
	maxEnvelopeBytes := versionAndCodecBytes + binary.MaxVarintLen64 + checksumBytes + binary.MaxVarintLen64
	out := make([]byte, 0, maxEnvelopeBytes+len(key)+len(payload))
	out = append(out, cacheItemVersion, codecType)
	out = binary.AppendUvarint(out, uint64(len(raw)))
	out = binary.LittleEndian.AppendUint32(out, crc32.ChecksumIEEE(raw))
	out = binary.AppendUvarint(out, uint64(len(key)))
	out = append(out, key...)
	return append(out, payload...)
}

// decodeCacheItem returns the raw bytes of a cache item. It decompresses the payload with decoder.
// The result aliases stored when the cache item is not compressed.
//
// It returns errKeyMismatch when the key in the cache item is not expectedKey, the unhashed cache key
// that the caller asked for. It returns errVersionMismatch when the version of the cache item is not
// cacheItemVersion. It returns another error when the cache item is malformed or corrupt, or when
// its raw size is above maxRawSize.
func decodeCacheItem(stored []byte, expectedKey string, maxRawSize int64, decoder *zstd.Decoder) ([]byte, error) {
	if len(stored) < versionAndCodecBytes {
		return nil, errors.New("cache item is truncated")
	}
	if stored[0] != cacheItemVersion {
		return nil, fmt.Errorf("%w: got %d, want %d", errVersionMismatch, stored[0], cacheItemVersion)
	}
	codecType := stored[1]

	rawSize, n := binary.Uvarint(stored[versionAndCodecBytes:])
	if n <= 0 || rawSize > uint64(maxRawSize) {
		return nil, errors.New("invalid cache item size")
	}
	rest := stored[versionAndCodecBytes+n:]
	if len(rest) < checksumBytes {
		return nil, errors.New("cache item is truncated")
	}
	wantCRC, rest := binary.LittleEndian.Uint32(rest), rest[checksumBytes:]

	keyLength, n := binary.Uvarint(rest)
	if n <= 0 || keyLength > uint64(len(rest)-n) {
		return nil, errors.New("cache item is truncated")
	}
	rest = rest[n:]
	if string(rest[:keyLength]) != expectedKey {
		return nil, errKeyMismatch
	}
	payload := rest[keyLength:]

	var raw []byte
	switch codecType {
	case codecNone:
		raw = payload
	case codecZstd:
		// The encoder always writes the decoded size in the frame. Reject a frame that omits it or names
		// another size. This stops a crafted frame from forcing a large allocation.
		var header zstd.Header
		if err := header.Decode(payload); err != nil {
			return nil, fmt.Errorf("reading the zstd frame: %w", err)
		}
		if !header.HasFCS || header.FrameContentSize != rawSize {
			return nil, errors.New("the zstd frame does not match the cache item size")
		}

		var err error
		if raw, err = decoder.DecodeAll(payload, make([]byte, 0, rawSize)); err != nil {
			return nil, fmt.Errorf("decompressing cache item: %w", err)
		}
	default:
		return nil, fmt.Errorf("unknown cache item codec %d", codecType)
	}

	if uint64(len(raw)) != rawSize {
		return nil, fmt.Errorf("cache item has %d bytes, want %d", len(raw), rawSize)
	}
	if crc32.ChecksumIEEE(raw) != wantCRC {
		return nil, errors.New("cache item checksum mismatch")
	}
	return raw, nil
}
