package pattern

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_Lex(t *testing.T) {
	for _, tc := range []struct {
		input    string
		expected []int
	}{
		{`_foo`, []int{LITERAL, LITERAL, LITERAL, LITERAL}},
		{`<foo`, []int{LITERAL, LITERAL, LITERAL, LITERAL}},
		{`<`, []int{LITERAL}},
		{`>`, []int{LITERAL}},
		{`<_1foo>`, []int{IDENTIFIER}},
		{`<_1foo> bar <buzz>`, []int{IDENTIFIER, LITERAL, LITERAL, LITERAL, LITERAL, LITERAL, IDENTIFIER}},
		{`<1foo>`, []int{LITERAL, LITERAL, LITERAL, LITERAL, LITERAL, LITERAL}},
		{`▶`, []int{LITERAL}},
	} {
		t.Run(tc.input, func(t *testing.T) {
			actual := []int{}
			l := newLexer()
			l.setData([]byte(tc.input))
			for {
				var lval exprSymType
				tok := l.Lex(&lval)
				if tok == 0 {
					break
				}
				actual = append(actual, tok)
			}
			assert.Equal(t, toksToStrings(tc.expected), toksToStrings(actual))
			assert.Equal(t, tc.expected, actual)
		})
	}
}

func toksToStrings(toks []int) []string {
	strings := make([]string, len(toks))
	for i, tok := range toks {
		strings[i] = exprToknames[tok-exprPrivate+1]
	}
	return strings
}
