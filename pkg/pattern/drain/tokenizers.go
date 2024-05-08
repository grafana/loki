package drain

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/go-logfmt/logfmt"

	"github.com/grafana/loki/v3/pkg/pattern/tokenization"
)

type Tokenizer interface {
	Marshal(string) []string
	Unmarshal([]string) string
}

// Tokenizers using the Adaptive logs tokenizer and replacer.
// This tokenizes multi-word quotes as single tokens.
// It replaces as much as possible with constants before passing to Drain (e.g. <NUM>, <DURATION>, <TIMESTAMP>, <HEX>)
type adaptiveLogsTokenizer struct{}

func (a *adaptiveLogsTokenizer) Marshal(in string) []string {
	preprocessedTokens := tokenization.PreprocessAndTokenizeWithOpts([]byte(in), tokenization.TokenizerOpts{
		UseSingleTokenForQuotes:   true,
		IncludeDelimitersInTokens: true,
		PreprocessNumbers:         false,
	})
	return preprocessedTokens
}

func (a *adaptiveLogsTokenizer) Unmarshal(tokens []string) string {
	// Not easy to unmarshal back to a pattern string for the UI: This works some of the time.
	return strings.Join(tokens, "")
}

// A tokenizer which tweaks the Adaptive one above to make it more suitable for Explore app.
// It tokenizes inside quotes, includes delimiters in the response in order to reconstruct the string, and ignores a few more generic replacement placeholders (e.g. does not use <NUM> or <HEX>)
type limitedReplacementsTokenizer struct{}

func (a *limitedReplacementsTokenizer) Marshal(in string) []string {
	preprocessedTokens := tokenization.PreprocessAndTokenizeWithOpts([]byte(in), tokenization.TokenizerOpts{
		UseSingleTokenForQuotes:   false,
		IncludeDelimitersInTokens: true,
		PreprocessNumbers:         false,
	})
	return preprocessedTokens
}

func (a *limitedReplacementsTokenizer) Unmarshal(tokens []string) string {
	return strings.Join(tokens, "")
}

// Preprocesses the string to replace some terms with placeholders, except a few common ones (e.g. does not use <NUM> or <HEX>)
// Uses a simple approach to tokenizing: Use space or comma, depending on which is more frequently used in this log line.
type spaceOrCommaTokenizer struct {
	delimiter string
}

func (a *spaceOrCommaTokenizer) Marshal(in string) []string {
	preprocessedContent := string(tokenization.Preprocess([]byte(in), false, false))

	if a.delimiter == "" {
		spaces := strings.Count(preprocessedContent, " ")
		commas := strings.Count(preprocessedContent, ",")
		a.delimiter = " "
		if commas > spaces {
			a.delimiter = ","
		}
	}

	return strings.Split(preprocessedContent, a.delimiter)
}
func (a *spaceOrCommaTokenizer) Unmarshal(tokens []string) string {
	return strings.Join(tokens, a.delimiter)
}

type logfmtTokenizer struct {
	tokenizeInsideQuotes bool
}

func (a *logfmtTokenizer) Marshal(in string) []string {
	tokens := []string{}
	processed := tokenization.Preprocess([]byte(in), false, false)
	decoder := logfmt.NewDecoder(bytes.NewReader(processed))
	for decoder.ScanRecord() {
		for decoder.ScanKeyval() {
			k := decoder.Key()
			v := decoder.Value()
			equals := "="
			if v == nil {
				equals = ""
			}
			tokens = append(tokens, fmt.Sprintf("%s%s", k, equals))
			if v == nil {
				continue
			}
			if a.tokenizeInsideQuotes {
				tokens = append(tokens, strings.Split(string(v), " ")...)
			} else {
				tokens = append(tokens, string(v))
			}
		}
	}

	return tokens
}

func (a *logfmtTokenizer) Unmarshal(tokens []string) string {
	var output strings.Builder
	var quoted strings.Builder
	var quotedTokens int

	for _, token := range tokens {
		if strings.HasSuffix(token, "=") {
			quotedOutput := quoted.String()
			if quotedTokens > 1 {
				output.WriteString(fmt.Sprintf("%q ", strings.TrimSuffix(quotedOutput, " ")))
			} else {
				output.WriteString(fmt.Sprintf("%s", quotedOutput))
			}
			quoted.Reset()
			quotedTokens = 0
			output.WriteString(token)
		} else {
			quoted.WriteString(token + " ")
			quotedTokens++
		}
	}
	quotedOutput := strings.TrimSuffix(quoted.String(), " ")
	if quotedTokens > 1 {
		output.WriteString(fmt.Sprintf("%q", strings.TrimSuffix(quotedOutput, " ")))
	} else {
		output.WriteString(fmt.Sprintf("%s", quotedOutput))
	}
	return output.String()
}
