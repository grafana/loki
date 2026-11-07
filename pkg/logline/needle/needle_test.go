package needle

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConstructors(t *testing.T) {
	for _, tc := range []struct {
		name string
		expr Expr
		want string
	}{
		{"zero value", Expr{}, "TRUE"},
		{"true", True(), "TRUE"},
		{"empty literal", Lit(""), "TRUE"},
		{"literal", Lit("abc"), `{"abc"}`},
		{"all of nothing", AllOf(), "TRUE"},
		{"all of empty", AllOf("", ""), "TRUE"},
		{"all of sorted and deduped", AllOf("def", "", "abc", "def"), `{"abc", "def"}`},
		{"all of keeps contained literals", AllOf("abcdef", "abc"), `{"abc", "abcdef"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, tc.expr.String())
		})
	}
}

func TestOr(t *testing.T) {
	traceIDs := make([]Expr, 1000)
	for i := range traceIDs {
		traceIDs[i] = AllOf("trace_id=", fmt.Sprintf("%016x", i))
	}

	for _, tc := range []struct {
		name            string
		maxConjunctions int
		xs              []Expr
		want            string
	}{
		{"no operands", 64, nil, "TRUE"},
		{"single operand", 64, []Expr{AllOf("b", "a")}, `{"a", "b"}`},
		{"two literals", 64, []Expr{Lit("foo"), Lit("bar")}, `{"bar"} or {"foo"}`},
		{"duplicates", 64, []Expr{Lit("foo"), Lit("foo")}, `{"foo"}`},
		{"true operand", 64, []Expr{Lit("foo"), True()}, "TRUE"},
		{"empty literal operand", 64, []Expr{Lit("foo"), Lit("")}, "TRUE"},
		{"nested", 64, []Expr{Or(64, Lit("a"), Lit("b")), Lit("c")}, `{"a"} or {"b"} or {"c"}`},

		// Absorption.
		{"substring absorbs", 64, []Expr{Lit("errors"), Lit("error")}, `{"error"}`},
		{"subset absorbs", 64, []Expr{AllOf("abc", "def"), Lit("abc")}, `{"abc"}`},
		{"contained subset absorbs", 64, []Expr{AllOf("xabc", "def"), Lit("abc")}, `{"abc"}`},
		{"split literal absorbs", 64, []Expr{AllOf("abc", "bcd"), Lit("abcd")}, `{"abc", "bcd"}`},
		{"mutual implication keeps one", 64, []Expr{Lit("abcd"), AllOf("abc", "abcd")}, `{"abc", "abcd"}`},
		{"no absorption", 64, []Expr{AllOf("abc", "def"), AllOf("abc", "xyz")}, `{"abc", "def"} or {"abc", "xyz"}`},

		// Over maxConjunctions.
		{"collapse keeps shared literals", 2, []Expr{AllOf("x", "a"), AllOf("x", "b"), AllOf("x", "c")}, `{"x"}`},
		{"collapse with nothing shared", 2, []Expr{Lit("a"), Lit("b"), Lit("c")}, "TRUE"},
		{"absorption before collapse", 2, []Expr{Lit("a"), Lit("b"), Lit("ab")}, `{"a"} or {"b"}`},
		{"one conjunction", 1, []Expr{Lit("a"), Lit("b")}, "TRUE"},
		{"maxConjunctions below one", 0, []Expr{AllOf("x", "a"), AllOf("x", "b")}, `{"x"}`},
		{"many branches", 64, traceIDs, `{"trace_id="}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, Or(tc.maxConjunctions, tc.xs...).String())
		})
	}
}

