package lz4block

import (
	"encoding/binary"
	"math/bits"
	"sync"

	"github.com/pierrec/lz4/v4/internal/lz4errors"
)

const (
	// The following constants are used to setup the compression algorithm.
	minMatch   = 4  // the minimum size of the match sequence size (4 bytes)
	winSizeLog = 16 // LZ4 64Kb window size limit
	winSize    = 1 << winSizeLog
	winMask    = winSize - 1 // 64Kb window of previous data for dependent blocks

	// hashLog determines the size of the hash table used to quickly find a previous match position.
	// Its value influences the compression speed and memory usage, the lower the faster,
	// but at the expense of the compression ratio.
	// 16 seems to be the best compromise for fast compression.
	hashLog = 16
	htSize  = 1 << hashLog

	mfLimit = 10 + minMatch // The last match cannot start within the last 14 bytes.
)

// Pool of hash tables for CompressBlock.
var HashTablePool = hashTablePool{sync.Pool{New: func() interface{} { return new([htSize]int) }}}

type hashTablePool struct {
	sync.Pool
}

func (p *hashTablePool) Get() *[htSize]int {
	return p.Pool.Get().(*[htSize]int)
}

// Zero out the table to avoid non-deterministic outputs (see issue#65).
func (p *hashTablePool) Put(t *[htSize]int) {
	*t = [htSize]int{}
	p.Pool.Put(t)
}

func recoverBlock(e *error) {
	if r := recover(); r != nil && *e == nil {
		*e = lz4errors.ErrInvalidSourceShortBuffer
	}
}

// blockHash hashes the lower 6 bytes into a value < htSize.
func blockHash(x uint64) uint32 {
	const prime6bytes = 227718039650203
	return uint32(((x << (64 - 48)) * prime6bytes) >> (64 - hashLog))
}

func CompressBlockBound(n int) int {
	return n + n/255 + 16
}

func UncompressBlock(src, dst []byte) (int, error) {
	if len(src) == 0 {
		return 0, nil
	}
	if di := decodeBlock(dst, src); di >= 0 {
		return di, nil
	}
	return 0, lz4errors.ErrInvalidSourceShortBuffer
}

