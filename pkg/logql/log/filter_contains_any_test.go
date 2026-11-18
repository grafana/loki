package log

import (
	"bytes"
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"github.com/grafana/regexp"
	"github.com/stretchr/testify/require"
)

func TestContainsAnyFilter(t *testing.T) {
	t.Run("Filter keeps the same lines as bytes.Contains for each literal on random input", func(t *testing.T) {
		rnd := rand.New(rand.NewSource(1))
		// A small alphabet makes literals overlap, share prefixes, and end inside each other.
		const alphabet = "abc"
		randomString := func(minLen, maxLen int) []byte {
			b := make([]byte, minLen+rnd.Intn(maxLen-minLen+1))
			for i := range b {
				b[i] = alphabet[rnd.Intn(len(alphabet))]
			}
			return b
		}

		for trial := range 2000 {
			literals := make([][]byte, 1+rnd.Intn(8))
			for i := range literals {
				literals[i] = randomString(1, 4)
			}
			f := ahoCorasickMatcher{}.newFilter(literals)

			for range 20 {
				line := randomString(0, 12)
				want := false
				for _, literal := range literals {
					want = want || bytes.Contains(line, literal)
				}
				require.Equal(t, want, f.Filter(line), "trial %d, literals %q, line %q", trial, literals, line)
			}
		}
	})

	t.Run("a case-insensitive filter keeps the same lines as containsLower for each literal on random input", func(t *testing.T) {
		rnd := rand.New(rand.NewSource(1))
		// The line pieces cover both ASCII cases, the two runes that lower to ASCII (U+0130 and
		// U+212A), runes that fold to themselves, and bytes that are not valid UTF-8.
		pieces := []string{"a", "i", "k", "A", "I", "K", "\u0130", "\u212A", "\u0131", "\u017F", "\u00E9", "\xE2", "\x84"}
		const literalAlphabet = "aik"

		for trial := range 3000 {
			literals := make([][]byte, 1+rnd.Intn(5))
			for i := range literals {
				literal := make([]byte, 1+rnd.Intn(4))
				for j := range literal {
					literal[j] = literalAlphabet[rnd.Intn(len(literalAlphabet))]
				}
				literals[i] = literal
			}
			f := newCaseInsensitiveContainsAnyFilter(literals)

			for range 20 {
				var line []byte
				for range rnd.Intn(8) {
					line = append(line, pieces[rnd.Intn(len(pieces))]...)
				}
				want := false
				for _, literal := range literals {
					want = want || containsLower(line, literal)
				}
				require.Equal(t, want, f.Filter(line), "trial %d, literals %q, line %q", trial, literals, line)
			}
		}
	})

	t.Run("FilterBatch keeps the same lines as Filter for merged and negated merged filters", func(t *testing.T) {
		enableMultiLiteralFilter(t, CaseSensitiveMatcherAhoCorasick)
		rnd := rand.New(rand.NewSource(1))
		const alphabet = "abA "
		randomString := func(minLen, maxLen int) string {
			var sb strings.Builder
			for range minLen + rnd.Intn(maxLen-minLen+1) {
				sb.WriteByte(alphabet[rnd.Intn(len(alphabet))])
			}
			return sb.String()
		}

		for trial := range 2000 {
			legs := make([]Filterer, minContainsAnyLiterals+rnd.Intn(4))
			for i := range legs {
				legs[i] = newContainsFilter([]byte(randomString(1, 3)), rnd.Intn(4) == 0)
			}
			var f Filterer = NewOrFilters(legs)
			if rnd.Intn(2) == 0 {
				f = NewNotFilter(WrapFilterer(f))
			}

			rows := make([]string, rnd.Intn(40))
			for i := range rows {
				rows[i] = randomString(0, 8)
			}
			batch := newTestLinesBatch(rows...)

			require.Equal(t, filterEachLine(f, batch), FilterBatch(f, batch, nil), "trial %d, rows %q", trial, rows)
		}
	})

	t.Run("Matches returns true when the checker accepts any literal", func(t *testing.T) {
		f := ahoCorasickMatcher{}.newFilter([][]byte{[]byte("foo"), []byte("bar")})

		require.True(t, f.Matches(literalChecker{literal: "bar"}))
		require.False(t, f.Matches(literalChecker{literal: "baz"}))
	})

	t.Run("Matches asks the checker for a case-insensitive test of a case-insensitive filter", func(t *testing.T) {
		f := newCaseInsensitiveContainsAnyFilter([][]byte{[]byte("foo"), []byte("bar"), []byte("baz")})

		require.True(t, f.Matches(literalChecker{literal: "bar", caseInsensitive: true}))
		require.False(t, f.Matches(literalChecker{literal: "bar"}))
	})

	t.Run("ToStage keeps the lines that Filter keeps", func(t *testing.T) {
		f := ahoCorasickMatcher{}.newFilter([][]byte{[]byte("foo"), []byte("bar")})
		stage := f.ToStage()

		for _, line := range []string{"a foo", "bar", "baz", ""} {
			_, ok := stage.Process(0, []byte(line), nil)
			require.Equal(t, f.Filter([]byte(line)), ok, line)
		}
	})
}

