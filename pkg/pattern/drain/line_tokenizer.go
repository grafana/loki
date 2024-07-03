package drain

import (
	"bytes"
	"fmt"
	"strings"
	"unicode"

	"github.com/buger/jsonparser"
	gologfmt "github.com/go-logfmt/logfmt"
	"github.com/grafana/loki/v3/pkg/logql/log/logfmt"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/util"
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

type logfmtTokenizer struct {
	dec        *logfmt.Decoder
	varReplace string
}

func newLogfmtTokenizer(varReplace string) *logfmtTokenizer {
	return &logfmtTokenizer{
		dec:        logfmt.NewDecoder(nil),
		varReplace: varReplace,
	}
}

func (t *logfmtTokenizer) Tokenize(line string) ([]string, interface{}) {
	var tokens []string
	t.dec.Reset([]byte(line))
	for !t.dec.EOL() && t.dec.ScanKeyval() {
		key := t.dec.Key()
		if isVariableField(key) {
			tokens = append(tokens, string(t.dec.Key()), t.varReplace)

			continue
		}
		tokens = append(tokens, string(t.dec.Key()), string(t.dec.Value()))
	}
	if t.dec.Err() != nil {
		return nil, nil
	}
	return tokens, nil
}

func (t *logfmtTokenizer) Join(tokens []string, state interface{}) string {
	if len(tokens) == 0 {
		return ""
	}
	if len(tokens)%2 == 1 {
		tokens = append(tokens, "")
	}
	buf := bytes.NewBuffer(make([]byte, 0, 1024))
	enc := gologfmt.NewEncoder(buf)
	for i := 0; i < len(tokens); i += 2 {
		k, v := tokens[i], tokens[i+1]
		if err := enc.EncodeKeyval(k, v); err != nil {
			return ""
		}
	}
	return buf.String()
}

type jsonTokenizer struct {
	*punctuationTokenizer
	varReplace string
}

func newJSONTokenizer(varReplace string) *jsonTokenizer {
	return &jsonTokenizer{newPunctuationTokenizer(), varReplace}
}

func (t *jsonTokenizer) Tokenize(line string) ([]string, interface{}) {
	var found []byte
	for _, key := range []string{"log", "message", "msg", "msg_", "_msg", "content"} {
		msg, ty, _, err := jsonparser.Get(util.GetUnsafeBytes(line), key)
		if err == nil && ty == jsonparser.String {
			found = msg
			break
		}
	}

	if found == nil {
		return nil, nil
	}
	tokens, state := t.punctuationTokenizer.Tokenize(util.GetUnsafeString(found))
	return tokens, state
}

func (t *jsonTokenizer) Join(tokens []string, state interface{}) string {
	return fmt.Sprintf("%s%s%s", t.varReplace, t.punctuationTokenizer.Join(tokens, state), t.varReplace)
}

func isVariableField(key []byte) bool {
	return bytes.EqualFold(key, []byte("ts")) ||
		bytes.EqualFold(key, []byte("traceID")) ||
		bytes.EqualFold(key, []byte("time")) ||
		bytes.EqualFold(key, []byte("timestamp"))
}
