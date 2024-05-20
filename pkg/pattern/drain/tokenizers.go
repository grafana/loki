package drain

import (
	"bytes"
	"strings"
	"unsafe"

	"github.com/go-logfmt/logfmt"

	"github.com/grafana/loki/v3/pkg/pattern/tokenization"
)

type Tokenizer interface {
	Marshal([]byte) [][]byte
	Unmarshal([]string) string
}

// Tokenizers using the Adaptive logs tokenizer and replacer.
// This tokenizes multi-word quotes as single tokens.
// It replaces as much as possible with constants before passing to Drain (e.g. <NUM>, <DURATION>, <TIMESTAMP>, <HEX>)
type adaptiveLogsTokenizer struct{}

func (a *adaptiveLogsTokenizer) Marshal(in []byte) [][]byte {
	_ = tokenization.PreprocessAndTokenizeWithOpts(in, tokenization.TokenizerOpts{
		UseSingleTokenForQuotes:   true,
		IncludeDelimitersInTokens: true,
		PreprocessNumbers:         false,
	})
	return [][]byte{}
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
	bytesReader          *bytes.Reader
	bufs                 [][]byte
}

func NewLogFmtTokenizer(tokenizeInsideQuotes bool) *logfmtTokenizer {
	return &logfmtTokenizer{
		tokenizeInsideQuotes: tokenizeInsideQuotes,
		bytesReader:          bytes.NewReader(nil),
		bufs:                 make([][]byte, 0, 128),
	}

}

func unsafeBytesToStrings(in [][]byte) []string {
	return unsafe.Slice((*string)(unsafe.Pointer(&in[0])), len(in))
}

func safeBytesToStrings(in [][]byte) []string {
	output := make([]string, len(in))
	for i, byteSlice := range in {
		output[i] = string(byteSlice)
	}
	return output
}

// Result is only valid until the next call to marshal?
func (a *logfmtTokenizer) Marshal(in []byte) [][]byte {
	tokens := a.bufs                                       //make([][]byte, 0, len(in)/4)                 // Guesstimate of at least 4 characters per token
	processed := tokenization.Preprocess(in, false, false) // Returns a new byte buffer after tokenisation
	a.bytesReader.Reset(processed)
	decoder := logfmt.NewDecoder(a.bytesReader)

	for decoder.ScanRecord() {
		for decoder.ScanKeyval() {
			k := decoder.Key()
			v := decoder.Value()
			if v == nil {
				tokens = append(tokens, k)
				continue
			}
			tokens = append(tokens, append(k, byte('=')))

			spaces := bytes.Count(v, []byte(" "))
			if a.tokenizeInsideQuotes && spaces > 0 {
				/*				start := 0
								for i := 0; i < spaces; i++ {
									next := bytes.In(v[start:], ' ')
									tokens = append(tokens, v[start:start+next])
									start += next + 1
								}
								tokens = append(tokens, v[start:])*/
				tokens = append(tokens, bytes.Split(v, []byte(" "))...)
			} else {
				tokens = append(tokens, v)
			}
		}
	}

	// Translate the [][]byte to []string without reallocating
	// This is safe because we only treat this as a byte slice within this function
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
				output.WriteString("\"")
				output.WriteString(quotedOutput[:len(quotedOutput)-1]) // Drop the trailing space
				output.WriteString("\" ")
			} else {
				output.WriteString(quotedOutput)
			}
			quoted.Reset()
			quotedTokens = 0
			output.WriteString(token)
		} else {
			quoted.WriteString(token)
			quoted.WriteString(" ")
			quotedTokens++
		}
	}
	quotedOutput := quoted.String()
	if quotedTokens > 1 {
		output.WriteString("\"")
		output.WriteString(quotedOutput[:len(quotedOutput)-1]) // Drop the trailing space
		output.WriteString("\"")
	} else {
		output.WriteString(quotedOutput)
	}
	return output.String()
}
