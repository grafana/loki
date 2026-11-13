package regexliteral

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logline/needle"
)

func TestDeferredText_HugeAdjacentCaptures(t *testing.T) {
	for _, captures := range []int{4000, 8000} {
		t.Run(strconv.Itoa(captures), func(t *testing.T) {
			pattern := "start" + strings.Repeat("(a{256})", captures) + "end"
			want := "start" + strings.Repeat("a", 256*captures) + "end"
			require.Equal(t, needle.Conjunctions{{want}}, Needles(pattern).Compile(1))
			require.Equal(t, []string{want}, Literals(pattern))
		})
	}
}

func TestDeferredText_FlushOrderPreservesBoundedAbsorption(t *testing.T) {
	const pattern = `(q00foo)|(q01foo)|(q02foo)|(q03foo)|(q04foo)|(q05foo)|` +
		`(q06foo)|(q07foo)|(q08foo)|(q09foo)|(q10foo)|(q11foo)|(foo)|(bar)`
	re, err := parse(pattern)
	require.NoError(t, err)
	set := analyze(re, maxExact).exact[re][richMode]
	require.Len(t, set, 14)

	// Sorting only at flush puts absorbing literals before the longer
	// alternatives, before bounded OR absorption can collapse to TRUE.
	conjs := Needles(pattern).Compile(2)
	require.Equal(t, needle.Conjunctions{{"bar"}, {"foo"}}, conjs)
	require.Equal(t, `{"bar"} or {"foo"}`, conjs.String())
}

func TestDeferredText_EmptyOptionalAndUTF8(t *testing.T) {
	for _, tc := range []struct {
		pattern string
		want    needle.Conjunctions
	}{
		{`(?:|)`, nil},
		{`(?:ab)?(?:ab)?`, nil},
		{`(?:){1000}`, nil},
		{`a(?:é?){0}b`, needle.Conjunctions{{"ab"}}},
		{`x(?:a|)y`, needle.Conjunctions{{"xay"}, {"xy"}}},
		{`(?:(é)(世))(界)`, needle.Conjunctions{{"é世界"}}},
		{`x(?:é|)界`, needle.Conjunctions{{"xé界"}, {"x界"}}},
		{`(?:(a)(bc)|(ab)(c))d`, needle.Conjunctions{{"abcd"}}},
	} {
		t.Run(tc.pattern, func(t *testing.T) {
			require.Equal(t, tc.want, Needles(tc.pattern).Compile(64))
		})
	}
}

func TestDeferredText_ExactCardinality(t *testing.T) {
	for _, tc := range []struct {
		pattern string
		limit   int
		want    []string
	}{
		// Different segmentations of one string still fit a singleton set.
		{`(?:(a)(bc)|(ab)(c))`, 1, []string{"abc"}},
		{`(a?)(a?)`, 4, []string{"", "a", "aa"}},
		// Preserve the product-before-dedup check: four combinations do
		// not fit a limit of three, even though only three are distinct.
		{`(a?)(a?)`, 3, nil},
	} {
		t.Run(tc.pattern+"/"+strconv.Itoa(tc.limit), func(t *testing.T) {
			re, err := parse(tc.pattern)
			require.NoError(t, err)
			set := analyze(re, tc.limit).exact[re][richMode]
			if tc.want == nil {
				require.Nil(t, set)
				return
			}
			require.NotNil(t, set)
			require.Equal(t, tc.want, materializeTextSet(set))
		})
	}
}

func TestDeferredText_LongSharedPrefixAlternatives(t *testing.T) {
	prefix := strings.Repeat("a", 256*512)
	suffix := strings.Repeat("b", 256*512)
	pattern := strings.Repeat("(a{256})", 512) + "(?:left|rite)" + strings.Repeat("(b{256})", 512)
	re, err := parse(pattern)
	require.NoError(t, err)
	set := analyze(re, maxExact).exact[re][richMode]
	require.NotNil(t, set)
	require.Equal(t, []string{prefix + "left" + suffix, prefix + "rite" + suffix}, materializeTextSet(set))
	// The basic walk's shared requirements must still survive Compile(1).
	require.Equal(t, []string{prefix, suffix}, Literals(pattern))
}

