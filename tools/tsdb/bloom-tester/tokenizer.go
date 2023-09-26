package main

import (
	"fmt"
	"unicode/utf8"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/log"
)

type Token struct {
	// Either key or value may be empty
	Key, Value string
}
type Tokenizer interface {
	Tokens(line string) []Token
}

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

type ngramTokenizer struct {
	// [min,max) exclusivity
	min, max, skip int
	buffers        [][]rune // circular buffers used for ngram generation
}

func newNGramTokenizer(min, max, skip int) *ngramTokenizer {
	t := &ngramTokenizer{
		min:  min,
		max:  max,
		skip: skip,
	}
	for i := t.min; i < t.max; i++ {
		t.buffers = append(t.buffers, make([]rune, i))
	}

	return t

}

func (t *ngramTokenizer) Tokens(line string) (res []Token) {
	var i int // rune index (not position that is measured in the range loop)
	for _, r := range line {

		// j is the index of the buffer to use
		for j := 0; j < (t.max - t.min); j++ {
			// n is the length of the ngram
			n := j + t.min
			// pos is the position in the buffer to overwrite
			pos := i % n
			t.buffers[j][pos] = r

			if i >= n-1 && (i+1-n)%(t.skip+1) == 0 {
				ngram := reassemble(t.buffers[j], (i+1)%n)
				res = append(res, Token{Key: string(ngram), Value: ""})
			}
		}
		i++
	}
	return
}

func reassemble(buf []rune, pos int) []byte {
	res := make([]byte, 0, len(buf)*4) // 4 bytes per rune (i32)
	for i := 0; i < len(buf); i++ {
		cur := (pos + i) % len(buf)
		res = utf8.AppendRune(res, buf[cur])
	}
	return res
}

type WrappedTokenizer struct {
	t Tokenizer
	f func(Token) Token
}

func (w *WrappedTokenizer) Tokens(line string) []Token {
	toks := w.t.Tokens(line)
	res := make([]Token, 0, len(toks))
	for _, tok := range toks {
		res = append(res, w.f(tok))
	}
	return append(res, toks...)
}

func ChunkIDTokenizer(chk logproto.ChunkRef, t Tokenizer) *WrappedTokenizer {
	return &WrappedTokenizer{
		t: t,
		f: func(tok Token) Token {
			tok.Key = fmt.Sprintf("%d:%d:%d:%s", chk.From, chk.Through, chk.Checksum, tok.Key)
			return tok
		},
	}
}
