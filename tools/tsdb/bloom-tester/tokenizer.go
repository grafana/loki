package main

import (
	"encoding/binary"
	"unicode/utf8"

	"github.com/grafana/loki/pkg/logproto"
)

type Token struct {
	Key   []byte
	Value string
}

type Tokenizer interface {
	Tokens(line string) []Token
	getSkip() int
	getMin() int
	getMax() int
}

/*
type logfmtTokenizer struct {
	parser *log.LogfmtParser
	lbls   *log.LabelsBuilder
}

func (t *logfmtTokenizer) Tokens(line string) []Token {
	t.lbls.Reset()
	t.parser.Process(0, []byte(line), t.lbls)
	ls := t.lbls.LabelsResult().Labels()
	res := make([]Token, 0, len(ls))
	for _, l := range ls {
		res = append(res, Token{Key: l.Name, Value: l.Value})
	}
	return res
}

func newLogfmtTokenizer() *logfmtTokenizer {
	return &logfmtTokenizer{
		// non strict, allow empty values
		parser: log.NewLogfmtParser(false, true),
		lbls:   log.NewBaseLabelsBuilder().ForLabels(nil, 0),
	}
}

*/

type ngramTokenizer struct {
	// [min,max) exclusivity
	min, max, skip int
	buffers        [][]rune // circular buffers used for ngram generation
	runeBuffer     []byte   // buffer used for token generation
	tokenBuffer    []Token  // buffer used for holding tokens
}

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
	t.tokenBuffer = make([]Token, 0, 1024)

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
				b := Token{}
				b.Key = make([]byte, 0, 132) // TODO: Yeah, that's too big but I didn't fee like doing the math at the end of the day
				b.Key = append(b.Key, t.runeBuffer...)
				b.Value = string(b.Key)
				t.tokenBuffer = append(t.tokenBuffer, b)
			}
		}
		i++
	}
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

func ChunkIDTokenizer(chk logproto.ChunkRef, t Tokenizer) *WrappedTokenizer {
	//prefix := fmt.Sprintf("%d:%d:%d:", chk.From, chk.Through, chk.Checksum)
	p := make([]byte, 0, 256)
	i64buf := make([]byte, binary.MaxVarintLen64)
	i32buf := make([]byte, 4)

	binary.PutVarint(i64buf, int64(chk.From))
	p = append(p, i64buf...)
	p = append(p, 58)
	binary.PutVarint(i64buf, int64(chk.Through))
	p = append(p, i64buf...)
	p = append(p, 58)
	binary.LittleEndian.PutUint32(i32buf, chk.Checksum)
	p = append(p, i32buf...)
	p = append(p, 58)

	return &WrappedTokenizer{
		t: t,
		f: func(tok Token) Token {
			tok.Key = append(append(tok.Key, p...), tok.Key...)[len(tok.Key):]
			tok.Value = string(tok.Key)
			return tok
		},
		tokenBuffer: make([]Token, 0, 1024),
		prefix:      p,
		i64buf:      i64buf,
		i32buf:      i32buf,
	}
}

func ChunkIDTokenizerHalfInit(t Tokenizer) *WrappedTokenizer {
	p := make([]byte, 0, 256)
	return &WrappedTokenizer{
		t:           t,
		tokenBuffer: make([]Token, 0, 1024),
		prefix:      p,
		i64buf:      make([]byte, binary.MaxVarintLen64),
		i32buf:      make([]byte, 4),
	}
}

func (w *WrappedTokenizer) reinit(chk logproto.ChunkRef) {
	//prefix := fmt.Sprintf("%d:%d:%d:", chk.From, chk.Through, chk.Checksum)
	w.prefix = w.prefix[:0]

	//w.prefix = fmt.Appendf(w.prefix, "%d:%d:%d:", chk.From, chk.Through, chk.Checksum)
	binary.PutVarint(w.i64buf, int64(chk.From))
	w.prefix = append(w.prefix, w.i64buf...)
	w.prefix = append(w.prefix, 58)
	binary.PutVarint(w.i64buf, int64(chk.Through))
	w.prefix = append(w.prefix, w.i64buf...)
	w.prefix = append(w.prefix, 58)
	binary.LittleEndian.PutUint32(w.i32buf, chk.Checksum)
	w.prefix = append(w.prefix, w.i32buf...)
	w.prefix = append(w.prefix, 58)

	w.f = func(tok Token) Token {
		tok.Key = append(append(tok.Key, w.prefix...), tok.Key...)[len(tok.Key):]
		tok.Value = string(tok.Key)
		return tok
	}
}
