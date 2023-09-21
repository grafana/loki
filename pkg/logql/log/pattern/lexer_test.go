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
	} {
		tc := tc
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

// TestIdentifierToLower tests that the identifier lexer function converts the identifier to lowercase
func TestIdentifierToLower(t *testing.T) {
	lex := newLexer()
	inputData := []byte("{MyLabel}")
	lex.setData(inputData)

	var out exprSymType
	lex.ts = 0              // Set the start position to 0
	lex.te = len(inputData) // Set the end position to the length of inputData

	token, err := lex.identifier(&out)

	if err != nil {
		t.Errorf("Expected no error, but got %v", err)
	}

	expectedToken := IDENTIFIER
	if token != expectedToken {
		t.Errorf("Expected token %d, but got %d", expectedToken, token)
	}

	expectedIdentifier := "mylabel"
	if out.str != expectedIdentifier {
		t.Errorf("Expected identifier %s, but got %s", expectedIdentifier, out.str)
	}
}
