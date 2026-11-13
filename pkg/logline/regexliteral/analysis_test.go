package regexliteral

import (
	"strconv"
	"strings"
	"testing"

	"github.com/grafana/regexp/syntax"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logline/needle"
)

// Deterministic syntax and context combinations check exact precision against
// the reference walker, not merely the absence of false negatives.
var analysisPatterns = []string{
	``, `plain`, `.*`, `.+`, `[a-z]+`, `a\bword$`,
	`(a?)(a?)`, `((a?)(a?)){0}`, `(?:.*){0}`, `(?:x*)?`,
	`(?:|ab)(?:ab|)`, `(?:(a)(bc)|(ab)(c))`,
	`(GET|POST) /api`, `(GET|POST|PUT|DELETE) /api/v1/(users|orders)`,
	`prefix_(a|b)_middle_(c|d)_suffix`, `[0-3][0-3][0-3]x`,
	`(q00foo)|(q01foo)|(q02foo)|(q03foo)|(q04foo)|(q05foo)|` +
		`(q06foo)|(q07foo)|(q08foo)|(q09foo)|(q10foo)|(q11foo)|(foo)|(bar)`,
	`foo.*bar|baz.*qux`, `foo|.*`, `foo|`, `a.*xyz|b.*xyz`,
	`(abc|def)+ghi`, `(abc.*def)+ghi`, `(abc.*def){2,3}`, `(ab?){3}`,
	`a{257}`, `a{258}`, `(?:a{256}){2}`, `(?:a{256}){3}`, `(?:a{256}){2,3}`,
	`(?:z|a{257}){2}`, `(?:ab|z){130}`, `(?:a?){8}`,
	`é{129}`, `é{130}`, `é(?:界|)終`, `(?i)café[ks]`, `[aAbB]`, `[KKk]`,
}

func referenceNeedles(re *syntax.Regexp) needle.Expr {
	return needle.And(newReferenceWalker(1).expr(re), newReferenceWalker(maxExact).expr(re))
}

func TestAnalysis_CompiledParity(t *testing.T) {
	maps := []struct {
		name string
		f    func(string) (string, bool)
	}{
		{"identity", func(s string) (string, bool) { return s, true }},
		{"uppercase", func(s string) (string, bool) { return strings.ToUpper(s), true }},
		{"drop short", func(s string) (string, bool) { return strings.ToUpper(s), len(s) >= 4 }},
	}
	for i, atom := range analysisPatterns {
		for j, pattern := range []string{
			atom,
			"L(" + atom + ")R",
			"L((" + atom + ").*tail)R",
			"L((" + atom + ")|other.*tail)R",
			"L((" + atom + ")+)?R",
		} {
			t.Run(strconv.Itoa(i)+"/"+strconv.Itoa(j), func(t *testing.T) {
				re, err := parse(pattern)
				require.NoError(t, err)
				want, got := referenceNeedles(re), Needles(pattern)
				for _, mapping := range maps {
					wm, gm := want.Map(mapping.f), got.Map(mapping.f)
					for _, cap := range []int{1, 2, 3, 4, 8, 16, 64} {
						require.Equal(t, wm.Compile(cap), gm.Compile(cap), "pattern=%q map=%s cap=%d", pattern, mapping.name, cap)
					}
				}
			})
		}
	}
}

func TestAnalysis_ExactParity(t *testing.T) {
	for _, pattern := range analysisPatterns {
		t.Run(pattern, func(t *testing.T) {
			re, err := parse(pattern)
			require.NoError(t, err)
			for _, limit := range []int{1, 3, 4, maxExact} {
				a := analyze(re, limit)
				for node, sets := range a.exact {
					for mode, cap := range []int{1, limit} {
						want, ok := newReferenceWalker(cap).exact(node)
						require.Equal(t, ok, sets[mode] != nil, "pattern=%q node=%s cap=%d", pattern, node, cap)
						if ok {
							require.Equal(t, materializeTextSet(want), materializeTextSet(sets[mode]), "pattern=%q node=%s cap=%d", pattern, node, cap)
						}
					}
				}
			}
		})
	}
}

func TestAnalysis_ExactFailuresAreSticky(t *testing.T) {
	re, err := parse(`((a?)(a?)){0}`)
	require.NoError(t, err)
	for _, limit := range []int{3, 4, maxExact} {
		a := analyze(re, limit)
		require.Nil(t, a.exact[re][basicMode])
		if limit == 3 {
			// Four intermediate combinations fail before deduplication;
			// neither a three-string result nor zero copies recovers exactness.
			require.Nil(t, a.exact[re][richMode])
		} else {
			set := a.exact[re][richMode]
			require.Len(t, set, 1)
			require.Nil(t, set[0])
		}
	}
}

func TestAnalysis_IgnoredSubtrees(t *testing.T) {
	for _, pattern := range []string{
		`((large|optional).*body)*`,
		`((large|optional).*body){0,2}`,
	} {
		t.Run(pattern, func(t *testing.T) {
			re, err := parse(pattern)
			require.NoError(t, err)
			a := analyze(re, maxExact)
			require.Len(t, a.exact, 1)
			require.Empty(t, a.expr(re, basicMode).Compile(1))
			require.Empty(t, a.expr(re, richMode).Compile(1))
		})
	}
}