func TestMergeContainsAny(t *testing.T) {
	enableMultiLiteralFilter(t, CaseSensitiveMatcherAhoCorasick)
	literals := func(n int) [][]byte {
		out := make([][]byte, n)
		for i := range out {
			out[i] = fmt.Appendf(nil, "lit%d", i)
		}
		return out
	}
	chain := func(legs ...MatcherFilterer) MatcherFilterer {
		var or MatcherFilterer
		for _, leg := range legs {
			or = ChainOrMatcherFilterer(or, leg)
		}
		return or
	}

	t.Run("returns the filter unchanged when it has fewer literals than the threshold", func(t *testing.T) {
		f := chain(containsFilters(literals(minContainsAnyLiterals - 1))...)

		require.Equal(t, f, mergeContainsAny(f))
	})

	t.Run("returns a filter that is not an OR unchanged", func(t *testing.T) {
		f := newContainsFilter([]byte("foo"), false)

		require.Equal(t, f, mergeContainsAny(f))
	})

	t.Run("replaces an OR of case-sensitive literals with one containsAnyFilter", func(t *testing.T) {
		got := mergeContainsAny(chain(containsFilters(literals(minContainsAnyLiterals))...))

		merged, ok := got.(containsAnyFilter)
		require.True(t, ok, "got %T", got)
		require.Len(t, merged.literals, minContainsAnyLiterals)
	})

	t.Run("keeps case-insensitive and regex legs after the merged literals", func(t *testing.T) {
		caseInsensitive := newContainsFilter([]byte("Foo"), true)
		re, err := newRegexpFilter("b.r", "b.r", true)
		require.NoError(t, err)
		merged := literals(minContainsAnyLiterals)
		legs := append(containsFilters(merged), caseInsensitive, re)

		got := mergeContainsAny(chain(legs...))

		require.Equal(t, newOrFilter(newOrFilter(ahoCorasickMatcher{}.newFilter(merged), caseInsensitive), re), got)
	})

	t.Run("unwraps wrapped legs and keeps a nested containsAnyFilter as a leg", func(t *testing.T) {
		merged := literals(minContainsAnyLiterals)
		nested := ahoCorasickMatcher{}.newFilter([][]byte{[]byte("nested")})
		var legs []MatcherFilterer
		for _, f := range containsFilters(merged) {
			legs = append(legs, WrapFilterer(f))
		}
		legs = append(legs, WrapFilterer(nested))

		got := mergeContainsAny(chain(legs...))

		require.Equal(t, newOrFilter(ahoCorasickMatcher{}.newFilter(merged), nested), got)
	})

	t.Run("returns the filter unchanged when a literal is longer than maxContainsAnyLiteralLen", func(t *testing.T) {
		long := append(literals(minContainsAnyLiterals-1), bytes.Repeat([]byte("a"), maxContainsAnyLiteralLen+1))
		f := chain(containsFilters(long)...)

		require.Equal(t, f, mergeContainsAny(f))
	})

	t.Run("returns the filter unchanged when the literals hold more than maxContainsAnyBytes", func(t *testing.T) {
		var many [][]byte
		for total := 0; total <= maxContainsAnyBytes; total += maxContainsAnyLiteralLen {
			many = append(many, fmt.Appendf(nil, "%0*d", maxContainsAnyLiteralLen, len(many)))
		}
		f := chain(containsFilters(many)...)

		require.Equal(t, f, mergeContainsAny(f))
	})

	t.Run("replaces an OR of case-insensitive ASCII literals with one case-insensitive containsAnyFilter", func(t *testing.T) {
		var legs []MatcherFilterer
		for _, literal := range literals(minCaseInsensitiveContainsAnyLiterals) {
			legs = append(legs, newContainsFilter(literal, true))
		}

		got := mergeContainsAny(chain(legs...))

		require.Equal(t, newCaseInsensitiveContainsAnyFilter(literals(minCaseInsensitiveContainsAnyLiterals)), got)
	})

	t.Run("merges case-sensitive and case-insensitive literals into separate filters", func(t *testing.T) {
		caseSensitive := literals(minContainsAnyLiterals)
		caseInsensitive := [][]byte{[]byte("panic"), []byte("fatal"), []byte("error")}
		legs := containsFilters(caseSensitive)
		for _, literal := range caseInsensitive {
			legs = append(legs, newContainsFilter(literal, true))
		}

		got := mergeContainsAny(chain(legs...))

		require.Equal(t, newOrFilter(ahoCorasickMatcher{}.newFilter(caseSensitive), newCaseInsensitiveContainsAnyFilter(caseInsensitive)), got)
	})

	t.Run("keeps a non-ASCII case-insensitive literal as a leg", func(t *testing.T) {
		nonASCII := newContainsFilter([]byte("\u00E9t\u00E9"), true)
		merged := [][]byte{[]byte("panic"), []byte("fatal"), []byte("error")}
		var legs []MatcherFilterer
		for _, literal := range merged {
			legs = append(legs, newContainsFilter(literal, true))
		}
		legs = append(legs, nonASCII)

		got := mergeContainsAny(chain(legs...))

		require.Equal(t, newOrFilter(newCaseInsensitiveContainsAnyFilter(merged), nonASCII), got)
	})

	t.Run("returns the filter unchanged when a case-insensitive literal has more than maxFoldedLetters folded letters", func(t *testing.T) {
		manyFolded := append([]byte("a"), bytes.Repeat([]byte("k"), maxFoldedLetters+1)...)
		legs := []MatcherFilterer{
			newContainsFilter(manyFolded, true),
			newContainsFilter([]byte("panic"), true),
			newContainsFilter([]byte("fatal"), true),
		}
		f := chain(legs...)

		require.Equal(t, f, mergeContainsAny(f))
	})

	t.Run("returns the filter unchanged when the multi-literal filter is disabled", func(t *testing.T) {
		setMultiLiteralFilter(t, MultiLiteralFilterConfig{})
		f := chain(containsFilters(literals(minContainsAnyLiterals))...)

		require.Equal(t, f, mergeContainsAny(f))
	})

	t.Run("uses a literalDFA for case-sensitive literals when the dfa matcher is set", func(t *testing.T) {
		enableMultiLiteralFilter(t, CaseSensitiveMatcherDFA)
		merged := literals(minContainsAnyLiterals)

		got := mergeContainsAny(chain(containsFilters(merged)...))

		require.Equal(t, dfaMatcher{}.newFilter(merged), got)
	})

	t.Run("merges literals above the aho-corasick limits when the dfa matcher is set", func(t *testing.T) {
		enableMultiLiteralFilter(t, CaseSensitiveMatcherDFA)
		long := append(literals(minContainsAnyLiterals-1), bytes.Repeat([]byte("a"), maxContainsAnyLiteralLen+1))

		got := mergeContainsAny(chain(containsFilters(long)...))

		require.Equal(t, dfaMatcher{}.newFilter(long), got)
	})

	t.Run("returns the filter unchanged when the literals hold more than maxLiteralDFABytes and the dfa matcher is set", func(t *testing.T) {
		enableMultiLiteralFilter(t, CaseSensitiveMatcherDFA)
		var many [][]byte
		for total := 0; total <= maxLiteralDFABytes; total += 1024 {
			many = append(many, fmt.Appendf(nil, "%01024d", len(many)))
		}
		f := chain(containsFilters(many)...)

		require.Equal(t, f, mergeContainsAny(f))
	})

	t.Run("returns an OR of equal filters unchanged", func(t *testing.T) {
		var legs []MatcherFilterer
		for _, literal := range literals(minContainsAnyLiterals) {
			legs = append(legs, newEqualFilter(literal, false))
		}
		f := chain(legs...)

		require.Equal(t, f, mergeContainsAny(f))
	})
}

