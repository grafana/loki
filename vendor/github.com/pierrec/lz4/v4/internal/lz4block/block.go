package lz4block

import (
	"encoding/binary"
	"math"
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

func UncompressBlock(src, dst, dict []byte) (int, error) {
	if len(src) == 0 {
		return 0, nil
	}
	if di := decodeBlock(dst, src, dict); di >= 0 {
		return di, nil
	}
	return 0, lz4errors.ErrInvalidSourceShortBuffer
}

type Compressor struct {
	// Offsets are at most 64kiB, so we can store only the lower 16 bits of
	// match positions: effectively, an offset from some 64kiB block boundary.
	//
	// When we retrieve such an offset, we interpret it as relative to the last
	// block boundary si &^ 0xffff, or the one before, (si &^ 0xffff) - 0x10000,
	// depending on which of these is inside the current window. If a table
	// entry was generated more than 64kiB back in the input, we find out by
	// inspecting the input stream.
	table [htSize]uint16

	// Bitmap indicating which positions in the table are in use.
	// This allows us to quickly reset the table for reuse,
	// without having to zero everything.
	inUse [htSize / 32]uint32
}

// Get returns the position of a presumptive match for the hash h.
// The match may be a false positive due to a hash collision or an old entry.
// If si < winSize, the return value may be negative.
func (c *Compressor) get(h uint32, si int) int {
	h &= htSize - 1
	i := 0
	if c.inUse[h/32]&(1<<(h%32)) != 0 {
		i = int(c.table[h])
	}
	i += si &^ winMask
	if i >= si {
		// Try previous 64kiB block (negative when in first block).
		i -= winSize
	}
	return i
}

func (c *Compressor) put(h uint32, si int) {
	h &= htSize - 1
	c.table[h] = uint16(si)
	c.inUse[h/32] |= 1 << (h % 32)
}

func (c *Compressor) reset() { c.inUse = [htSize / 32]uint32{} }

var compressorPool = sync.Pool{New: func() interface{} { return new(Compressor) }}

func CompressBlock(src, dst []byte) (int, error) {
	c := compressorPool.Get().(*Compressor)
	n, err := c.CompressBlock(src, dst)
	compressorPool.Put(c)
	return n, err
}

func (c *Compressor) CompressBlock(src, dst []byte) (int, error) {
	// Zero out reused table to avoid non-deterministic output (issue #65).
	c.reset()

	// Return 0, nil only if the destination buffer size is < CompressBlockBound.
	isNotCompressible := len(dst) < CompressBlockBound(len(src))

	// adaptSkipLog sets how quickly the compressor begins skipping blocks when data is incompressible.
	// This significantly speeds up incompressible data and usually has very small impact on compression.
	// bytes to skip =  1 + (bytes since last match >> adaptSkipLog)
	const adaptSkipLog = 7

	// si: Current position of the search.
	// anchor: Position of the current literals.
	var si, di, anchor int
	var r srcReader
	sn := len(src) - mfLimit
	if sn <= 0 {
		goto lastLiterals
	}
	// All loads are within src: positions searched stop mfLimit bytes before
	// its end, and candidates are earlier positions, as the table only holds
	// positions stored by this call (unused entries read as zero).
	r = newSrcReader(src)

	// Fast scan strategy: the hash table only stores the last 4 bytes sequences.
	for si < sn {
		// Hash the next 6 bytes (sequence)...
		match := r.load64(src, si)
		h := blockHash(match)
		h2 := blockHash(match >> 8)

		// We check a match at s, s+1 and s+2 and pick the first one we get.
		// Checking 3 only requires us to load the source one.
		ref := c.get(h, si)
		ref2 := c.get(h2, si+1)
		c.put(h, si)
		c.put(h2, si+1)

		offset := si - ref

		if offset <= 0 || offset >= winSize || uint32(match) != r.load32(src, ref) {
			// No match. Start calculating another hash.
			// The processor can usually do this out-of-order.
			h = blockHash(match >> 16)
			ref3 := c.get(h, si+2)

			// Check the second match at si+1
			si += 1
			offset = si - ref2

			if offset <= 0 || offset >= winSize || uint32(match>>8) != r.load32(src, ref2) {
				// No match. Check the third match at si+2
				si += 1
				offset = si - ref3
				c.put(h, si)

				if offset <= 0 || offset >= winSize || uint32(match>>16) != r.load32(src, ref3) {
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
		for si+8 <= sn {
			x := r.load64(src, si) ^ r.load64(src, si-offset)
			if x == 0 {
				si += 8
			} else {
				// Stop is first non-zero byte.
				si += bits.TrailingZeros64(x) >> 3
				break
			}
		}

		mLen = si - mLen
		if di >= len(dst) {
			return 0, lz4errors.ErrInvalidSourceShortBuffer
		}
		// Token: literal length in the high nibble, match length in the low.
		tok := byte(0xF)
		if mLen < 0xF {
			tok = byte(mLen)
		}

		// Encode literals length.
		if lLen < 0xF {
			dst[di] = tok | byte(lLen<<4)
		} else {
			dst[di] = tok | 0xF0
			di++
			l := lLen - 0xF
			for ; l >= 0xFF && di < len(dst); l -= 0xFF {
				dst[di] = 0xFF
				di++
			}
			if di >= len(dst) {
				return 0, lz4errors.ErrInvalidSourceShortBuffer
			}
			dst[di] = byte(l)
		}
		di++

		// Literals.
		if di+lLen > len(dst) {
			return 0, lz4errors.ErrInvalidSourceShortBuffer
		}
		copy(dst[di:di+lLen], src[anchor:anchor+lLen])
		di += lLen + 2
		anchor = si

		// Encode offset.
		if di > len(dst) {
			return 0, lz4errors.ErrInvalidSourceShortBuffer
		}
		dst[di-2], dst[di-1] = byte(offset), byte(offset>>8)

		// Encode match length part 2.
		if mLen >= 0xF {
			for mLen -= 0xF; mLen >= 0xFF && di < len(dst); mLen -= 0xFF {
				dst[di] = 0xFF
				di++
			}
			if di >= len(dst) {
				return 0, lz4errors.ErrInvalidSourceShortBuffer
			}
			dst[di] = byte(mLen)
			di++
		}
		// Check if we can load next values.
		if si >= sn {
			break
		}
		// Hash match end-2
		h = blockHash(r.load64(src, si-2))
		c.put(h, si-2)
	}

lastLiterals:
	if isNotCompressible && anchor == 0 {
		// Incompressible.
		return 0, nil
	}

	// Last literals.
	if di >= len(dst) {
		return 0, lz4errors.ErrInvalidSourceShortBuffer
	}
	lLen := len(src) - anchor
	if lLen < 0xF {
		dst[di] = byte(lLen << 4)
	} else {
		dst[di] = 0xF0
		di++
		for lLen -= 0xF; lLen >= 0xFF && di < len(dst); lLen -= 0xFF {
			dst[di] = 0xFF
			di++
		}
		if di >= len(dst) {
			return 0, lz4errors.ErrInvalidSourceShortBuffer
		}
		dst[di] = byte(lLen)
	}
	di++

	// Write the last literals.
	if isNotCompressible && di >= anchor {
		// Incompressible.
		return 0, nil
	}
	if di+len(src)-anchor > len(dst) {
		return 0, lz4errors.ErrInvalidSourceShortBuffer
	}
	di += copy(dst[di:di+len(src)-anchor], src[anchor:])
	return di, nil
}

// blockHash hashes 4 bytes into a value < winSize.
func blockHashHC(x uint32) uint32 {
	const hasher uint32 = 2654435761 // Knuth multiplicative hash.
	return x * hasher >> (32 - winSizeLog)
}

// CompressorHC holds the match finder state for the high compression mode.
//
// Positions are stored in the narrowest type that can hold them: 16 bits for
// inputs of up to winSize bytes, 32 bits otherwise. Keeping the tables small
// matters because following the hash chain is dominated by cache misses.
type CompressorHC struct {
	small hcTables[uint16]
	large *hcTables[int32] // Allocated on first use.
	// Whether the tables have been used and need to be reset before reuse.
	smallDirty, largeDirty bool
}

type hcPosition interface {
	uint16 | int32
}

type hcTables[T hcPosition] struct {
	// hashTable: stores the last position found for a given hash, or 0.
	hashTable [htSize]T
	// chainTable: stores, for each position in the window, the previous
	// position with the same hash. An entry is always written when its
	// position is inserted, before it can be read, so it never needs to be
	// reset.
	chainTable [winSize]T
}

// findRunMatch returns the same match as the chain walk in compressBlockHC,
// for when src[si:] starts with a run of k >= minRunHC bytes equal to b, with
// k capped at maxLen. On such data the chain holds every position of every
// earlier run of b, and comparing each of them dominates the search.
//
// Let p < si be a position that starts a run of r bytes equal to b. Its match
// with si is exactly min(r, k) bytes long unless r == k:
//   - if r < k, then src[p+r] != b but src[si+r] == b, so the match is r
//     bytes long (r < k <= maxLen, so matchLength reports it exactly);
//   - if r > k, then src[p+k] == b but src[si+k] != b, or k == maxLen and
//     matchLength reports no more than maxLen, so the match is k bytes long;
//   - if r == k, the match is at least k bytes long, and may be longer.
//
// The plain walk replaces the current match only with a strictly longer one,
// so of the positions it visits it keeps the first of the longest, and it
// stops early once the match reaches maxLen. Its one-byte filter only skips
// positions that cannot be kept, so it does not change the result.
//
// When the chain goes from a position cand in a run of b to cand-1, and
// src[cand-1] == b, then cand-1 is in the same run, with a run one byte
// longer. The loop below follows such steps without comparing anything,
// spending one try per position exactly as the plain walk does. If cand has
// a run of rc bytes, the positions cand, cand-1, ..., last have runs of rc,
// rc+1, ..., rc+cand-last bytes, and the case analysis picks the position
// and length the plain walk would have kept among them.
func (t *hcTables[T]) findRunMatch(src []byte, si, sn, maxLen int, depth CompressionLevel, h uint32, b byte, k int) (mLen, offset int) {
	for next, try := int(t.hashTable[h]), depth; try > 0 && next > 0 && si-next < winSize; try-- {
		cand := next
		next = int(t.chainTable[next&winMask])
		var ml int
		if next == cand-1 && src[cand] == b && src[next] == b {
			// rc is the run at cand, counted up to k+1 bytes, which is enough
			// to tell r < k, r == k and r > k apart. A run that reaches si
			// continues with the run at si, so it is longer than k.
			rc := k + 1
			if d := si - cand; d > k {
				rc = runLength(src, cand, b, k+1)
			} else if r := runLength(src, cand, b, d); r < d {
				rc = r
			}
			last := cand
			for try > 1 && next > 0 && si-next < winSize && next == last-1 && src[next] == b {
				last = next
				next = int(t.chainTable[next&winMask])
				try--
			}
			switch {
			case rc > k:
				// Every position has r > k and matches exactly k bytes, so
				// the plain walk keeps the first one, cand.
				ml = k
			case k-rc <= cand-last:
				// The walk reached the position with r == k. The positions
				// before it match rc to k-1 bytes and those after it match
				// k, while it matches at least k, so the plain walk keeps
				// it. Positions after it cannot change the result, as they
				// are not longer; and if it reaches maxLen, the plain walk
				// stops there, as this one does below.
				cand -= k - rc
				ml = matchLength(src, cand, si, sn)
			default:
				// Every position has r < k, so they match rc, rc+1, ...,
				// rc+cand-last bytes and the plain walk keeps the last one.
				ml = rc + cand - last
				cand = last
			}
		} else {
			if src[cand+mLen] != src[si+mLen] {
				continue
			}
			ml = matchLength(src, cand, si, sn)
		}
		if ml < minMatch || ml <= mLen {
			continue
		}
		mLen = ml
		offset = si - cand
		if mLen >= maxLen {
			break
		}
	}
	return
}

// minRunHC is the shortest run length for which runs are handled in bulk.
const minRunHC = 16

// matchLength returns the length of the match between src[cand:] and
// src[si:], comparing 8 bytes at a time while fewer than sn-si bytes match.
func matchLength(src []byte, cand, si, sn int) int {
	ml := 0
	for ml < sn-si {
		x := binary.LittleEndian.Uint64(src[cand+ml:]) ^ binary.LittleEndian.Uint64(src[si+ml:])
		if x != 0 {
			// Stop is first non-zero byte.
			return ml + bits.TrailingZeros64(x)>>3
		}
		ml += 8
	}
	return ml
}

// runLength returns the number of consecutive bytes equal to b at src[i:],
// up to max. It reads src[i : i+max+7].
func runLength(src []byte, i int, b byte, max int) int {
	pattern := uint64(b) * 0x0101010101010101
	for n := 0; n < max; n += 8 {
		if x := binary.LittleEndian.Uint64(src[i+n:]) ^ pattern; x != 0 {
			n += bits.TrailingZeros64(x) >> 3
			if n > max {
				return max
			}
			return n
		}
	}
	return max
}

var compressorHCPool = sync.Pool{New: func() interface{} { return new(CompressorHC) }}

func CompressBlockHC(src, dst []byte, depth CompressionLevel) (int, error) {
	c := compressorHCPool.Get().(*CompressorHC)
	n, err := c.CompressBlock(src, dst, depth)
	compressorHCPool.Put(c)
	return n, err
}

func (c *CompressorHC) CompressBlock(src, dst []byte, depth CompressionLevel) (int, error) {
	// Zero out reused tables to avoid non-deterministic output (issue #65).
	if len(src) <= winSize {
		if c.smallDirty {
			c.small.hashTable = [htSize]uint16{}
		}
		c.smallDirty = true
		return compressBlockHC(&c.small, src, dst, depth)
	}
	if int64(len(src)) > math.MaxInt32 {
		// Positions are stored as int32.
		return 0, lz4errors.ErrInvalidSourceShortBuffer
	}
	if c.large == nil {
		c.large = new(hcTables[int32])
	} else if c.largeDirty {
		c.large.hashTable = [htSize]int32{}
	}
	c.largeDirty = true
	return compressBlockHC(c.large, src, dst, depth)
}

func compressBlockHC[T hcPosition](t *hcTables[T], src, dst []byte, depth CompressionLevel) (_ int, err error) {
	defer recoverBlock(&err)
	// Short destinations are caught by bounds checks, recovered above. Cap dst
	// so that copies into it cannot reslice past len into the caller's memory.
	dst = dst[:len(dst):len(dst)]

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

	if depth == 0 {
		depth = winSize
	}

	for si < sn {
		// Hash the next 4 bytes (sequence).
		match := binary.LittleEndian.Uint32(src[si:])
		h := blockHashHC(match)

		// The comparison loop below advances 8 bytes at a time while ml < sn-si,
		// so no match can be longer than sn-si rounded up to a multiple of 8.
		maxLen := (sn - si + 7) &^ 7

		// Follow the chain until out of window and give the longest match.
		// k is the length of the run of one byte at si, if measured.
		var mLen, offset, k int
		if b := byte(match); match == uint32(b)*0x01010101 && t.hashTable[h] > 0 && si-int(t.hashTable[h]) < winSize {
			// There is at least one candidate: measure the run.
			if k = runLength(src, si, b, maxLen); k >= minRunHC {
				mLen, offset = t.findRunMatch(src, si, sn, maxLen, depth, h, b, k)
				goto found
			}
		}
		for next, try := int(t.hashTable[h]), depth; try > 0 && next > 0 && si-next < winSize; try-- {
			// Load the following position first: it does not depend on the
			// comparison, and the walk is bound by these dependent loads.
			cand := next
			next = int(t.chainTable[next&winMask])
			// The first (mLen==0) or next byte (mLen>=minMatch) at current match length
			// must match to improve on the match length.
			if src[cand+mLen] != src[si+mLen] {
				continue
			}
			// Compare the current position with a previous with the same hash.
			ml := matchLength(src, cand, si, sn)
			if ml < minMatch || ml <= mLen {
				// Match too small (<minMath) or smaller than the current match.
				continue
			}
			// Found a longer match, keep its position and length.
			mLen = ml
			offset = si - cand
			if mLen >= maxLen {
				// No other candidate can be longer.
				break
			}
			// Try another previous position with the same hash.
		}
	found:
		t.chainTable[si&winMask] = t.hashTable[h]
		t.hashTable[h] = T(si)

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
		if k > minMatch && winStart == si+1 {
			// The positions si+1 to si+k-4 start with the same 4 bytes as si,
			// so inserting them one at a time, as the loop below does, would
			// chain each to its predecessor and leave the last one in the
			// hash table. The loop rolls its 4-byte hash input forward from
			// si, so this is only done when it starts at si+1.
			n := k - minMatch
			if n > mLen-1 {
				n = mLen - 1
			}
			for j := si + 1; j <= si+n; j++ {
				t.chainTable[j&winMask] = T(j - 1)
			}
			t.hashTable[h] = T(si + n)
			winStart += n
		}
		for si, ml := winStart, si+mLen; si < ml; {
			match >>= 8
			match |= uint32(src[si+3]) << 24
			h := blockHashHC(match)
			t.chainTable[si&winMask] = t.hashTable[h]
			t.hashTable[h] = T(si)
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
