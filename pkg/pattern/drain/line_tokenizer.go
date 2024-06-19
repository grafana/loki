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
	tokens := make([]string, len(line))                  // Maximum size is every character is punctuation
	spacesAfter := make([]int, strings.Count(line, " ")) // Could be a bitmap, but it's not worth it for a few bytes.

	start := 0
	nextTokenIdx := 0
	nextSpaceIdx := 0
	for i, char := range line {
		if unicode.IsLetter(char) || unicode.IsNumber(char) || char < 128 && p.excludeDelimiters[char] != 0 {
			continue
		}
		included := char < 128 && p.includeDelimiters[char] != 0
		if char == ' ' || included || unicode.IsPunct(char) {
			if i > start {
				tokens[nextTokenIdx] = line[start:i]
				nextTokenIdx++
			}
			if char == ' ' {
				spacesAfter[nextSpaceIdx] = nextTokenIdx - 1
				nextSpaceIdx++
			} else {
				tokens[nextTokenIdx] = line[i : i+1]
				nextTokenIdx++
			}
			start = i + 1
		}
	}

	if start < len(line) {
		tokens[nextTokenIdx] = line[start:]
		nextTokenIdx++
	}

	return tokens[:nextTokenIdx], spacesAfter[:nextSpaceIdx]
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
