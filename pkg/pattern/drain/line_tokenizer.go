package drain

import (
	"strings"
	"unicode"
)

type LineTokenizer interface {
	Tokenize(line string) ([]string, interface{})
	Join(tokens []string, state interface{}) string
}

type spacesTokenizer struct{}

func (spacesTokenizer) Tokenize(line string) ([]string, interface{}) {
	return strings.Split(line, " "), nil
}

func (spacesTokenizer) Join(tokens []string, _ interface{}) string {
	return strings.Join(tokens, " ")
}

type punctuationTokenizer struct {
	includeDelimiters [128]rune
	excludeDelimiters [128]rune
}

func newPunctuationTokenizer() *punctuationTokenizer {
	var included [128]rune
	var excluded [128]rune
	included['='] = 1
	excluded['_'] = 1
	excluded['-'] = 1
	return &punctuationTokenizer{
		includeDelimiters: included,
		excludeDelimiters: excluded,
	}
}

func (p *punctuationTokenizer) Tokenize(line string) ([]string, interface{}) {
	tokens := make([]string, 0, 128)  // ~p90 for most log types except json
	spacesAfter := make([]int, 0, 64) // p95 metadata size.

	start := 0
	for i, char := range line {
		if len(tokens) >= cap(tokens)-1 {
			// Line is too long: append the rest of the string as a single token before returning
			break
		}
		if unicode.IsLetter(char) || unicode.IsNumber(char) || char < 128 && p.excludeDelimiters[char] != 0 {
			continue
		}
		included := char < 128 && p.includeDelimiters[char] != 0
		if char == ' ' || included || unicode.IsPunct(char) {
			if i > start {
				tokens = append(tokens, line[start:i])
			}
			if char == ' ' {
				spacesAfter = append(spacesAfter, len(tokens)-1)
			} else {
				tokens = append(tokens, line[i:i+1])
			}
			start = i + 1
		}
	}

	if start < len(line) {
		tokens = append(tokens, line[start:])
	}

	return tokens, spacesAfter
}

func (p *punctuationTokenizer) Join(tokens []string, state interface{}) string {
	spacesAfter := state.([]int)
	strBuilder := strings.Builder{}
	spacesIdx := 0
	for i, token := range tokens {
		strBuilder.WriteString(token)
		for spacesIdx < len(spacesAfter) && i == spacesAfter[spacesIdx] {
			// One entry for each space following the token
			strBuilder.WriteRune(' ')
			spacesIdx++
		}
	}
	return strBuilder.String()
}

type splittingTokenizer struct{}

func (splittingTokenizer) Tokenize(line string) ([]string, interface{}) {
	numEquals := strings.Count(line, "=")
	numColons := strings.Count(line, ":")
	numSpaces := strings.Count(line, " ")

	expectedTokens := numSpaces + numEquals
	keyvalSeparator := "="
	if numColons > numEquals {
		keyvalSeparator = ":"
		expectedTokens = numSpaces + numColons
	}

	tokens := make([]string, 0, expectedTokens)
	spacesAfter := make([]int, 0, strings.Count(line, " "))
	for _, token := range strings.SplitAfter(line, keyvalSeparator) {
		words := strings.Split(token, " ")
		for i, entry := range words {
			tokens = append(tokens, entry)
			if i == len(words)-1 {
				continue
			}
			spacesAfter = append(spacesAfter, len(tokens)-1)
		}
	}
	return tokens, spacesAfter
}

func (splittingTokenizer) Join(tokens []string, state interface{}) string {
	spacesAfter := state.([]int)
	strBuilder := strings.Builder{}
	spacesIdx := 0
	for i, token := range tokens {
		strBuilder.WriteString(token)
		for spacesIdx < len(spacesAfter) && i == spacesAfter[spacesIdx] {
			// One entry for each space following the token
			strBuilder.WriteRune(' ')
			spacesIdx++
		}
	}
	return strBuilder.String()
}
