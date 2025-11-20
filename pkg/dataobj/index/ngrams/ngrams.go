package ngrams

import (
	"bytes"
	"iter"
	"slices"
	"strings"
	"unicode"
)

var AlphabetRunes = []rune("abcdefghijklmnopqrstuvwxyz0123456789_-.() {}")

func NgramIndex(ngram string) int {
	total := 0
	for i, r := range ngram {
		idx := slices.Index(AlphabetRunes, r)
		for range 2 - i {
			idx *= len(AlphabetRunes)
		}
		total += idx
	}
	return total
}

// Sanitize sanitizes a key to be a valid label name
func Sanitize(key string) string {
	if len(key) == 0 {
		return key
	}
	key = strings.TrimSpace(key)
	if len(key) == 0 {
		return key
	}

	key = strings.ToLower(key)

	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_' || (r >= '0' && r <= '9') || r == '-' || r == '.' || r == '(' || r == ')' || r == ' ' {
			return r
		}
		return '_'
	}, key)
}

func BigramOf(s string) iter.Seq[string] {
	return func(yield func(string) bool) {
		for i := 0; i < len(s)-1; i++ {
			if !yield(s[i : i+2]) {
				return
			}
		}
	}
}

func TrigramOf(s string) iter.Seq[string] {
	return func(yield func(string) bool) {
		for i := 0; i < len(s)-2; i++ {
			if !yield(s[i : i+3]) {
				return
			}
		}
	}
}

func FourgramOf(s string) iter.Seq[string] {
	return func(yield func(string) bool) {
		for i := 0; i < len(s)-3; i++ {
			if !yield(s[i : i+4]) {
				return
			}
		}
	}
}

func Tokenize(line []byte) []string {
	tokens := make([]string, 0, len(line))
	start := 0
	for i, r := range bytes.Runes(line) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			continue
		}
		if start < i {
			tokens = append(tokens, string(line[start:i]))
		}
		start = i + 1
	}

	return tokens
}
