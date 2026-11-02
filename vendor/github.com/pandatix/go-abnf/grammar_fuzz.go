package goabnf

import (
	"testing"
)

func FuzzParseABNF(f *testing.F) {
	f.Fuzz(func(t *testing.T, input []byte) {
		grammar, err := ParseABNF(input)

		if err != nil {
			if grammar != nil {
				t.Fatal("Expected no path when error")
			}
			if err, ok := err.(*ErrMultipleSolutionsFound); ok {
				t.Fatalf("For input %s, got error %s", input, err)
			}
			return
		}
		if grammar == nil {
			t.Fatal("Expected a grammar when no error")
		}
	})
}

func FuzzGeneratedABNF(f *testing.F) {
	f.Fuzz(func(t *testing.T, seed int64) {
		input, _ := ABNF.Generate(seed, "rulelist")

		g, err := ParseABNF(input, WithValidation(false))

		if err != nil {
			if g != nil {
				t.Fatal("Expected no path when error")
			}
			if err, ok := err.(*ErrMultipleSolutionsFound); ok {
				t.Fatalf("For input %s, got error %s", input, err)
			}
			return
		}
		if g == nil {
			t.Fatal("Expected a grammar when no error")
			return
		}
	})
}
