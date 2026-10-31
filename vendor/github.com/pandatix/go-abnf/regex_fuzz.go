package goabnf

import (
	"regexp"
	"testing"
)

func FuzzRegex(f *testing.F) {
	f.Fuzz(func(t *testing.T, seed int64, rulename string) {
		input, _ := ABNF.Generate(seed, "rulelist")
		g, err := ParseABNF(input)
		if err != nil {
			return
		}

		raw, err := g.Regex(rulename)
		if err != nil {
			if _, ok := err.(*ErrRuleNotFound); ok {
				return
			}
			if err == ErrHandlingProseVal {
				return
			}
			t.Fatalf("was not expecting the regex build to come up with an error, got: %s", err)
			return
		}

		if _, err = regexp.Compile(raw); err != nil {
			t.Fatalf("regex should be compilable, at least for the official Go implementation, got: %s", err)
			return
		}
	})
}
