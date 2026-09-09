package jsonlite

import (
	"errors"
	"math/bits"
	"sync"
	"unsafe"

	"github.com/parquet-go/bitpack/unsafecast"
)

// This file implements a simdjson-style "stage 1" structural indexer
// (Langdale & Lemire, https://arxiv.org/abs/1902.08318).
//
// The input is scanned in 64-byte blocks. For each block we build bitmasks
// classifying every byte (backslash, quote, control, whitespace, structural),
// then use carry-propagating bit arithmetic to resolve escape sequences and
// string boundaries, and finally emit the byte offsets of:
//
//   - structural characters: { } [ ] , :  (outside strings)
//   - unescaped quotes (both opening and closing)
//   - the first byte of every primitive (numbers, true/false/null)
//
// String contents emit nothing, so for a string token the offset following
// the opening quote is always its closing quote. Stage 1 also validates that
// strings are terminated and contain no unescaped control characters.

var (
	errUnterminatedString = errors.New("unterminated string")
	errControlCharacter   = errors.New("unescaped control character in string")
)

const oddBits = uint64(0xAAAAAAAAAAAAAAAA)

// blockMasks classifies each of the 64 bytes of one input block.
type blockMasks struct {
	bs         uint64 // backslash
	quote      uint64 // '"'
	ctrl       uint64 // < 0x20
	ws         uint64 // space, \t, \n, \r
	structural uint64 // { } [ ] , :
	hi         uint64 // >= 0x80 (non-ASCII)
}

// stage1Flags reports document-level facts learned during stage 1.
type stage1Flags uint8

const (
	// flagBackslash: the document contains at least one backslash, so string
	// escape sequences need validation.
	flagBackslash stage1Flags = 1 << iota
	// flagNonASCII: the document contains bytes >= 0x80.
	flagNonASCII
)

const (
	classWS byte = 1 << iota
	classStructural
	classQuote
	classBackslash
	classCtrl
	classHigh
)

var byteClass = func() (t [256]byte) {
	for _, c := range []byte{' ', '\t', '\n', '\r'} {
		t[c] |= classWS
	}
	for _, c := range []byte{'{', '}', '[', ']', ',', ':'} {
		t[c] |= classStructural
	}
	t['"'] |= classQuote
	t['\\'] |= classBackslash
	for c := range 0x20 {
		t[c] |= classCtrl
	}
	for c := 0x80; c < 0x100; c++ {
		t[c] |= classHigh
	}
	return
}()

func classifyBlockPortable(b *[64]byte) (m blockMasks) {
	for i, c := range b {
		bit := uint64(1) << i
		switch cl := byteClass[c]; {
		case cl == 0:
		case cl&classWS != 0:
			m.ws |= bit
			if cl&classCtrl != 0 {
				m.ctrl |= bit
			}
		case cl&classStructural != 0:
			m.structural |= bit
		case cl&classQuote != 0:
			m.quote |= bit
		case cl&classBackslash != 0:
			m.bs |= bit
		case cl&classHigh != 0:
			m.hi |= bit
		default:
			m.ctrl |= bit
		}
	}
	return m
}

// stage1State carries information across 64-byte blocks.
type stage1State struct {
	nextIsEscaped uint64 // 1 if the first char of the next block is escaped
	prevInString  uint64 // 0 or ^0: whether the next block starts inside a string
	prevSep       uint64 // 1 if the last byte of the previous block ends a token
	sawBackslash  bool   // whether any backslash was seen in the document
	sawNonASCII   bool   // whether any byte >= 0x80 was seen in the document
	err           error
}

// findEscaped returns the mask of escaped characters (characters preceded by
// an unescaped backslash), handling runs of consecutive backslashes and
// carries across blocks. This is the escape scanner from simdjson.
func (st *stage1State) findEscaped(backslash uint64) uint64 {
	if backslash == 0 {
		escaped := st.nextIsEscaped
		st.nextIsEscaped = 0
		return escaped
	}
	st.sawBackslash = true
	potentialEscape := backslash &^ st.nextIsEscaped
	maybeEscaped := potentialEscape << 1
	maybeEscapedAndOddBits := maybeEscaped | oddBits
	evenSeriesCodesAndOddBits := maybeEscapedAndOddBits - potentialEscape
	escapeAndTerminalCode := evenSeriesCodesAndOddBits ^ oddBits
	escaped := escapeAndTerminalCode ^ (backslash | st.nextIsEscaped)
	escape := escapeAndTerminalCode & backslash
	st.nextIsEscaped = escape >> 63
	return escaped
}

// prefixXor computes the running XOR of all bits at or below each position:
// bit i of the result is the XOR of bits 0..i of x. Equivalent to a carry-less
// multiply by ^0.
func prefixXor(x uint64) uint64 {
	x ^= x << 1
	x ^= x << 2
	x ^= x << 4
	x ^= x << 8
	x ^= x << 16
	x ^= x << 32
	return x
}

// crunch resolves one block's masks into emitted index positions.
func (st *stage1State) crunch(m blockMasks, base int, index []uint32) []uint32 {
	if m.hi != 0 {
		st.sawNonASCII = true
	}
	escaped := st.findEscaped(m.bs)
	quotes := m.quote &^ escaped

	inString := prefixXor(quotes) ^ st.prevInString
	st.prevInString = uint64(int64(inString) >> 63)

	// Control characters must not appear raw inside strings. (The opening
	// quote is part of inString but is never a control character.)
	if m.ctrl&inString != 0 && st.err == nil {
		st.err = errControlCharacter
	}

	structural := m.structural &^ inString
	ws := m.ws &^ inString

	// Primitive starts: bytes outside strings that are not separators and
	// follow a separator (whitespace, structural, quote, or start of input).
	sep := structural | ws | quotes | (inString &^ quotes)
	scalar := ^(structural | ws | quotes | inString)
	starts := scalar & (sep<<1 | st.prevSep)
	st.prevSep = sep >> 63

	emit := structural | quotes | starts
	for emit != 0 {
		index = append(index, uint32(base+bits.TrailingZeros64(emit)))
		emit &= emit - 1
	}
	return index
}

func (st *stage1State) flags() stage1Flags {
	var f stage1Flags
	if st.sawBackslash {
		f |= flagBackslash
	}
	if st.sawNonASCII {
		f |= flagNonASCII
	}
	return f
}

// structuralIndexPortable is the scalar structural indexer; structuralIndex
// (defined per build) dispatches to it or to the vectorized implementation.
func structuralIndexPortable(s string, index []uint32) ([]uint32, stage1Flags, error) {
	var st stage1State
	st.prevSep = 1

	buf := unsafe.Slice(unsafe.StringData(s), len(s))
	blocks := unsafecast.Slice[[64]byte](buf)
	for bi := range blocks {
		index = st.crunch(classifyBlockPortable(&blocks[bi]), bi*64, index)
	}
	i := len(blocks) * 64
	if i < len(s) {
		var b [64]byte
		for j := range b {
			b[j] = ' '
		}
		copy(b[:], s[i:])
		index = st.crunch(classifyBlockPortable(&b), i, index)
	}
	if st.err != nil {
		return index, st.flags(), st.err
	}
	if st.prevInString != 0 {
		return index, st.flags(), errUnterminatedString
	}
	return index, st.flags(), nil
}

var indexPool = sync.Pool{
	New: func() any {
		s := make([]uint32, 0, 512)
		return &s
	},
}
