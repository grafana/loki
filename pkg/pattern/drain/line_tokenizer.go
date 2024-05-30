package drain

import "strings"

type LineTokenizer interface {
	Tokenize(line string) []string
	Join(tokens []string) string
}

type spacesTokenizer struct{}

func (spacesTokenizer) Tokenize(line string) []string {
	return strings.Split(line, " ")
}

func (spacesTokenizer) Join(tokens []string) string {
	return strings.Join(tokens, " ")
}

type splittingTokenizer struct{}

func (splittingTokenizer) Tokenize(line string) []string {
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
	for _, token := range strings.SplitAfter(line, keyvalSeparator) {
		tokens = append(tokens, strings.Split(token, " ")...)
	}
	return tokens
}

func (splittingTokenizer) Join(tokens []string) string {
	var builder strings.Builder
	for _, token := range tokens {
		if strings.HasSuffix(token, "=") || strings.HasSuffix(token, ":") {
			builder.WriteString(token)
		} else {
			builder.WriteString(token + " ")
		}
	}
	output := builder.String()
	if output[len(output)-1] == ' ' {
		return output[:len(output)-1]
	}
	return output
}
