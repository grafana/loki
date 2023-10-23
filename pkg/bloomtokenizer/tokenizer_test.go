package bloomtokenizer

import (
	"bufio"
	"encoding/binary"
	"os"
	"testing"

	"github.com/grafana/loki/pkg/logproto"

	"github.com/stretchr/testify/require"
)

var (
	twoSkipOne = newNGramTokenizer(2, 3, 1)
	three      = newNGramTokenizer(3, 4, 0)
	threeSkip1 = newNGramTokenizer(3, 4, 1)
	threeSkip2 = newNGramTokenizer(3, 4, 2)
	four       = newNGramTokenizer(4, 5, 0)
	fourSkip1  = newNGramTokenizer(4, 5, 1)
	fourSkip2  = newNGramTokenizer(4, 5, 2)
	five       = newNGramTokenizer(5, 6, 0)
	six        = newNGramTokenizer(6, 7, 0)
)

func TestNGrams(t *testing.T) {
	tokenizer := newNGramTokenizer(2, 4, 0)
	for _, tc := range []struct {
		desc  string
		input string
		exp   []Token
	}{
		{
			desc:  "empty",
			input: "",
			exp:   []Token{},
		},
		{
			desc:  "single char",
			input: "a",
			exp:   []Token{},
		},
		{
			desc:  "two chars",
			input: "ab",
			exp:   []Token{{Key: []byte("ab"), Value: "ab"}},
		},
		{
			desc:  "three chars",
			input: "abc",
			exp:   []Token{{Key: []byte("ab"), Value: "ab"}, {Key: []byte("bc"), Value: "bc"}, {Key: []byte("abc"), Value: "abc"}},
		},
		{
			desc:  "four chars",
			input: "abcd",
			exp:   []Token{{Key: []byte("ab"), Value: "ab"}, {Key: []byte("bc"), Value: "bc"}, {Key: []byte("abc"), Value: "abc"}, {Key: []byte("cd"), Value: "cd"}, {Key: []byte("bcd"), Value: "bcd"}},
		},
		{
			desc:  "foo",
			input: "日本語",
			exp:   []Token{{Key: []byte("日本"), Value: "日本"}, {Key: []byte("本語"), Value: "本語"}, {Key: []byte("日本語"), Value: "日本語"}},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			require.Equal(t, tc.exp, tokenizer.Tokens(tc.input))
		})
	}
}

func TestNGramsSkip(t *testing.T) {

	for _, tc := range []struct {
		desc      string
		tokenizer *ngramTokenizer
		input     string
		exp       []Token
	}{
		{
			desc:      "four chars",
			tokenizer: twoSkipOne,
			input:     "abcd",
			exp:       []Token{{Key: []byte("ab"), Value: "ab"}, {Key: []byte("cd"), Value: "cd"}},
		},
		{
			desc:      "special chars",
			tokenizer: twoSkipOne,
			input:     "日本語",
			exp:       []Token{{Key: []byte("日本"), Value: "日本"}},
		},
		{
			desc:      "multi",
			tokenizer: newNGramTokenizer(2, 4, 1),
			input:     "abcdefghij",
			exp: []Token{
				{Key: []byte("ab"), Value: "ab"},
				{Key: []byte("abc"), Value: "abc"},
				{Key: []byte("cd"), Value: "cd"},
				{Key: []byte("cde"), Value: "cde"},
				{Key: []byte("ef"), Value: "ef"},
				{Key: []byte("efg"), Value: "efg"},
				{Key: []byte("gh"), Value: "gh"},
				{Key: []byte("ghi"), Value: "ghi"},
				{Key: []byte("ij"), Value: "ij"},
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			require.Equal(t, tc.exp, tc.tokenizer.Tokens(tc.input))
		})
	}
}

func Test3GramSkip0Tokenizer(t *testing.T) {
	tokenizer := three
	for _, tc := range []struct {
		desc  string
		input string
		exp   []Token
	}{
		{
			desc:  "empty",
			input: "",
			exp:   []Token{},
		},
		{
			desc:  "single char",
			input: "a",
			exp:   []Token{},
		},
		{
			desc:  "three char",
			input: "abc",
			exp:   []Token{{Key: []byte("abc"), Value: "abc"}},
		},
		{
			desc:  "four chars",
			input: "abcd",
			exp:   []Token{{Key: []byte("abc"), Value: "abc"}, {Key: []byte("bcd"), Value: "bcd"}},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			require.Equal(t, tc.exp, tokenizer.Tokens(tc.input))
		})
	}
}

