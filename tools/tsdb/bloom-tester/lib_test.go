package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNGrams(t *testing.T) {
	tokenizer := newNGramTokenizer(2, 4)
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
	} {
		t.Run(tc.desc, func(t *testing.T) {
			require.Equal(t, tc.exp, tokenizer.Tokens(tc.input))
		})
	}
}