func CompressBlock(src, dst []byte, hashTable []int) (_ int, err error) {
	defer recoverBlock(&err)

	// Return 0, nil only if the destination buffer size is < CompressBlockBound.
	isNotCompressible := len(dst) < CompressBlockBound(len(src))

	// adaptSkipLog sets how quickly the compressor begins skipping blocks when data is incompressible.
	// This significantly speeds up incompressible data and usually has very small impact on compression.
	// bytes to skip =  1 + (bytes since last match >> adaptSkipLog)
	const adaptSkipLog = 7

	// si: Current position of the search.
	// anchor: Position of the current literals.
	var si, di, anchor int
	sn := len(src) - mfLimit
	if sn <= 0 {
		goto lastLiterals
	}

	if cap(hashTable) < htSize {
		poolTable := HashTablePool.Get()
		defer HashTablePool.Put(poolTable)
		hashTable = poolTable[:]
	} else {
		hashTable = hashTable[:htSize]
	}
	_ = hashTable[htSize-1]

	// Fast scan strategy: the hash table only stores the last 4 bytes sequences.
	for si < sn {
		// Hash the next 6 bytes (sequence)...
		match := binary.LittleEndian.Uint64(src[si:])
		h := blockHash(match)
		h2 := blockHash(match >> 8)

		// We check a match at s, s+1 and s+2 and pick the first one we get.
		// Checking 3 only requires us to load the source one.
		ref := hashTable[h]
		ref2 := hashTable[h2]
		hashTable[h] = si
		hashTable[h2] = si + 1
		offset := si - ref

		// If offset <= 0 we got an old entry in the hash table.
		if offset <= 0 || offset >= winSize || // Out of window.
			uint32(match) != binary.LittleEndian.Uint32(src[ref:]) { // Hash collision on different matches.
			// No match. Start calculating another hash.
			// The processor can usually do this out-of-order.
			h = blockHash(match >> 16)
			ref = hashTable[h]

			// Check the second match at si+1
			si += 1
			offset = si - ref2

			if offset <= 0 || offset >= winSize ||
				uint32(match>>8) != binary.LittleEndian.Uint32(src[ref2:]) {
				// No match. Check the third match at si+2
				si += 1
				offset = si - ref
				hashTable[h] = si

				if offset <= 0 || offset >= winSize ||
					uint32(match>>16) != binary.LittleEndian.Uint32(src[ref:]) {
					// Skip one extra byte (at si+3) before we check 3 matches again.
					si += 2 + (si-anchor)>>adaptSkipLog
					continue
				}
			}
		}

		// Match found.
		lLen := si - anchor // Literal length.
		// We already matched 4 bytes.
		mLen := 4

		// Extend backwards if we can, reducing literals.
		tOff := si - offset - 1
		for lLen > 0 && tOff >= 0 && src[si-1] == src[tOff] {
			si--
			tOff--
			lLen--
			mLen++
		}

		// Add the match length, so we continue search at the end.
		// Use mLen to store the offset base.
		si, mLen = si+mLen, si+minMatch

		// Find the longest match by looking by batches of 8 bytes.
		for si+8 < sn {
			x := binary.LittleEndian.Uint64(src[si:]) ^ binary.LittleEndian.Uint64(src[si-offset:])
			if x == 0 {
				si += 8
			} else {
				// Stop is first non-zero byte.
				si += bits.TrailingZeros64(x) >> 3
				break
			}
		}

		mLen = si - mLen
		if mLen < 0xF {
			dst[di] = byte(mLen)
		} else {
			dst[di] = 0xF
		}

		// Encode literals length.
		if lLen < 0xF {
			dst[di] |= byte(lLen << 4)
		} else {
			dst[di] |= 0xF0
			di++
			l := lLen - 0xF
			for ; l >= 0xFF; l -= 0xFF {
				dst[di] = 0xFF
				di++
			}
			dst[di] = byte(l)
		}
		di++

		// Literals.
		copy(dst[di:di+lLen], src[anchor:anchor+lLen])
		di += lLen + 2
		anchor = si

		// Encode offset.
		_ = dst[di] // Bound check elimination.
		dst[di-2], dst[di-1] = byte(offset), byte(offset>>8)

		// Encode match length part 2.
		if mLen >= 0xF {
			for mLen -= 0xF; mLen >= 0xFF; mLen -= 0xFF {
				dst[di] = 0xFF
				di++
			}
			dst[di] = byte(mLen)
			di++
		}
		// Check if we can load next values.
		if si >= sn {
			break
		}
		// Hash match end-2
		h = blockHash(binary.LittleEndian.Uint64(src[si-2:]))
		hashTable[h] = si - 2
	}

lastLiterals:
	if isNotCompressible && anchor == 0 {
		// Incompressible.
		return 0, nil
	}

	// Last literals.
	lLen := len(src) - anchor
	if lLen < 0xF {
		dst[di] = byte(lLen << 4)
	} else {
		dst[di] = 0xF0
		di++
		for lLen -= 0xF; lLen >= 0xFF; lLen -= 0xFF {
			dst[di] = 0xFF
			di++
		}
		dst[di] = byte(lLen)
	}
	di++

	// Write the last literals.
	if isNotCompressible && di >= anchor {
		// Incompressible.
		return 0, nil
	}
	di += copy(dst[di:di+len(src)-anchor], src[anchor:])
	return di, nil
}

// blockHash hashes 4 bytes into a value < winSize.
func blockHashHC(x uint32) uint32 {
	const hasher uint32 = 2654435761 // Knuth multiplicative hash.
	return x * hasher >> (32 - winSizeLog)
}