func TestAnd(t *testing.T) {
	for _, tc := range []struct {
		name            string
		maxConjunctions int
		xs              []Expr
		want            string
	}{
		{"no operands", 64, nil, "TRUE"},
		{"true operands", 64, []Expr{True(), True()}, "TRUE"},
		{"true is the identity", 64, []Expr{True(), Lit("a")}, `{"a"}`},
		{"literals", 64, []Expr{Lit("b"), Lit("a"), Lit("b")}, `{"a", "b"}`},
		{"distributes", 64, []Expr{Lit("seed"), Or(64, Lit("a"), Lit("b"))}, `{"a", "seed"} or {"b", "seed"}`},
		{
			"product", 64,
			[]Expr{Or(64, Lit("a"), Lit("b")), Or(64, Lit("c"), Lit("d"))},
			`{"a", "c"} or {"a", "d"} or {"b", "c"} or {"b", "d"}`,
		},
		{"absorbs after product", 64, []Expr{Or(64, Lit("error"), Lit("warn")), Lit("errors")}, `{"error", "errors"}`},
		{"shared literal absorbs", 64, []Expr{Or(64, Lit("a"), Lit("b")), Or(64, Lit("a"), Lit("c"))}, `{"a"} or {"b", "c"}`},

		// Over maxConjunctions, the larger operand collapses.
		{
			"collapse the second operand", 3,
			[]Expr{Or(64, Lit("c"), Lit("d")), Or(64, Lit("a"), Lit("b"))},
			`{"c"} or {"d"}`,
		},
		{
			"collapse keeps shared literals", 3,
			[]Expr{Or(64, Lit("a"), Lit("b")), Or(64, AllOf("x", "c"), AllOf("x", "d"))},
			`{"a", "x"} or {"b", "x"}`,
		},
		{
			"fewest first", 2,
			[]Expr{Or(64, Lit("a"), Lit("b"), Lit("c")), Or(64, Lit("d"), Lit("e"))},
			`{"d"} or {"e"}`,
		},
		{
			"one conjunction keeps single literals", 1,
			[]Expr{Or(64, Lit("b"), Lit("c")), Lit("a"), Or(64, Lit("d"), Lit("e")), Lit("f")},
			`{"a", "f"}`,
		},
		{
			"operand above maxConjunctions", 2,
			[]Expr{Or(64, AllOf("x", "a"), AllOf("x", "b"), AllOf("x", "c"))},
			`{"x"}`,
		},
		{
			"maxConjunctions below one", -5,
			[]Expr{Lit("a"), Or(64, Lit("b"), Lit("c"))},
			`{"a"}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, And(tc.maxConjunctions, tc.xs...).String())
		})
	}
}

func TestMap(t *testing.T) {
	upper := func(s string) (string, bool) { return strings.ToUpper(s), true }
	minLen3 := func(s string) (string, bool) { return s, len(s) >= 3 }
	toEmpty := func(string) (string, bool) { return "", true }

	for _, tc := range []struct {
		name string
		expr Expr
		f    func(string) (string, bool)
		want string
	}{
		{"true", True(), upper, "TRUE"},
		{"rewrites", Or(64, Lit("abc"), Lit("def")), upper, `{"ABC"} or {"DEF"}`},
		{"merges equal results", Or(64, Lit("abc"), Lit("ABC")), upper, `{"ABC"}`},
		{"merges literals in a conjunction", AllOf("abc", "ABC"), upper, `{"ABC"}`},
		{"absorbs after rewrite", Or(64, AllOf("abcd", "XYZ"), Lit("ABC")), upper, `{"ABC"}`},
		{"drops from a conjunction", AllOf("ab", "abc"), minLen3, `{"abc"}`},
		{"empty conjunction is true", Or(64, Lit("ab"), Lit("abc")), minLen3, "TRUE"},
		{"empty result is dropped", Or(64, Lit("abc"), Lit("def")), toEmpty, "TRUE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, tc.expr.Map(tc.f).String())
		})
	}
}

func TestCollapse(t *testing.T) {
	for _, tc := range []struct {
		name string
		expr Expr
		want string
	}{
		{"true", True(), "TRUE"},
		{"one conjunction", AllOf("a", "b"), `{"a", "b"}`},
		{"shared literals", Or(64, AllOf("x", "y", "a"), AllOf("x", "y", "b")), `{"x", "y"}`},
		{"nothing shared", Or(64, Lit("a"), Lit("b")), "TRUE"},
		{"substrings are not shared", Or(64, AllOf("abc", "x"), AllOf("abcd", "y")), "TRUE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, tc.expr.Collapse().String())
		})
	}
}

func TestPrune(t *testing.T) {
	for _, tc := range []struct {
		name string
		expr Expr
		want string
	}{
		{"true", True(), "TRUE"},
		{"contained literal", AllOf("abcdef", "xabcdefy"), `{"xabcdefy"}`},
		{"chain", AllOf("a", "ab", "abc"), `{"abc"}`},
		{"unrelated literals", AllOf("abc", "def"), `{"abc", "def"}`},
		{"per conjunction", Or(64, AllOf("abc", "abcd"), AllOf("xy", "xyz")), `{"abcd"} or {"xyz"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, tc.expr.Prune().String())
		})
	}
}

func TestConjunctions(t *testing.T) {
	require.Empty(t, True().Conjunctions())

	e := Or(64, AllOf("b", "a"), Lit("c"))
	got := e.Conjunctions()
	require.Equal(t, [][]string{{"a", "b"}, {"c"}}, got)

	got[0][0] = "changed"
	require.Equal(t, `{"a", "b"} or {"c"}`, e.String())
}

func TestOperandsUnchanged(t *testing.T) {
	a := Or(64, Lit("b"), Lit("a"))
	b := Or(64, Lit("d"), Lit("c"))
	_ = And(64, a, b)
	_ = Or(64, b, a)
	_ = a.Map(func(s string) (string, bool) { return strings.ToUpper(s), true })
	_ = AllOf("abc", "abcd").Prune()
	require.Equal(t, `{"a"} or {"b"}`, a.String())
	require.Equal(t, `{"c"} or {"d"}`, b.String())
}
