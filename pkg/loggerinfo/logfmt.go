package loggerinfo

import (
	"regexp"
	"strings"

	"github.com/go-logfmt/logfmt"
)

var logfmtRegex = regexp.MustCompile(`^(\w+=("[^"]*"|\S+)(\s+\w+=("[^"]*"|\S+))*)$`)

func IsLogFmt(s string) bool { return logfmtRegex.Match([]byte(s)) }

func TokenizeLogFmt(s string, keys ...string) []string {
	var tokens []string
	dec := logfmt.NewDecoder(strings.NewReader(s))
	for dec.ScanRecord() {
		for dec.ScanKeyval() {
			k := dec.Key()
			v := dec.Value()
			if shouldMark(k, keys) {
				v = markToken(v) // Preserve _key_fields_
			}
			k = markToken(k) // Preserve all keys.
			tokens = append(tokens, string(k), string(v))
		}
	}
	return tokens
}

func LogFmtPattern(tokens []string) string {
	restoreTokens(tokens)
	if len(tokens)%2 != 0 {
		// Just in case.
		tokens = append(tokens, "")
	}
	var b strings.Builder
	enc := logfmt.NewEncoder(&b)
	for i := 1; i < len(tokens); i += 2 {
		if err := enc.EncodeKeyval(tokens[i-1], tokens[i]); err != nil {
			panic(err)
		}
	}
	return b.String()
}

func markToken(t []byte) []byte {
	b := make([]byte, len(t)+1)
	copy(b[1:], t)
	return b
}

func restoreTokens(s []string) {
	for i := range s {
		b := []byte(s[i])
		if len(b) > 0 && b[0] == 0 {
			b = b[1:]
			s[i] = string(b)
		}
	}
}

func shouldMark(k []byte, keys []string) bool {
	x := string(k)
	for _, key := range keys {
		if x == key {
			return true
		}
	}
	return false
}