func TestDeferredText_CompareAndDedup(t *testing.T) {
	// Include prefix relationships, non-ASCII byte order, and different
	// concatenation shapes. All comparisons must agree with Go strings.
	texts := []*deferredText{
		nil,
		textLeaf("a"),
		textLeaf("ab"),
		concatText(textLeaf("a"), textLeaf("b")),
		concatText(concatText(textLeaf("a"), textLeaf("b")), textLeaf("c")),
		concatText(textLeaf("a"), concatText(textLeaf("b"), textLeaf("c"))),
		textLeaf("abd"),
		textLeaf("é"),
		concatText(textLeaf("é"), textLeaf("界")),
		textLeaf("界"),
	}
	for i, a := range texts {
		for j, b := range texts {
			require.Equal(t, strings.Compare(materializeText(a), materializeText(b)), compareText(a, b), "pair %d/%d", i, j)
		}
	}

	a := concatText(textLeaf("a"), textLeaf("bc"))
	b := concatText(textLeaf("ab"), textLeaf("c"))
	require.Equal(t, a.hash, b.hash)
	require.Len(t, textSet{a}.with(b), 1)

	// A fingerprint collision must not merge unequal strings.
	collision := textLeaf("xyz")
	collision.hash = a.hash
	require.Len(t, textSet{a}.with(collision), 2)

	set := crossText(textSet{textLeaf("ab"), textLeaf("a")}, textSet{nil, textLeaf("b")})
	require.Equal(t, []string{"a", "ab", "abb"}, materializeTextSet(set))
	// Repeat accounting uses lexical order, not insertion order or length.
	require.Equal(t, 3, (textSet{textLeaf("z"), textLeaf("aaa")}).firstBytes())
}

func TestDeferredText_CompareSkipsSharedSubtrees(t *testing.T) {
	// The logical prefix is a GiB, but sharing keeps its representation tiny.
	// Comparing or deduplicating these ropes must not scan that prefix.
	prefix := repeatText(textLeaf("x"), 1<<30)
	a := concatText(prefix, textLeaf("ab"))
	b := concatText(concatText(prefix, textLeaf("a")), textLeaf("b"))
	require.Equal(t, 0, compareText(a, b))
	require.Len(t, textSet{a}.with(b), 1)
	require.Equal(t, -1, compareText(a, concatText(prefix, textLeaf("ac"))))
	require.Equal(t, (1<<30)+2, (textSet{
		concatText(prefix, textLeaf("b")),
		concatText(prefix, textLeaf("aa")),
	}).firstBytes())
}

func TestDeferredText_RepeatAdjacency(t *testing.T) {
	for _, tc := range []struct {
		name    string
		pattern string
		want    []string
	}{
		{"last exact copy may cross cap", `xa{257}y`, []string{"x" + strings.Repeat("a", 257) + "y"}},
		{"omitted copy splits run", `xa{258}y`, []string{"ay", "x" + strings.Repeat("a", 256)}},
		{"UTF8 cap counts bytes", `xé{129}y`, []string{"x" + strings.Repeat("é", 129) + "y"}},
		{"omitted UTF8 copies split run", `xé{130}y`, []string{"x" + strings.Repeat("é", 128), "éy"}},
		{"last exact block may cross cap", `x(?:a{256}){2}y`, []string{"x" + strings.Repeat("a", 512) + "y"}},
		{"omitted block splits run", `x(?:a{256}){3}y`, []string{strings.Repeat("a", 256) + "y", "x" + strings.Repeat("a", 256)}},
		{"variable block count splits run", `x(?:a{256}){2,3}y`, []string{strings.Repeat("a", 256) + "y", "x" + strings.Repeat("a", 256)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, Literals(tc.pattern))
		})
	}

	// The first branch is shorter but lexically later. The lexically first
	// member has 257 bytes, so a second exact copy must be rejected.
	re, err := parse(`(?:z|a{257}){2}`)
	require.NoError(t, err)
	require.Nil(t, analyze(re, maxExact).exact[re][richMode])
}

func materializeTextSet(set textSet) []string {
	out := make([]string, 0, len(set))
	for _, text := range set {
		out = append(out, materializeText(text))
	}
	slices.Sort(out)
	return out
}