func Test3GramSkip1Tokenizer(t *testing.T) {
	tokenizer := threeSkip1
	for _, tc := range []struct {
		desc  string
		input string
		exp   []Token
	}{
		{
			desc:  "empty",
			input: "",
			exp:   []Token{},
		},
		{
			desc:  "single char",
			input: "a",
			exp:   []Token{},
		},
		{
			desc:  "three char",
			input: "abc",
			exp:   []Token{{Key: []byte("abc"), Value: "abc"}},
		},
		{
			desc:  "four chars",
			input: "abcd",
			exp:   []Token{{Key: []byte("abc"), Value: "abc"}},
		},
		{
			desc:  "five chars",
			input: "abcde",
			exp:   []Token{{Key: []byte("abc"), Value: "abc"}, {Key: []byte("cde"), Value: "cde"}},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			require.Equal(t, tc.exp, tokenizer.Tokens(tc.input))
		})
	}
}

func Test3GramSkip2Tokenizer(t *testing.T) {
	tokenizer := threeSkip2
	for _, tc := range []struct {
		desc  string
		input string
		exp   []Token
	}{
		{
			desc:  "empty",
			input: "",
			exp:   []Token{},
		},
		{
			desc:  "single char",
			input: "a",
			exp:   []Token{},
		},
		{
			desc:  "four chars",
			input: "abcd",
			exp:   []Token{{Key: []byte("abc"), Value: "abc"}},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			require.Equal(t, tc.exp, tokenizer.Tokens(tc.input))
		})
	}
}

func Test4GramSkip0Tokenizer(t *testing.T) {
	tokenizer := four
	for _, tc := range []struct {
		desc  string
		input string
		exp   []Token
	}{
		{
			desc:  "empty",
			input: "",
			exp:   []Token{},
		},
		{
			desc:  "single char",
			input: "a",
			exp:   []Token{},
		},
		{
			desc:  "three char",
			input: "abc",
			exp:   []Token{},
		},
		{
			desc:  "four chars",
			input: "abcd",
			exp:   []Token{{Key: []byte("abcd"), Value: "abcd"}},
		},
		{
			desc:  "five chars",
			input: "abcde",
			exp:   []Token{{Key: []byte("abcd"), Value: "abcd"}, {Key: []byte("bcde"), Value: "bcde"}},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			require.Equal(t, tc.exp, tokenizer.Tokens(tc.input))
		})
	}
}

func Test4GramSkip1Tokenizer(t *testing.T) {
	tokenizer := fourSkip1
	for _, tc := range []struct {
		desc  string
		input string
		exp   []Token
	}{
		{
			desc:  "empty",
			input: "",
			exp:   []Token{},
		},
		{
			desc:  "single char",
			input: "a",
			exp:   []Token{},
		},
		{
			desc:  "three char",
			input: "abc",
			exp:   []Token{},
		},
		{
			desc:  "four chars",
			input: "abcd",
			exp:   []Token{{Key: []byte("abcd"), Value: "abcd"}},
		},
		{
			desc:  "five chars",
			input: "abcde",
			exp:   []Token{{Key: []byte("abcd"), Value: "abcd"}},
		},
		{
			desc:  "six chars",
			input: "abcdef",
			exp:   []Token{{Key: []byte("abcd"), Value: "abcd"}, {Key: []byte("cdef"), Value: "cdef"}},
		},
		{
			desc:  "seven chars",
			input: "abcdefg",
			exp:   []Token{{Key: []byte("abcd"), Value: "abcd"}, {Key: []byte("cdef"), Value: "cdef"}},
		},
		{
			desc:  "eight chars",
			input: "abcdefgh",
			exp:   []Token{{Key: []byte("abcd"), Value: "abcd"}, {Key: []byte("cdef"), Value: "cdef"}, {Key: []byte("efgh"), Value: "efgh"}},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			require.Equal(t, tc.exp, tokenizer.Tokens(tc.input))
		})
	}
}

