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
	getSkip() int
	getMin() int
	getMax() int
}

const TokenBufferSize = 4096

type ngramTokenizer struct {
	// [min,max) exclusivity
	min, max, skip      int
	buffers             [][]rune // circular buffers used for ngram generation
	runeBuffer          []byte   // buffer used for token generation
	tokenBuffer         []Token  // buffer used for holding tokens that is returned
	internalTokenBuffer []Token  // circular buffer for tokens
}

/*
N-Grams (https://en.wikipedia.org/wiki/N-gram) are a series of 'n' adjacent characters in a string.
These will be utilized for the bloom filters to allow for fuzzy searching.
*/
func newNGramTokenizer(min, max, skip int) *ngramTokenizer {
	capacity := max - min
	t := &ngramTokenizer{
		min:     min,
		max:     max,
		skip:    skip,
		buffers: make([][]rune, capacity),
	}
	for i := t.min; i < t.max; i++ {
		t.buffers[i-t.min] = make([]rune, i)
	}
	t.runeBuffer = make([]byte, 0, max*4)
	t.tokenBuffer = make([]Token, 0, TokenBufferSize)
	t.internalTokenBuffer = make([]Token, 0, TokenBufferSize)
	for i := 0; i < cap(t.internalTokenBuffer); i++ {
		tok := Token{}
		tok.Key = make([]byte, 0, 132)
		t.internalTokenBuffer = append(t.internalTokenBuffer, tok)
	}

	return t
}

func (t *ngramTokenizer) getSkip() int {
	return t.skip
}

func (t *ngramTokenizer) getMin() int {
	return t.min
}

func (t *ngramTokenizer) getMax() int {
	return t.max
}

func (t *ngramTokenizer) Tokens(line string) []Token {
	t.tokenBuffer = t.tokenBuffer[:0] // Reset the result slice
	var i int                         // rune index (not position that is measured in the range loop)
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
					tok := Token{}
					tok.Key = make([]byte, 0, 132) // Using a 4 byte token and a chunk identifier, it's really 28 bytes. Adding in for special chars and the like here
					t.internalTokenBuffer = append(t.internalTokenBuffer, tok)
				}
				t.internalTokenBuffer[numToks].Key = t.internalTokenBuffer[numToks].Key[:0]
				t.internalTokenBuffer[numToks].Key = append(t.internalTokenBuffer[numToks].Key, t.runeBuffer...)
				numToks++
			}
		}
		i++
	}
	t.tokenBuffer = append(t.tokenBuffer, t.internalTokenBuffer[:numToks]...)
	return t.tokenBuffer
}

func reassemble(buf []rune, pos int, result []byte) []byte {
	result = result[:0] // Reset the result slice
	for i := 0; i < len(buf); i++ {
		cur := (pos + i) % len(buf)
		result = utf8.AppendRune(result, buf[cur])
	}
	return result
}

type WrappedTokenizer struct {
	t           Tokenizer
	f           func(Token) Token
	tokenBuffer []Token
	prefix      []byte
	i64buf      []byte
	i32buf      []byte
}

func (w *WrappedTokenizer) Tokens(line string) []Token {
	w.tokenBuffer = w.tokenBuffer[:0] // Reset the result slice
	toks := w.t.Tokens(line)
	for _, tok := range toks {
		w.tokenBuffer = append(w.tokenBuffer, w.f(tok))
	}
	return append(w.tokenBuffer, toks...)
}

func (w *WrappedTokenizer) getSkip() int {
	return w.t.getSkip()
}

func (w *WrappedTokenizer) getMin() int {
	return w.t.getMin()
}

func (w *WrappedTokenizer) getMax() int {
	return w.t.getMax()
}

func ChunkIDTokenizer(t Tokenizer) *WrappedTokenizer {
	p := make([]byte, 0, 256)
	return &WrappedTokenizer{
		t:           t,
		tokenBuffer: make([]Token, 0, TokenBufferSize),
		prefix:      p,
		i64buf:      make([]byte, binary.MaxVarintLen64),
		i32buf:      make([]byte, 4),
		f: func(tok Token) Token {
			tok.Key = append(append(tok.Key, p...), tok.Key...)[len(tok.Key):]
			return tok
		},
	}
}

func (w *WrappedTokenizer) reinit(chk logproto.ChunkRef) {
	w.prefix = w.prefix[:0]

	binary.PutVarint(w.i64buf, int64(chk.From))
	w.prefix = append(w.prefix, w.i64buf...)
	binary.PutVarint(w.i64buf, int64(chk.Through))
	w.prefix = append(w.prefix, w.i64buf...)
	binary.LittleEndian.PutUint32(w.i32buf, chk.Checksum)
	w.prefix = append(w.prefix, w.i32buf...)

	w.f = func(tok Token) Token {
		tok.Key = append(append(tok.Key, w.prefix...), tok.Key...)[len(tok.Key):]
		return tok
	}
}
