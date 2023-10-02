package main

import (
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
			exp:   nil,
		},
		{
			desc:  "single char",
			input: "a",
			exp:   nil,
		},
		{
			desc:  "two chars",
			input: "ab",
			exp:   []Token{{Key: "ab", Value: ""}},
		},
		{
			desc:  "three chars",
			input: "abc",
			exp:   []Token{{Key: "ab", Value: ""}, {Key: "bc", Value: ""}, {Key: "abc", Value: ""}},
		},
		{
			desc:  "four chars",
			input: "abcd",
			exp:   []Token{{Key: "ab", Value: ""}, {Key: "bc", Value: ""}, {Key: "abc", Value: ""}, {Key: "cd", Value: ""}, {Key: "bcd", Value: ""}},
		},
		{
			desc:  "foo",
			input: "日本語",
			exp:   []Token{{Key: "日本", Value: ""}, {Key: "本語", Value: ""}, {Key: "日本語", Value: ""}},
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
			exp:       []Token{{Key: "ab", Value: ""}, {Key: "cd", Value: ""}},
		},
		{
			desc:      "special chars",
			tokenizer: twoSkipOne,
			input:     "日本語",
			exp:       []Token{{Key: "日本", Value: ""}},
		},
		{
			desc:      "multi",
			tokenizer: newNGramTokenizer(2, 4, 1),
			input:     "abcdefghij",
			exp: []Token{
				{Key: "ab", Value: ""},
				{Key: "abc", Value: ""},
				{Key: "cd", Value: ""},
				{Key: "cde", Value: ""},
				{Key: "ef", Value: ""},
				{Key: "efg", Value: ""},
				{Key: "gh", Value: ""},
				{Key: "ghi", Value: ""},
				{Key: "ij", Value: ""},
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			require.Equal(t, tc.exp, tc.tokenizer.Tokens(tc.input))
		})
	}
}