func TestNewOrFilters(t *testing.T) {
	enableMultiLiteralFilter(t, CaseSensitiveMatcherAhoCorasick)
	t.Run("returns nil for no filters", func(t *testing.T) {
		require.Nil(t, NewOrFilters(nil))
	})

	t.Run("drops an empty literal leg when it merges the literals", func(t *testing.T) {
		filters := []Filterer{newContainsFilter(nil, false)}
		for i := range minContainsAnyLiterals {
			filters = append(filters, newContainsFilter(fmt.Appendf(nil, "lit%d", i), false))
		}

		f := NewOrFilters(filters)

		_, ok := f.(containsAnyFilter)
		require.True(t, ok, "got %T", f)
		require.False(t, f.Filter([]byte("no literal here")))
		require.True(t, f.Filter([]byte("has lit3")))
	})
}

func TestSetMultiLiteralFilter(t *testing.T) {
	t.Run("returns an error for an unknown case-sensitive matcher and keeps the previous setting", func(t *testing.T) {
		enableMultiLiteralFilter(t, CaseSensitiveMatcherDFA)

		err := SetMultiLiteralFilter(MultiLiteralFilterConfig{Enabled: true, CaseSensitiveMatcher: "unknown"})

		require.ErrorContains(t, err, "unknown")
		merger := activeMerger.Load()
		require.NotNil(t, merger)
		require.Equal(t, CaseSensitiveMatcherDFA, merger.caseSensitive.name())
	})

	t.Run("disables merging when Enabled is false", func(t *testing.T) {
		enableMultiLiteralFilter(t, CaseSensitiveMatcherAhoCorasick)

		setMultiLiteralFilter(t, MultiLiteralFilterConfig{CaseSensitiveMatcher: CaseSensitiveMatcherDFA})

		require.Nil(t, activeMerger.Load())
	})

	t.Run("selects the case-sensitive matcher from the config", func(t *testing.T) {
		for _, name := range []string{CaseSensitiveMatcherAhoCorasick, CaseSensitiveMatcherDFA} {
			enableMultiLiteralFilter(t, name)

			merger := activeMerger.Load()
			require.NotNil(t, merger)
			require.Equal(t, name, merger.caseSensitive.name())
		}
	})
}

