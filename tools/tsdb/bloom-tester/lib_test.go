package main

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
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

func Test4NGrams(t *testing.T) {
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
			desc:  "two chars",
			input: "ab",
			exp:   []Token{},
		},
		{
			desc:  "three chars",
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

func Test6NGrams(t *testing.T) {
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
			desc:  "two chars",
			input: "ab",
			exp:   []Token{},
		},
		{
			desc:  "three chars",
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
		{
			desc:  "eight chars",
			input: "abcdefgh",
			exp:   []Token{{Key: []byte("abcdef"), Value: "abcdef"}, {Key: []byte("bcdefg"), Value: "bcdefg"}, {Key: []byte("cdefgh"), Value: "cdefgh"}},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			require.Equal(t, tc.exp, tokenizer.Tokens(tc.input))
		})
	}
}

func TestNGramsSkip(t *testing.T) {
	twoSkipOne := newNGramTokenizer(2, 3, 1)
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

var num = 1000000

func BenchmarkLRU1Put(b *testing.B) {
	cache := NewLRUCache(num)
	for i := 0; i < b.N; i++ {
		cache.Put(strconv.Itoa(i))
	}
}

func BenchmarkLRU1Get(b *testing.B) {
	cache := NewLRUCache(num)
	for i := 0; i < num; i++ {
		cache.Put(strconv.Itoa(i))
	}
	for i := 0; i < b.N; i++ {
		cache.Get(strconv.Itoa(i))
	}
}

func BenchmarkLRU2Put(b *testing.B) {
	cache := NewLRUCache2(num)
	for i := 0; i < b.N; i++ {
		cache.Put(strconv.Itoa(i))
	}
}

func BenchmarkLRU2Get(b *testing.B) {
	cache := NewLRUCache2(num)
	for i := 0; i < num; i++ {
		cache.Put(strconv.Itoa(i))
	}
	for i := 0; i < b.N; i++ {
		cache.Get(strconv.Itoa(i))
	}
}

func BenchmarkLRU3Put(b *testing.B) {
	cache := NewLRUCache2(num)
	for i := 0; i < b.N; i++ {
		cache.Put(strconv.Itoa(i))
	}
}

func BenchmarkLRU3Get(b *testing.B) {
	cache := NewLRUCache2(num)
	for i := 0; i < num; i++ {
		cache.Put(strconv.Itoa(i))
	}
	for i := 0; i < b.N; i++ {
		cache.Get(strconv.Itoa(i))
	}
}
