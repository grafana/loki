package v1

import (
	"encoding/binary"
	"unicode/utf8"

	"github.com/grafana/loki/pkg/logproto"
)

type Token struct {
	Key []byte
}

type Tokenizer interface {
	Tokens(line string) []Token
	GetSkip() int
	GetMin() int
	GetMax() int
}

const TokenBufferSize = 4096
const TokenKeySize = 132

type NgramTokenizer struct {
	// [min,max) exclusivity
	min, max, skip      int
	buffers             [][]rune // circular buffers used for ngram generation
	runeBuffer          []byte   // buffer used for token generation
	internalTokenBuffer []Token  // circular buffer for tokens
}

/*
N-Grams (https://en.wikipedia.org/wiki/N-gram) are a series of 'n' adjacent characters in a string.
These will be utilized for the bloom filters to allow for fuzzy searching.
*/
func NewNGramTokenizer(min, max, skip int) *NgramTokenizer {
	capacity := max - min
	t := &NgramTokenizer{
		min:                 min,
		max:                 max,
		skip:                skip,
		buffers:             make([][]rune, capacity),
		runeBuffer:          make([]byte, 0, max*4),
		internalTokenBuffer: make([]Token, 0, TokenBufferSize),
	}

	for i := range t.buffers {
		t.buffers[i] = make([]rune, t.min+i)
	}

	for i := 0; i < cap(t.internalTokenBuffer); i++ {
		t.internalTokenBuffer = append(t.internalTokenBuffer, Token{Key: make([]byte, 0, TokenKeySize)})
	}

	return t
}

func (t *NgramTokenizer) GetSkip() int {
	return t.skip
}

func (t *NgramTokenizer) GetMin() int {
	return t.min
}

func (t *NgramTokenizer) GetMax() int {
	return t.max
}

func (t *NgramTokenizer) Tokens(line string) []Token {
	var i int // rune index (not position that is measured in the range loop)
	numToks := 0
	for _, r := range line {

		// j is the index of the buffer to use
		for j := 0; j < (t.max - t.min); j++ {
			// n is the length of the ngram
			n := j + t.min
			// pos is the position in the buffer to overwrite
			pos := i % n
			t.buffers[j][pos] = r

			if i >= n-1 && (i+1-n)%(t.skip+1) == 0 {
				t.runeBuffer = reassemble(t.buffers[j], (i+1)%n, t.runeBuffer)
				if numToks >= cap(t.internalTokenBuffer) || numToks == len(t.internalTokenBuffer) {
					t.internalTokenBuffer = append(t.internalTokenBuffer, Token{Key: make([]byte, 0, TokenKeySize)})
				}
				t.internalTokenBuffer[numToks].Key = t.internalTokenBuffer[numToks].Key[:0]
				t.internalTokenBuffer[numToks].Key = append(t.internalTokenBuffer[numToks].Key, t.runeBuffer...)
				numToks++
			}
		}
		i++
	}
	return t.internalTokenBuffer[0:numToks]
}

func reassemble(buf []rune, pos int, result []byte) []byte {
	result = result[:0] // Reset the result slice
	for i := 0; i < len(buf); i++ {
		cur := (pos + i) % len(buf)
		result = utf8.AppendRune(result, buf[cur])
	}
	return result
}

func chunkIDTransformer(tok Token, prefix []byte) Token {
	tok.Key = append(append(tok.Key, prefix...), tok.Key...)[len(tok.Key):]
	return tok
}

type WrappedTokenizer struct {
	t           Tokenizer
	tokenBuffer []Token
	prefix      []byte
	i64buf      []byte
	i32buf      []byte
}

func (w *WrappedTokenizer) Tokens(line string) []Token {
	w.tokenBuffer = w.tokenBuffer[:0] // Reset the result slice
	toks := w.t.Tokens(line)
	for _, tok := range toks {
		w.tokenBuffer = append(w.tokenBuffer, chunkIDTransformer(tok, w.prefix), tok)
	}

	return w.tokenBuffer
}

func (w *WrappedTokenizer) GetSkip() int {
	return w.t.GetSkip()
}

func (w *WrappedTokenizer) GetMin() int {
	return w.t.GetMin()
}

func (w *WrappedTokenizer) GetMax() int {
	return w.t.GetMax()
}

func ChunkIDTokenizer(t Tokenizer) *WrappedTokenizer {
	p := make([]byte, 0, 256)
	return &WrappedTokenizer{
		t:           t,
		tokenBuffer: make([]Token, 0, TokenBufferSize),
		prefix:      p,
		i64buf:      make([]byte, binary.MaxVarintLen64),
		i32buf:      make([]byte, 4),
	}
}

func (w *WrappedTokenizer) Reinit(chk logproto.ChunkRef) {
	w.prefix = w.prefix[:0]

	binary.PutVarint(w.i64buf, int64(chk.From))
	w.prefix = append(w.prefix, w.i64buf...)
	binary.PutVarint(w.i64buf, int64(chk.Through))
	w.prefix = append(w.prefix, w.i64buf...)
	binary.LittleEndian.PutUint32(w.i32buf, chk.Checksum)
	w.prefix = append(w.prefix, w.i32buf...)
}