func CompressBlockHC(src, dst []byte, depth CompressionLevel, hashTable, chainTable []int) (_ int, err error) {
	defer recoverBlock(&err)

	// Return 0, nil only if the destination buffer size is < CompressBlockBound.
	isNotCompressible := len(dst) < CompressBlockBound(len(src))

	// adaptSkipLog sets how quickly the compressor begins skipping blocks when data is incompressible.
	// This significantly speeds up incompressible data and usually has very small impact on compression.
	// bytes to skip =  1 + (bytes since last match >> adaptSkipLog)
	const adaptSkipLog = 7

	var si, di, anchor int
	sn := len(src) - mfLimit
	if sn <= 0 {
		goto lastLiterals
	}

	// hashTable: stores the last position found for a given hash
	// chainTable: stores previous positions for a given hash
	if cap(hashTable) < htSize {
		poolTable := HashTablePool.Get()
		defer HashTablePool.Put(poolTable)
		hashTable = poolTable[:]
	} else {
		hashTable = hashTable[:htSize]
	}
	_ = hashTable[htSize-1]
	if cap(chainTable) < htSize {
		poolTable := HashTablePool.Get()
		defer HashTablePool.Put(poolTable)
		chainTable = poolTable[:]
	} else {
		chainTable = chainTable[:htSize]
	}
	_ = chainTable[htSize-1]

	if depth == 0 {
		depth = winSize
	}

	for si < sn {
		// Hash the next 4 bytes (sequence).
		match := binary.LittleEndian.Uint32(src[si:])
		h := blockHashHC(match)

		// Follow the chain until out of window and give the longest match.
		mLen := 0
		offset := 0
		for next, try := hashTable[h], depth; try > 0 && next > 0 && si-next < winSize; next, try = chainTable[next&winMask], try-1 {
			// The first (mLen==0) or next byte (mLen>=minMatch) at current match length
			// must match to improve on the match length.
			if src[next+mLen] != src[si+mLen] {
				continue
			}
			ml := 0
			// Compare the current position with a previous with the same hash.
			for ml < sn-si {
				x := binary.LittleEndian.Uint64(src[next+ml:]) ^ binary.LittleEndian.Uint64(src[si+ml:])
				if x == 0 {
					ml += 8
				} else {
					// Stop is first non-zero byte.
					ml += bits.TrailingZeros64(x) >> 3
					break
				}
			}
			if ml < minMatch || ml <= mLen {
				// Match too small (<minMath) or smaller than the current match.
				continue
			}
			// Found a longer match, keep its position and length.
			mLen = ml
			offset = si - next
			// Try another previous position with the same hash.
		}
		chainTable[si&winMask] = hashTable[h]
		hashTable[h] = si

		// No match found.
		if mLen == 0 {
			si += 1 + (si-anchor)>>adaptSkipLog
			continue
		}

		// Match found.
		// Update hash/chain tables with overlapping bytes:
		// si already hashed, add everything from si+1 up to the match length.
		winStart := si + 1
		if ws := si + mLen - winSize; ws > winStart {
			winStart = ws
		}
		for si, ml := winStart, si+mLen; si < ml; {
			match >>= 8
			match |= uint32(src[si+3]) << 24
			h := blockHashHC(match)
			chainTable[si&winMask] = hashTable[h]
			hashTable[h] = si
			si++
		}

		lLen := si - anchor
		si += mLen
		mLen -= minMatch // Match length does not include minMatch.

		if mLen < 0xF {
			dst[di] = byte(mLen)
		} else {
			dst[di] = 0xF
		}

		// Encode literals length.
		if lLen < 0xF {
			dst[di] |= byte(lLen << 4)
		} else {
			dst[di] |= 0xF0
			di++
			l := lLen - 0xF
			for ; l >= 0xFF; l -= 0xFF {
				dst[di] = 0xFF
				di++
			}
			dst[di] = byte(l)
		}
		di++

		// Literals.
		copy(dst[di:di+lLen], src[anchor:anchor+lLen])
		di += lLen
		anchor = si

		// Encode offset.
		di += 2
		dst[di-2], dst[di-1] = byte(offset), byte(offset>>8)

		// Encode match length part 2.
		if mLen >= 0xF {
			for mLen -= 0xF; mLen >= 0xFF; mLen -= 0xFF {
				dst[di] = 0xFF
				di++
			}
			dst[di] = byte(mLen)
			di++
		}
	}

	if isNotCompressible && anchor == 0 {
		// Incompressible.
		return 0, nil
	}

	// Last literals.
lastLiterals:
	lLen := len(src) - anchor
	if lLen < 0xF {
		dst[di] = byte(lLen << 4)
	} else {
		dst[di] = 0xF0
		di++
		lLen -= 0xF
		for ; lLen >= 0xFF; lLen -= 0xFF {
			dst[di] = 0xFF
			di++
		}
		dst[di] = byte(lLen)
	}
	di++

	// Write the last literals.
	if isNotCompressible && di >= anchor {
		// Incompressible.
		return 0, nil
	}
	di += copy(dst[di:di+len(src)-anchor], src[anchor:])
	return di, nil
}
