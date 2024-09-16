package v1

import (
	"fmt"
	"unicode/utf8"

	"github.com/grafana/loki/pkg/push"
	iter "github.com/grafana/loki/v3/pkg/iter/v2"
)

const (
	MaxRuneLen = 4
)

type StructuredMetadataTokenizer struct {
	// prefix to add to tokens, typically the encoded chunkref
	prefix string
	tokens []string
}

func NewStructuredMetadataTokenizer(prefix string) *StructuredMetadataTokenizer {
	return &StructuredMetadataTokenizer{
		prefix: prefix,
		tokens: make([]string, 6),
	}
}

// Tokens implements the NGramBuilder interface
func (t *StructuredMetadataTokenizer) Tokens(kv push.LabelAdapter) iter.Iterator[string] {
	combined := fmt.Sprintf("%s=%s", kv.Name, kv.Value)
	t.tokens = append(t.tokens[:0],
		kv.Name, t.prefix+kv.Name,
		kv.Value, t.prefix+kv.Value,
		combined, t.prefix+combined,
	)
	return iter.NewSliceIter(t.tokens)
}

func reassemble(buf []rune, ln, pos int, result []byte) []byte {
	result = result[:0] // Reset the result slice
	for i := 0; i < ln; i++ {
		cur := pos % len(buf)
		pos++
		result = utf8.AppendRune(result, buf[cur])
	}
	return result
}

// Iterable variants (more performant, less space)
type NGramTokenizer struct {
	n, skip int
	buffer  []rune // circular buffer used for ngram generation
	res     []byte // buffer used for token generation
}

func (t *NGramTokenizer) N() int {
	return t.n
}

func (t *NGramTokenizer) SkipFactor() int {
	return t.skip
}

/*
N-Grams (https://en.wikipedia.org/wiki/N-gram) are a series of 'n' adjacent characters in a string.
These will be utilized for the bloom filters to allow for fuzzy searching.
*/
func NewNGramTokenizer(n, skip int) *NGramTokenizer {
	t := &NGramTokenizer{
		n:      n,
		skip:   skip,
		buffer: make([]rune, n+skip),
		res:    make([]byte, 0, n*MaxRuneLen), // maximum 4 bytes per rune
	}

	return t
}

// Token implements the NGramBuilder interface
// The Token iterator uses shared buffers for performance. The []byte returned by At()
// is not safe for use after subsequent calls to Next()
func (t *NGramTokenizer) Tokens(line string) iter.Iterator[[]byte] {
	return &NGramTokenIter{
		n:    t.N(),
		skip: t.SkipFactor(),

		line: line,

		buffer: t.buffer,
		res:    t.res,
	}
}

type NGramTokenIter struct {
	n, skip int

	runeIndex, offset int
	line              string // source

	buffer []rune // circular buffers used for ngram generation
	res    []byte
}

func (t *NGramTokenIter) Next() bool {
	for i, r := range t.line[t.offset:] {
		t.buffer[t.runeIndex%len(t.buffer)] = r
		t.runeIndex++

		if t.runeIndex < t.n {
			continue
		}

		// if the start of the ngram is at the interval of our skip factor, emit it.
		// we increment the skip due to modulo logic:
		// because `n % 0 is a divide by zero and n % 1 is always 0`
		if (t.runeIndex-t.n)%(t.skip+1) == 0 {
			// update the offset, but don't go past the end of the line;
			// for instance invalid utf-8
			t.offset = min(len(t.line), t.offset+i+utf8.RuneLen(r))
			return true
		}

	}
	return false
}

func (t *NGramTokenIter) At() []byte {
	return reassemble(t.buffer, t.n, (t.runeIndex-t.n)%len(t.buffer), t.res[:0])
}

func (t *NGramTokenIter) Err() error {
	return nil
}

type PrefixedTokenIter struct {
	buf       []byte
	prefixLen int

	iter.Iterator[[]byte]
}

func (t *PrefixedTokenIter) At() []byte {
	return append(t.buf[:t.prefixLen], t.Iterator.At()...)
}

func NewPrefixedTokenIter(buf []byte, prefixLn int, itr iter.Iterator[[]byte]) *PrefixedTokenIter {
	return &PrefixedTokenIter{
		buf:       buf,
		prefixLen: prefixLn,
		Iterator:  itr,
	}
}