func BenchmarkContainsAnyFilter(b *testing.B) {
	rnd := rand.New(rand.NewSource(1))
	const hex = "0123456789abcdef"
	randomHex := func(n int) []byte {
		out := make([]byte, n)
		for i := range out {
			out[i] = hex[rnd.Intn(len(hex))]
		}
		return out
	}

	rows := make([]string, 4096)
	for i := range rows {
		rows[i] = fmt.Sprintf(
			"ts=2026-11-17T10:%02d:%02d.%03dZ level=info caller=handler.go:%d msg=%q status=%d duration=%d.%dms trace_id=%s span_id=%s",
			rnd.Intn(60), rnd.Intn(60), rnd.Intn(1000), rnd.Intn(900)+100, "request served",
			[]int{200, 404, 500}[rnd.Intn(3)], rnd.Intn(100), rnd.Intn(10), randomHex(32), randomHex(16),
		)
	}
	batch := newTestLinesBatch(rows...)

	for _, n := range []int{2, 4, 6, 8, 16, 64, 200} {
		literals := make([][]byte, n)
		for i := range literals {
			literals[i] = randomHex(16)
		}
		var chain MatcherFilterer
		for _, leg := range containsFilters(literals) {
			chain = ChainOrMatcherFilterer(chain, leg)
		}
		filters := map[string]Filterer{
			"or-chain":     chain,
			"contains-any": ahoCorasickMatcher{}.newFilter(literals),
		}

		for name, f := range filters {
			b.Run(fmt.Sprintf("case-sensitive/literals=%d/%s", n, name), func(b *testing.B) {
				dst := make([]int32, 0, len(rows))
				b.SetBytes(int64(len(batch.Data)))
				b.ReportAllocs()
				for b.Loop() {
					dst = FilterBatch(f, batch, dst[:0])
				}
			})
		}
	}

	const letters = "abcdefghijklmnopqrstuvwxyz"
	randomWord := func() []byte {
		out := make([]byte, 5+rnd.Intn(6))
		for i := range out {
			out[i] = letters[rnd.Intn(len(letters))]
		}
		return out
	}
	for _, n := range []int{1, 2, 3, 4, 6, 11} {
		var chain MatcherFilterer
		literals := make([][]byte, n)
		for i := range literals {
			literals[i] = randomWord()
			chain = ChainOrMatcherFilterer(chain, newContainsFilter(literals[i], true))
		}
		filters := map[string]Filterer{
			"or-chain":     chain,
			"contains-any": newCaseInsensitiveContainsAnyFilter(literals),
		}

		for name, f := range filters {
			b.Run(fmt.Sprintf("case-insensitive/literals=%d/%s", n, name), func(b *testing.B) {
				dst := make([]int32, 0, len(rows))
				b.SetBytes(int64(len(batch.Data)))
				b.ReportAllocs()
				for b.Loop() {
					dst = FilterBatch(f, batch, dst[:0])
				}
			})
		}
	}
}

