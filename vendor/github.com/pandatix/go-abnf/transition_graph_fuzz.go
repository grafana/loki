package goabnf

import (
	"bytes"
	"testing"
)

func FuzzRawGrammarToTransitionGraph(f *testing.F) {
	f.Fuzz(func(t *testing.T, input []byte, rulename string) {
		if len(input) > 32 || bytes.Count(input, []byte{'('}) > 2 {
			t.Skip()
		}

		fuzzTarget_GrammarToTransitionGraph(t, input, rulename)
	})
}

func FuzzGeneratedGrammarToTransitionGraph(f *testing.F) {
	f.Fuzz(func(t *testing.T, seed int64, rulename string) {
		input, _ := ABNF.Generate(seed, "rulelist")

		fuzzTarget_GrammarToTransitionGraph(t, input, rulename)
	})
}

func fuzzTarget_GrammarToTransitionGraph(t *testing.T, input []byte, rulename string) {
	g, err := ParseABNF(input, WithValidation(false))
	if err != nil {
		t.Skip()
	}

	tg, err := g.TransitionGraph(rulename,
		WithDeflateRules(true),
		WithDeflateNumVals(false),  // don't set to true, else it could easily produce a high number of edges/vertices
		WithDeflateCharVals(false), // don't set to true, else it could easily produce a high number of edges/vertices
		WithRepetitionThreshold(3),
	)
	if err == nil {
		_ = tg.ToMermaid()
	}
}

func FuzzRawGrammarExhaustiveCombinations(f *testing.F) {
	f.Fuzz(func(t *testing.T, input []byte, rulename string) {
		if len(input) > 32 || bytes.Count(input, []byte{'('}) > 2 {
			t.Skip()
		}

		fuzzTarget_ExhaustiveCombinations(t, input, rulename)
	})
}

func FuzzGeneratedGrammarExhaustiveCombinations(f *testing.F) {
	f.Fuzz(func(t *testing.T, seed int64, rulename string) {
		input, _ := ABNF.Generate(seed, "rulelist")

		fuzzTarget_ExhaustiveCombinations(t, input, rulename)
	})
}

func fuzzTarget_ExhaustiveCombinations(t *testing.T, input []byte, rulename string) {
	g, err := ParseABNF(input, WithValidation(false))
	if err != nil {
		t.Skip()
	}

	tg, err := g.TransitionGraph(rulename,
		WithDeflateRules(true),
		WithDeflateNumVals(false),  // don't set to true, else it could easily produce a high number of edges/vertices
		WithDeflateCharVals(false), // don't set to true, else it could easily produce a high number of edges/vertices
		WithRepetitionThreshold(3),
	)
	if err != nil {
		t.Skip()
	}

	tgr := tg.Reader()
	for tgr.Next() {
		tgr.Scan()
	}
}
