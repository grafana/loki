package decoder

import (
	"encoding/binary"
	"math/bits"
	"unsafe"

	"github.com/goccy/go-json/internal/errors"
)

const (
	lsb = 0x0101010101010101
	msb = 0x8080808080808080
	// bit5 is the bit which distinguishes '{' from '[' and '}' from ']'.
	bit5 = 0x2020202020202020

	// scanBlockSize is the number of the bytes the masks of a scan cover.
	scanBlockSize = 64
	// shortScanLength is the number of the bytes a scan looks at one by one before it works by blocks.
	shortScanLength = 16
)

// NewInput returns the buffer which the decoders decode: a copy of the input followed by a nul byte,
// which ends every scan.
func NewInput(data []byte) []byte {
	buf := make([]byte, len(data)+1)
	copy(buf, data)
	return buf
}

// load64 returns the eight bytes at pos as a little-endian word, whatever the byte order of the machine:
// the masks made from a word give the position of a byte by the count of the trailing zeros.
// It compiles to a single load on a little-endian machine.
func load64(buf []byte, pos int64) uint64 {
	return binary.LittleEndian.Uint64((*[8]byte)(unsafe.Add((*sliceHeader)(unsafe.Pointer(&buf)).data, pos))[:])
}

// byteMask returns the word whose byte is 0x80 where the byte of w is c, and 0 elsewhere.
func byteMask(w, c uint64) uint64 {
	x := w ^ (c * lsb)
	return ^(((x & ^uint64(msb)) + ^uint64(msb)) | x) & msb
}

// scanMasks are the masks of a block of scanBlockSize bytes: bit i of a mask is set if the byte i
// of the block is the character. The brackets are folded: open is '{' or '[' and closing is '}' or ']'.
type scanMasks struct {
	quote     uint64
	backslash uint64
	open      uint64
	closing   uint64
}

// scanBlockWords computes the masks of the block at p by words, eight bytes at a time.
// It is what the architectures without a SIMD scan use.
func scanBlockWords(p unsafe.Pointer, m *scanMasks) {
	var quote, backslash, open, closing uint64
	for i := 0; i < scanBlockSize/8; i++ {
		w := binary.LittleEndian.Uint64((*[8]byte)(unsafe.Add(p, i*8))[:])
		folded := w &^ bit5
		shift := uint(i * 8)
		quote |= gatherMask(byteMask(w, '"')) << shift
		backslash |= gatherMask(byteMask(w, '\\')) << shift
		open |= gatherMask(byteMask(folded, '[')) << shift
		closing |= gatherMask(byteMask(folded, ']')) << shift
	}
	m.quote, m.backslash, m.open, m.closing = quote, backslash, open, closing
}

// gatherMask turns a mask of 0x80 bytes into the byte whose bit i is the byte i of the mask.
func gatherMask(m uint64) uint64 {
	return ((m >> 7) * 0x0102040810204080) >> 56
}

// compoundScanner finds the end of an object or an array without decoding it: the position after
// the bracket which closes it. It keeps its state between the calls of scan, so that a stream may
// scan what it has read so far and go on after reading more.
//
// The bytes are scanned a block at a time, by their masks ( scanMasks ): the quotes which are not
// escaped toggle whether the bytes are inside a string, which a prefix xor of the quote mask tells
// for every byte at once, and the brackets outside strings count the depth. The bytes are looked at
// one by one only in a block where the value may end or the nesting may get too deep.
type compoundScanner struct {
	// depth is the number of the brackets opened but not closed.
	depth int64
	// maxDepth is the depth beyond which the nesting is too deep.
	maxDepth int64
	inString bool
	// escaped is whether the next byte is escaped by the backslash before it.
	escaped bool
}