func Test4GramSkip2Tokenizer(t *testing.T) {
	tokenizer := fourSkip2
	for _, tc := range []struct {
		desc  string
		input string
		exp   []Token
	}{
		{
			desc:  "empty",
			input: "",
			exp:   []Token{},
		},
		{
			desc:  "single char",
			input: "a",
			exp:   []Token{},
		},
		{
			desc:  "three char",
			input: "abc",
			exp:   []Token{},
		},
		{
			desc:  "four chars",
			input: "abcd",
			exp:   []Token{{Key: []byte("abcd"), Value: "abcd"}},
		},
		{
			desc:  "five chars",
			input: "abcde",
			exp:   []Token{{Key: []byte("abcd"), Value: "abcd"}},
		},
		{
			desc:  "six chars",
			input: "abcdef",
			exp:   []Token{{Key: []byte("abcd"), Value: "abcd"}},
		},
		{
			desc:  "seven chars",
			input: "abcdefg",
			exp:   []Token{{Key: []byte("abcd"), Value: "abcd"}, {Key: []byte("defg"), Value: "defg"}},
		},
		{
			desc:  "eight chars",
			input: "abcdefgh",
			exp:   []Token{{Key: []byte("abcd"), Value: "abcd"}, {Key: []byte("defg"), Value: "defg"}},
		},
		{
			desc:  "nine chars",
			input: "abcdefghi",
			exp:   []Token{{Key: []byte("abcd"), Value: "abcd"}, {Key: []byte("defg"), Value: "defg"}},
		},
		{
			desc:  "ten chars",
			input: "abcdefghij",
			exp:   []Token{{Key: []byte("abcd"), Value: "abcd"}, {Key: []byte("defg"), Value: "defg"}, {Key: []byte("ghij"), Value: "ghij"}},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			require.Equal(t, tc.exp, tokenizer.Tokens(tc.input))
		})
	}
}

func Test5GramSkip0Tokenizer(t *testing.T) {
	tokenizer := five
	for _, tc := range []struct {
		desc  string
		input string
		exp   []Token
	}{
		{
			desc:  "empty",
			input: "",
			exp:   []Token{},
		},
		{
			desc:  "single char",
			input: "a",
			exp:   []Token{},
		},
		{
			desc:  "three char",
			input: "abc",
			exp:   []Token{},
		},
		{
			desc:  "four chars",
			input: "abcd",
			exp:   []Token{},
		},
		{
			desc:  "five chars",
			input: "abcde",
			exp:   []Token{{Key: []byte("abcde"), Value: "abcde"}},
		},
		{
			desc:  "six chars",
			input: "abcdef",
			exp:   []Token{{Key: []byte("abcde"), Value: "abcde"}, {Key: []byte("bcdef"), Value: "bcdef"}},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			require.Equal(t, tc.exp, tokenizer.Tokens(tc.input))
		})
	}
}

func Test6GramSkip0Tokenizer(t *testing.T) {
	tokenizer := six
	for _, tc := range []struct {
		desc  string
		input string
		exp   []Token
	}{
		{
			desc:  "empty",
			input: "",
			exp:   []Token{},
		},
		{
			desc:  "single char",
			input: "a",
			exp:   []Token{},
		},
		{
			desc:  "three char",
			input: "abc",
			exp:   []Token{},
		},
		{
			desc:  "four chars",
			input: "abcd",
			exp:   []Token{},
		},
		{
			desc:  "five chars",
			input: "abcde",
			exp:   []Token{},
		},
		{
			desc:  "six chars",
			input: "abcdef",
			exp:   []Token{{Key: []byte("abcdef"), Value: "abcdef"}},
		},
		{
			desc:  "seven chars",
			input: "abcdefg",
			exp:   []Token{{Key: []byte("abcdef"), Value: "abcdef"}, {Key: []byte("bcdefg"), Value: "bcdefg"}},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			require.Equal(t, tc.exp, tokenizer.Tokens(tc.input))
		})
	}
}

