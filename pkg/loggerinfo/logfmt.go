package loggerinfo

import (
	"regexp"
	"strings"

	"github.com/go-logfmt/logfmt"
)

var logfmtRegex = regexp.MustCompile(`^(\w+=("[^"]*"|\S+)(\s+\w+=("[^"]*"|\S+))*)$`)

func IsLogFmt(s string) bool { return logfmtRegex.Match([]byte(s)) }

func TokenizeLogFmt(s string) []string {
	var tokens []string
	dec := logfmt.NewDecoder(strings.NewReader(s))
	for dec.ScanRecord() {
		for dec.ScanKeyval() {
			tokens = append(tokens, string(dec.Key()), string(dec.Value()))
		}
	}
	return tokens
}

func LogFmtPattern(tokens []string) string {
	if len(tokens)%2 != 0 {
		return strings.Join(tokens, " ")
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