// scan scans buf[pos:lim] and returns the position after the bracket which closes the value, or,
// when the value doesn't end before lim, the position it stopped at and false.
// A whole block is read at once where the capacity of the buffer has room for it.
func (sc *compoundScanner) scan(buf []byte, pos, lim int64) (int64, bool, error) {
	const evenBits = 0x5555555555555555
	// A short value ends within a few bytes, which are looked at one by one: that costs less
	// than the masks of a whole block.
	pos, found, err := sc.scanBytes(buf, pos, min(pos+shortScanLength, lim))
	if found || err != nil {
		return pos, found, err
	}
	depth := sc.depth
	var inString, prevEscaped uint64
	if sc.inString {
		inString = ^uint64(0)
	}
	if sc.escaped {
		prevEscaped = 1
	}
	base := (*sliceHeader)(unsafe.Pointer(&buf)).data
	readable := int64(cap(buf))
	for pos < lim {
		if pos+scanBlockSize > readable {
			// The buffer has no room for a block: its last bytes are looked at one by one.
			sc.depth, sc.inString, sc.escaped = depth, inString != 0, prevEscaped != 0
			return sc.scanBytes(buf, pos, lim)
		}
		var m scanMasks
		scanBlock(unsafe.Add(base, pos), &m)
		n := lim - pos
		if n < scanBlockSize {
			valid := uint64(1)<<uint(n) - 1
			m.quote &= valid
			m.backslash &= valid
			m.open &= valid
			m.closing &= valid
		}
		// escaped has the bytes which follow an odd number of backslashes. A backslash which is
		// itself escaped doesn't escape; a run of backslashes may go on in the next block ( the carry ).
		backslash := m.backslash &^ prevEscaped
		followsEscape := backslash<<1 | prevEscaped
		oddStarts := backslash &^ evenBits &^ followsEscape
		evenSequences, carry := bits.Add64(oddStarts, backslash, 0)
		escaped := (evenBits ^ (evenSequences << 1)) & followsEscape
		if n < scanBlockSize {
			// whether the byte at lim, which the next call scans first, is escaped
			prevEscaped = (escaped >> uint(n)) & 1
		} else {
			prevEscaped = carry
		}
		// inside has the bytes which are inside a string after the byte itself is read:
		// the parity of the quotes up to the byte, flipped if the block begins inside a string.
		quote := m.quote &^ escaped
		inside := quote
		inside ^= inside << 1
		inside ^= inside << 2
		inside ^= inside << 4
		inside ^= inside << 8
		inside ^= inside << 16
		inside ^= inside << 32
		inside ^= inString
		inString = uint64(int64(inside) >> 63) // the state after the last byte, in every bit
		// An escaped byte is neither a quote nor a bracket, wherever it is.
		outside := ^(inside | escaped)
		open := m.open & outside
		closing := m.closing & outside
		opens := int64(bits.OnesCount64(open))
		closes := int64(bits.OnesCount64(closing))
		if closes < depth && depth+opens <= sc.maxDepth {
			// the value can't end in this block, nor go too deep
			depth += opens - closes
			pos += scanBlockSize
			continue
		}
		for brackets := open | closing; brackets != 0; brackets &= brackets - 1 {
			i := bits.TrailingZeros64(brackets)
			if open&(1<<uint(i)) != 0 {
				depth++
				if depth > sc.maxDepth {
					return 0, false, errors.ErrExceededMaxDepth(buf[pos+int64(i)], pos+int64(i))
				}
			} else {
				depth--
				if depth == 0 {
					return pos + int64(i) + 1, true, nil
				}
			}
		}
		pos += scanBlockSize
	}
	sc.depth, sc.inString, sc.escaped = depth, inString != 0, prevEscaped != 0
	return lim, false, nil
}

// scanBytes scans buf[pos:end] one byte at a time, as scan does by blocks.
// A byte after a backslash is neither a quote nor a bracket, wherever it is.
func (sc *compoundScanner) scanBytes(buf []byte, pos, end int64) (int64, bool, error) {
	if pos >= end {
		return pos, false, nil
	}
	depth, inString := sc.depth, sc.inString
	if sc.escaped {
		sc.escaped = false
		pos++
	}
	for pos < end {
		if inString {
			for pos < end {
				c := buf[pos]
				pos++
				if c == '"' {
					inString = false
					break
				}
				if c == '\\' {
					pos++
				}
			}
			continue
		}
		c := buf[pos]
		pos++
		switch c {
		case '"':
			inString = true
		case '\\':
			pos++
		case '{', '[':
			depth++
			if depth > sc.maxDepth {
				return 0, false, errors.ErrExceededMaxDepth(c, pos-1)
			}
		case '}', ']':
			depth--
			if depth == 0 {
				return pos, true, nil
			}
		}
	}
	// A backslash as the last byte escapes the byte after end.
	escaped := pos > end
	if escaped {
		pos = end
	}
	sc.depth, sc.inString, sc.escaped = depth, inString, escaped
	return pos, false, nil
}