func TestAnalysis_OptionalNonExactHeadShortCircuits(t *testing.T) {
	for _, captures := range []int{64, 1000} {
		t.Run(strconv.Itoa(captures), func(t *testing.T) {
			pattern := `(?:.+` + strings.Repeat(`(a{256})`, captures) + `)?`
			re, err := parse(pattern)
			require.NoError(t, err)
			require.Equal(t, syntax.OpQuest, re.Op)
			body := re.Sub[0]
			require.Equal(t, syntax.OpConcat, body.Op)
			a := analyze(re, maxExact)
			// Only the optional node, its concat, and the non-exact plus
			// head are needed. Neither exact analysis nor emission should
			// visit or build ropes for any of the trailing captures.
			require.Equal(t, 3, len(a.exact))
			_, cached := a.exact[body.Sub[1]]
			require.False(t, cached)
			for _, mode := range []int{basicMode, richMode} {
				require.Empty(t, a.expr(re, mode).Compile(1))
				require.Equal(t, 3, len(a.exact))
			}
			require.Empty(t, Needles(pattern).Compile(2))
		})
	}
}

func TestAnalysis_EmissionAnalyzesSkippedChildren(t *testing.T) {
	re, err := parse(`head.+tail`)
	require.NoError(t, err)
	require.Equal(t, syntax.OpConcat, re.Op)
	require.Len(t, re.Sub, 3)
	a := analyze(re, maxExact)
	_, cached := a.exact[re.Sub[2]]
	require.False(t, cached)
	_, cached = a.exact[re.Sub[1].Sub[0]]
	require.False(t, cached)

	want := needle.Conjunctions{{"head", "tail"}}
	require.Equal(t, want, a.expr(re, basicMode).Compile(1))
	_, cached = a.exact[re.Sub[2]]
	require.True(t, cached)
	_, cached = a.exact[re.Sub[1].Sub[0]]
	require.True(t, cached)
	entries := len(a.exact)
	// The second mode reuses summaries populated by fallback emission.
	require.Equal(t, want, a.expr(re, richMode).Compile(1))
	require.Len(t, a.exact, entries)
}

func TestAnalysis_SharedSubtreeVisitedOnce(t *testing.T) {
	// A small DAG makes repeated analysis observable without timing or
	// allocation thresholds: unfolding both paths would require billions
	// of visits. Only analysis is exercised; no output text is expanded.
	re := &syntax.Regexp{Op: syntax.OpEmptyMatch}
	for range 32 {
		re = &syntax.Regexp{Op: syntax.OpConcat, Sub: []*syntax.Regexp{re, re}}
	}
	a := analyze(re, maxExact)
	require.Len(t, a.exact, 33)
	for _, set := range a.exact[re] {
		require.Len(t, set, 1)
		require.Nil(t, set[0])
	}
}

func TestAnalysis_NestedNonExact(t *testing.T) {
	for _, tc := range []struct {
		pattern string
		want    needle.Conjunctions
	}{
		{`L((foo.*bar))R`, needle.Conjunctions{{"Lfoo", "barR"}}},
		{`L((foo.*bar)+)R`, needle.Conjunctions{{"L", "R", "bar", "foo"}}},
		{`L((foo.*bar){2,3})R`, needle.Conjunctions{{"L", "R", "bar", "foo"}}},
		{`L((foo.*bar)?)R`, needle.Conjunctions{{"L", "R"}}},
		// Even zero copies must retain the child's exact failure: do
		// not infer an empty exact set and accidentally join L to R.
		{`L((?:.*){0})R`, needle.Conjunctions{{"L", "R"}}},
	} {
		t.Run(tc.pattern, func(t *testing.T) {
			require.Equal(t, tc.want, Needles(tc.pattern).Compile(64))
		})
	}

	for _, depth := range []int{32, 128, 512} {
		t.Run(strconv.Itoa(depth), func(t *testing.T) {
			pattern := "L" + strings.Repeat("(", depth) + "foo.*bar" + strings.Repeat(")", depth) + "R"
			re, err := parse(pattern)
			require.NoError(t, err)
			a := analyze(re, maxExact)
			// Each capture has one cached failure, not another recursive
			// exact attempt during each enclosing fallback walk.
			require.GreaterOrEqual(t, len(a.exact), depth)
			require.Equal(t, needle.Conjunctions{{"Lfoo", "barR"}}, Needles(pattern).Compile(64))
			require.Equal(t, referenceNeedles(re).Compile(64), Needles(pattern).Compile(64))
		})
	}
}

func TestAnalysis_DeferredExactAncestors(t *testing.T) {
	// Long exact ancestors must retain ropes, not eagerly materialize every
	// child's standalone requirement and copy every growing prefix again.
	var re = &syntax.Regexp{Op: syntax.OpLiteral, Rune: []rune("start")}
	for range 1024 {
		re = &syntax.Regexp{Op: syntax.OpConcat, Sub: []*syntax.Regexp{
			{Op: syntax.OpCapture, Sub: []*syntax.Regexp{re}},
			{Op: syntax.OpLiteral, Rune: []rune("x")},
		}}
	}
	a := analyze(re, maxExact)
	set := a.exact[re][richMode]
	require.Len(t, set, 1)
	require.Empty(t, set[0].leaf)
	require.NotNil(t, set[0].left)
	// Successful singleton summaries can share the same immutable set
	// between modes instead of rehashing and rebuilding their ropes.
	basic := a.exact[re][basicMode]
	require.Same(t, &basic[0], &set[0])
	want := needle.Conjunctions{{"start" + strings.Repeat("x", 1024)}}
	require.Equal(t, want, a.expr(re, basicMode).Compile(1))
	require.Equal(t, want, a.expr(re, richMode).Compile(1))
}