// enableMultiLiteralFilter enables the multi-literal filter with matcher. It restores the previous
// setting when the test ends.
func enableMultiLiteralFilter(t testing.TB, matcher string) {
	t.Helper()
	setMultiLiteralFilter(t, MultiLiteralFilterConfig{Enabled: true, CaseSensitiveMatcher: matcher})
}

// setMultiLiteralFilter applies cfg. It restores the previous setting when the test ends.
func setMultiLiteralFilter(t testing.TB, cfg MultiLiteralFilterConfig) {
	t.Helper()
	previous := activeMerger.Load()
	require.NoError(t, SetMultiLiteralFilter(cfg))
	t.Cleanup(func() { activeMerger.Store(previous) })
}

// containsFilters returns one case-sensitive contains filter for each literal.
func containsFilters(literals [][]byte) []MatcherFilterer {
	out := make([]MatcherFilterer, len(literals))
	for i, literal := range literals {
		out[i] = newContainsFilter(literal, false)
	}
	return out
}

// literalChecker accepts only the literal it holds, as a contains test with the case sensitivity it
// holds.
type literalChecker struct {
	literal         string
	caseInsensitive bool
}

func (c literalChecker) Test(line []byte, caseInsensitive bool, equal bool) bool {
	return caseInsensitive == c.caseInsensitive && !equal && string(line) == c.literal
}

func (c literalChecker) TestRegex(*regexp.Regexp) bool { return false }