func makeBuf(from, through, checksum int) []byte {
	p := make([]byte, 0, 256)
	i64buf := make([]byte, binary.MaxVarintLen64)
	i32buf := make([]byte, 4)

	binary.PutVarint(i64buf, int64(from))
	p = append(p, i64buf...)
	p = append(p, 58)
	binary.PutVarint(i64buf, int64(through))
	p = append(p, i64buf...)
	p = append(p, 58)
	binary.LittleEndian.PutUint32(i32buf, uint32(checksum))
	p = append(p, i32buf...)
	p = append(p, 58)
	return p
}

func TestWrappedTokenizer(t *testing.T) {
	tokenizer := threeSkip2
	for _, tc := range []struct {
		desc  string
		input string
		exp   []Token
	}{
		{
			desc:  "empty",
			input: "",
			exp:   []Token{},
		},
		{
			desc:  "single char",
			input: "a",
			exp:   []Token{},
		},
		{
			desc:  "four chars",
			input: "abcd",
			exp: []Token{
				{Key: append(makeBuf(0, 999999, 1), []byte("abc")...), Value: string(makeBuf(0, 999999, 1)) + "abc"},
				{Key: []byte("abc"), Value: "abc"}},
		},
		{
			desc:  "uuid",
			input: "2b1a5e46-36a2-4694-a4b1-f34cc7bdfc45",
			exp: []Token{
				{Key: append(makeBuf(0, 999999, 1), []byte("2b1")...), Value: string(makeBuf(0, 999999, 1)) + "2b1"},
				{Key: append(makeBuf(0, 999999, 1), []byte("a5e")...), Value: string(makeBuf(0, 999999, 1)) + "a5e"},
				{Key: append(makeBuf(0, 999999, 1), []byte("46-")...), Value: string(makeBuf(0, 999999, 1)) + "46-"},
				{Key: append(makeBuf(0, 999999, 1), []byte("36a")...), Value: string(makeBuf(0, 999999, 1)) + "36a"},
				{Key: append(makeBuf(0, 999999, 1), []byte("2-4")...), Value: string(makeBuf(0, 999999, 1)) + "2-4"},
				{Key: append(makeBuf(0, 999999, 1), []byte("694")...), Value: string(makeBuf(0, 999999, 1)) + "694"},
				{Key: append(makeBuf(0, 999999, 1), []byte("-a4")...), Value: string(makeBuf(0, 999999, 1)) + "-a4"},
				{Key: append(makeBuf(0, 999999, 1), []byte("b1-")...), Value: string(makeBuf(0, 999999, 1)) + "b1-"},
				{Key: append(makeBuf(0, 999999, 1), []byte("f34")...), Value: string(makeBuf(0, 999999, 1)) + "f34"},
				{Key: append(makeBuf(0, 999999, 1), []byte("cc7")...), Value: string(makeBuf(0, 999999, 1)) + "cc7"},
				{Key: append(makeBuf(0, 999999, 1), []byte("bdf")...), Value: string(makeBuf(0, 999999, 1)) + "bdf"},
				{Key: append(makeBuf(0, 999999, 1), []byte("c45")...), Value: string(makeBuf(0, 999999, 1)) + "c45"},
				{Key: []byte("2b1"), Value: "2b1"},
				{Key: []byte("a5e"), Value: "a5e"},
				{Key: []byte("46-"), Value: "46-"},
				{Key: []byte("36a"), Value: "36a"},
				{Key: []byte("2-4"), Value: "2-4"},
				{Key: []byte("694"), Value: "694"},
				{Key: []byte("-a4"), Value: "-a4"},
				{Key: []byte("b1-"), Value: "b1-"},
				{Key: []byte("f34"), Value: "f34"},
				{Key: []byte("cc7"), Value: "cc7"},
				{Key: []byte("bdf"), Value: "bdf"},
				{Key: []byte("c45"), Value: "c45"},
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			chunkTokenizer := ChunkIDTokenizer(tokenizer)
			chunkTokenizer.reinit(logproto.ChunkRef{From: 0, Through: 999999, Checksum: 1})
			require.Equal(t, tc.exp, chunkTokenizer.Tokens(tc.input))
		})
	}
}

func BenchmarkTokens(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		file, _ := os.Open("../../../pkg/logql/sketch/testdata/war_peace.txt")
		defer file.Close()
		scanner := bufio.NewScanner(file)

		b.StartTimer()
		for scanner.Scan() {
			line := scanner.Text()
			_ = three.Tokens(line)
		}
	}
}
