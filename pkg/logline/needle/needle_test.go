package needle

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConjunctionsString(t *testing.T) {
	for _, tc := range []struct {
		name string
		conj Conjunctions
		want string
	}{
		{"nil", nil, "TRUE"},
		{"empty", Conjunctions{}, "TRUE"},
		{"one set", Conjunctions{{"abc", "def"}}, `{"abc", "def"}`},
		{"alternatives", Conjunctions{{"abc"}, {"def", "ghi"}}, `{"abc"} or {"def", "ghi"}`},
		{"escaped literals", Conjunctions{{"a\"b", "line\nbreak", "\\"}}, `{"a\"b", "line\nbreak", "\\"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, tc.conj.String())
		})
	}
}

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
		{"all of prunes contained literals at compilation", AllOf("abcdef", "abc"), `{"abcdef"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, tc.expr.Compile(64).String())
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
		{"nested", 64, []Expr{Or(Lit("a"), Lit("b")), Lit("c")}, `{"a"} or {"b"} or {"c"}`},
		{"substring absorbs", 64, []Expr{Lit("errors"), Lit("error")}, `{"error"}`},
		{"subset absorbs", 64, []Expr{AllOf("abc", "def"), Lit("abc")}, `{"abc"}`},
		{"contained subset absorbs", 64, []Expr{AllOf("xabc", "def"), Lit("abc")}, `{"abc"}`},
		{"split literal absorbs", 64, []Expr{AllOf("abc", "bcd"), Lit("abcd")}, `{"abc", "bcd"}`},
		{"mutual implication keeps one then prunes", 64, []Expr{Lit("abcd"), AllOf("abc", "abcd")}, `{"abcd"}`},
		{"no absorption", 64, []Expr{AllOf("abc", "def"), AllOf("abc", "xyz")}, `{"abc", "def"} or {"abc", "xyz"}`},
		{"collapse keeps shared literals", 2, []Expr{AllOf("x", "a"), AllOf("x", "b"), AllOf("x", "c")}, `{"x"}`},
		{"collapse with nothing shared", 2, []Expr{Lit("a"), Lit("b"), Lit("c")}, "TRUE"},
		{"absorption before collapse", 2, []Expr{Lit("a"), Lit("b"), Lit("ab")}, `{"a"} or {"b"}`},
		{"one conjunction", 1, []Expr{Lit("a"), Lit("b")}, "TRUE"},
		{"maxConjunctions below one", 0, []Expr{AllOf("x", "a"), AllOf("x", "b")}, `{"x"}`},
		{"maxConjunctions does not overflow absorption threshold", int(^uint(0) >> 1), []Expr{Lit("a"), Lit("b")}, `{"a"} or {"b"}`},
		{"large union gets candidate from last branch", 1, []Expr{Lit("Xbc0"), Lit("Xbc1"), Lit("Xbc2"), Lit("Xbc3"), Lit("Xbc4"), Lit("bc")}, `{"bc"}`},
		{"many branches", 64, traceIDs, `{"trace_id="}`},
		{"many branches share a substring", 1, append([]Expr{Lit("trace_id")}, traceIDs...), `{"trace_id"}`},
		{
			"collapse keeps shared substrings", 2,
			[]Expr{AllOf("get foo", " foo"), AllOf("put foo", " foo"), AllOf("1 foobaz", " foobaz")},
			`{" foo"}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, Or(tc.xs...).Compile(tc.maxConjunctions).String())
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
		{"distributes", 64, []Expr{Lit("seed"), Or(Lit("a"), Lit("b"))}, `{"a", "seed"} or {"b", "seed"}`},
		{
			"product", 64,
			[]Expr{Or(Lit("a"), Lit("b")), Or(Lit("c"), Lit("d"))},
			`{"a", "c"} or {"a", "d"} or {"b", "c"} or {"b", "d"}`,
		},
		{"absorbs after product", 64, []Expr{Or(Lit("error"), Lit("warn")), Lit("errors")}, `{"errors"}`},
		{"shared literal absorbs", 64, []Expr{Or(Lit("a"), Lit("b")), Or(Lit("a"), Lit("c"))}, `{"a"} or {"b", "c"}`},
		{
			"collapse the second operand", 3,
			[]Expr{Or(Lit("c"), Lit("d")), Or(Lit("a"), Lit("b"))},
			`{"c"} or {"d"}`,
		},
		{
			"collapse keeps shared literals", 3,
			[]Expr{Or(Lit("a"), Lit("b")), Or(AllOf("x", "c"), AllOf("x", "d"))},
			`{"a", "x"} or {"b", "x"}`,
		},
		{
			"collapse keeps shared substrings", 3,
			[]Expr{Or(Lit("a"), Lit("b")), Or(AllOf("foo", "x"), AllOf("foobar", "y"))},
			`{"a", "foo"} or {"b", "foo"}`,
		},
		{
			"fewest first", 2,
			[]Expr{Or(Lit("a"), Lit("b"), Lit("c")), Or(Lit("d"), Lit("e"))},
			`{"d"} or {"e"}`,
		},
		{
			"one conjunction keeps single literals", 1,
			[]Expr{Or(Lit("b"), Lit("c")), Lit("a"), Or(Lit("d"), Lit("e")), Lit("f")},
			`{"a", "f"}`,
		},
		{
			"operand above maxConjunctions", 2,
			[]Expr{Or(AllOf("x", "a"), AllOf("x", "b"), AllOf("x", "c"))},
			`{"x"}`,
		},
		{
			"maxConjunctions below one", -5,
			[]Expr{Lit("a"), Or(Lit("b"), Lit("c"))},
			`{"a"}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, And(tc.xs...).Compile(tc.maxConjunctions).String())
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
		{"rewrites", Or(Lit("abc"), Lit("def")), upper, `{"ABC"} or {"DEF"}`},
		{"merges equal results", Or(Lit("abc"), Lit("ABC")), upper, `{"ABC"}`},
		{"merges literals in a conjunction", AllOf("abc", "ABC"), upper, `{"ABC"}`},
		{"absorbs after rewrite", Or(AllOf("abcd", "XYZ"), Lit("ABC")), upper, `{"ABC"}`},
		{"drops from a conjunction", AllOf("ab", "abc"), minLen3, `{"abc"}`},
		{"empty conjunction is true", Or(Lit("ab"), Lit("abc")), minLen3, "TRUE"},
		{"empty result is dropped", Or(Lit("abc"), Lit("def")), toEmpty, "TRUE"},
		{"nested AND and OR", And(Lit("service"), Or(Lit("abc"), Lit("def"))), upper, `{"ABC", "SERVICE"} or {"DEF", "SERVICE"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, tc.expr.Map(tc.f).Compile(64).String())
		})
	}
}

func TestCompileCollapse(t *testing.T) {
	for _, tc := range []struct {
		name string
		expr Expr
		want string
	}{
		{"true", True(), "TRUE"},
		{"one conjunction", AllOf("a", "b"), `{"a", "b"}`},
		{"shared literals", Or(AllOf("x", "y", "a"), AllOf("x", "y", "b")), `{"x", "y"}`},
		{"nothing shared", Or(Lit("a"), Lit("b")), "TRUE"},
		{"shared substring", Or(AllOf("abc", "x"), AllOf("abcd", "y")), `{"abc"}`},
		{"shared substring from later conjunction", Or(AllOf("abcd", "x"), AllOf("bcd", "y")), `{"bcd"}`},
		{
			"shared substring from last conjunction", Or(AllOf("abcX", "x"), AllOf("Xbcd", "y"), AllOf("bc", "z")),
			`{"bc"}`,
		},
		{"does not discover new substrings", Or(Lit("abcX"), Lit("Xbcd")), "TRUE"},
		{
			"all implied literals survive", Or(AllOf("abc", "bcd", "x"), AllOf("abcd", "y")),
			`{"abc", "bcd"}`,
		},
		{
			"redundant literals remain collapse candidates", Or(AllOf("abc", "abcd"), AllOf("XabcY", "z")),
			`{"abc"}`,
		},
		{
			"absorbed branches remain collapse candidates", Or(Lit("abcd"), AllOf("bc", "abcd"), AllOf("XbcY", "z")),
			`{"bc"}`,
		},
		{
			"nested AND implication", Or(And(Lit("abc"), Or(Lit("p"), Lit("q"))), AllOf("XabcY", "z")),
			`{"abc"}`,
		},
		{
			"late outer candidate survives nested cap", Or(And(Or(Lit("abcX"), Lit("Xbcd")), Or(Lit("p"), Lit("q"))), Lit("bc")),
			`{"bc"}`,
		},
		{
			"factored last branch supplies witness", Or(Lit("bc"), And(Or(Lit("abcX"), Lit("Xbcd")), Lit("z"))),
			`{"bc"}`,
		},
		{
			"outside candidates are not imported into a collapsed subtree", And(Or(Lit("abcX"), Lit("Xbcd")), Or(Lit("bc"), Lit("xy"))),
			"TRUE",
		},
		{
			"nested OR must imply candidate on every branch", Or(And(Or(Lit("abc"), Lit("def")), Lit("x")), AllOf("abc", "y")),
			"TRUE",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, tc.expr.Compile(1).String())
		})
	}
}

func TestCompilePrune(t *testing.T) {
	for _, tc := range []struct {
		name string
		expr Expr
		want string
	}{
		{"true", True(), "TRUE"},
		{"contained literal", AllOf("abcdef", "xabcdefy"), `{"xabcdefy"}`},
		{"chain", AllOf("a", "ab", "abc"), `{"abc"}`},
		{"unrelated literals", AllOf("abc", "def"), `{"abc", "def"}`},
		{"per conjunction", Or(AllOf("abc", "abcd"), AllOf("xy", "xyz")), `{"abcd"} or {"xyz"}`},
		{"reorders conjunctions after pruning", Or(AllOf("a", "zax"), Lit("m")), `{"m"} or {"zax"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, tc.expr.Compile(64).String())
		})
	}
}

func TestCompileTrue(t *testing.T) {
	for _, e := range []Expr{
		{}, True(), Lit(""), AllOf(), And(), Or(),
		Or(Lit("a"), Lit("b")),
		AllOf("a", "b").Map(func(string) (string, bool) { return "", false }),
	} {
		require.Nil(t, e.Compile(1))
	}
}

func TestCompileOwnership(t *testing.T) {
	for _, tc := range []struct {
		name  string
		expr  Expr
		limit int
		want  Conjunctions
	}{
		{"literal", Lit("foo"), 1, Conjunctions{{"foo"}}},
		{"pruned literals", AllOf("ab", "abc", "def"), 1, Conjunctions{{"abc", "def"}}},
		{"union", Or(AllOf("b", "a"), Lit("c")), 64, Conjunctions{{"a", "b"}, {"c"}}},
		{"product", And(Or(Lit("a"), Lit("b")), Lit("seed")), 64, Conjunctions{{"a", "seed"}, {"b", "seed"}}},
		{"collapse", Or(AllOf("shared", "x"), AllOf("shared", "y")), 1, Conjunctions{{"shared"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, other := tc.expr.Compile(tc.limit), tc.expr.Compile(tc.limit)
			require.Equal(t, tc.want, got)
			for _, c := range got {
				require.NotEmpty(t, c)
			}
			got[0][0] = "changed"
			got[0] = append(got[0], "extra")
			if len(got) > 1 {
				require.Equal(t, tc.want[1:], got[1:], "conjunctions must not share mutable backing arrays")
			}
			got[0] = nil
			got = append(got, []string{"added"})
			require.Len(t, got, len(tc.want)+1)
			require.Equal(t, tc.want, other, "an earlier compilation must remain unchanged")
			require.Equal(t, tc.want, tc.expr.Compile(tc.limit), "the source must remain unchanged")
		})
	}

	shared := AllOf("alpha", "beta")
	left, right := And(shared, Lit("left")), And(shared, Lit("right"))
	e := Or(left, right)
	want := Conjunctions{{"alpha", "beta", "left"}, {"alpha", "beta", "right"}}
	got, other := e.Compile(64), e.Compile(64)
	got[0][0] = "changed"
	require.Equal(t, want[1], got[1])
	got[1] = nil
	require.Equal(t, want, other)
	require.Equal(t, want, e.Compile(64))
	require.Equal(t, Conjunctions{{"alpha", "beta"}}, shared.Compile(1))
	require.Equal(t, want[:1], left.Compile(1))
	require.Equal(t, want[1:], right.Compile(1))
}

func TestTreeImmutability(t *testing.T) {
	lits := []string{"b", "a"}
	a := AllOf(lits...)
	lits[0] = "changed"
	children := []Expr{a, Lit("c")}
	conjunction := And(children...)
	disjunction := Or(children...)
	children[0] = True()

	before := disjunction.Compile(64)
	_ = And(disjunction, Lit("d")).Compile(1)
	_ = Or(conjunction, Lit("e")).Compile(1)
	mapped := disjunction.Map(func(s string) (string, bool) { return strings.ToUpper(s), true })
	require.Equal(t, `{"a", "b"}`, a.Compile(64).String())
	require.Equal(t, `{"a", "b", "c"}`, conjunction.Compile(64).String())
	require.Equal(t, `{"a", "b"} or {"c"}`, disjunction.Compile(64).String())
	require.Equal(t, `{"A", "B"} or {"C"}`, mapped.Compile(64).String())
	require.Equal(t, before, disjunction.Compile(64))
	require.Equal(t, `{"a", "b"} or {"c"}`, before.String())
}

func TestCompileLateCap(t *testing.T) {
	e := And(Or(Lit("a"), Lit("b")), Or(Lit("c"), Lit("d")))
	// Construction retains the two factors, not the four conjunctions.
	require.Equal(t, and, e.root.op)
	require.Len(t, e.root.children, 2)
	require.Equal(t, or, e.root.children[0].root.op)
	require.Equal(t, `{"a"} or {"b"}`, e.Compile(2).String())
	require.Equal(t, `{"a", "c"} or {"a", "d"} or {"b", "c"} or {"b", "d"}`, e.Compile(4).String())
	require.Equal(t, `{"a"} or {"b"}`, e.Compile(2).String())
}

func TestMapBeforeCap(t *testing.T) {
	e := Or(Lit("ABC"), Lit("abc"))
	require.Equal(t, "TRUE", e.Compile(1).String())
	mapped := e.Map(func(s string) (string, bool) { return strings.ToUpper(s), true })
	require.Equal(t, `{"ABC"}`, mapped.Compile(1).String())
	require.Equal(t, "TRUE", e.Compile(1).String())

	// Even a contained leaf remains available until Map has run.
	e = AllOf("abc", "abcdef")
	mapped = e.Map(func(s string) (string, bool) { return s, len(s) == 3 })
	require.Equal(t, `{"abc"}`, mapped.Compile(1).String())
	require.Equal(t, `{"abcdef"}`, e.Compile(1).String())
}

func TestCompileAssociativeChains(t *testing.T) {
	a := Or(Lit("a"), Lit("b"), Lit("c"))
	b := Or(Lit("d"), Lit("e"))
	c := Or(Lit("f"), Lit("g"))
	want := `{"d", "f"} or {"d", "g"} or {"e", "f"} or {"e", "g"}`
	// Flatten before lowering: the nested first pair must not consume the
	// cap before the smaller factors have been considered together.
	for _, e := range []Expr{And(a, b, c), And(And(a, b), c), And(a, And(b, c))} {
		require.Equal(t, want, e.Compile(4).String())
	}
	for _, e := range []Expr{Or(a, b, c), Or(Or(a, b), c), Or(a, Or(b, c))} {
		require.Equal(t, Or(a, b, c).Compile(4).String(), e.Compile(4).String())
	}
}

func TestCompileBoundedProduct(t *testing.T) {
	const factors = 80 // The exact product would contain 2^80 conjunctions.
	xs := make([]Expr, 0, factors+1)
	xs = append(xs, Lit("mandatory"))
	var line strings.Builder
	line.WriteString("mandatory ")
	for i := range factors {
		a, b := fmt.Sprintf("a%03d", i), fmt.Sprintf("b%03d", i)
		xs = append(xs, Or(AllOf("shared", a), AllOf("shared", b)))
		line.WriteString(a + " ")
	}
	line.WriteString("shared")
	e := And(xs...)
	for _, limit := range []int{1, 2, 16, 64} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			conjunctions := e.Compile(limit)
			require.Len(t, conjunctions, limit)
			matches := false
			for _, c := range conjunctions {
				require.Contains(t, c, "mandatory")
				require.Contains(t, c, "shared")
				all := true
				for _, lit := range c {
					all = all && strings.Contains(line.String(), lit)
				}
				matches = matches || all
			}
			require.True(t, matches, "an exact match must satisfy the bounded requirement")
		})
	}
}

func TestCompileBoundedUnion(t *testing.T) {
	// Five source branches, each lowering to four conjunctions, exceed the
	// OR absorption allowance of 4 * 4 + 3 without a large operand count.
	xs := make([]Expr, 5)
	for i := range xs {
		xs[i] = And(
			Lit("guard"),
			Or(Lit(fmt.Sprintf("a%d", i)), Lit(fmt.Sprintf("b%d", i))),
			Or(Lit(fmt.Sprintf("c%d", i)), Lit(fmt.Sprintf("d%d", i))),
		)
	}
	require.Equal(t, `{"guard"}`, Or(xs...).Compile(4).String())
}

func TestCompileCollapsedOperandNormalizesBeforeNextCap(t *testing.T) {
	a := Or(Lit("a"), Lit("b"))
	b := Or(AllOf("a", "b", "u"), AllOf("a", "b", "v"), AllOf("a", "b", "w"))
	c := Or(Lit("c"), Lit("d"), Lit("e"))
	require.Equal(t, `{"a", "b", "c"} or {"a", "b", "d"} or {"a", "b", "e"}`, And(a, b, c).Compile(3).String())
}

func TestCompileDuplicateOr(t *testing.T) {
	for _, reused := range []bool{false, true} {
		t.Run(fmt.Sprintf("reused=%t", reused), func(t *testing.T) {
			e := Lit("foo")
			foo, bar := Lit("foo"), Lit("bar")
			for range 100 {
				if reused {
					e = Or(e, foo, bar)
				} else {
					e = Or(e, Lit("foo"), Lit("bar"))
				}
			}
			require.Equal(t, `{"bar"} or {"foo"}`, e.Compile(2).String())
		})
	}

	// Substring absorption, not just exact deduplication, must happen before
	// deciding that a long union exceeds the allowance.
	e := Lit("foo")
	for i := range 100 {
		e = Or(e, Lit(fmt.Sprintf("foo_%03d", i)))
	}
	require.Equal(t, `{"foo"}`, e.Compile(1).String())
}

func nestedOverCapExpression(count int, reversed bool) Expr {
	e := Lit("shared")
	for i := range count {
		nested := And(e, Lit(fmt.Sprintf("left_%06d", i)))
		short := AllOf("shared", fmt.Sprintf("right_%06d", i))
		if reversed {
			e = Or(short, nested)
		} else {
			e = Or(nested, short)
		}
	}
	return e
}

func TestCompileNestedOverCap(t *testing.T) {
	for _, reversed := range []bool{false, true} {
		for _, limit := range []int{1, 4} {
			for _, depth := range []int{64, 256, 800} {
				t.Run(fmt.Sprintf("reversed=%t/cap=%d/depth=%d", reversed, limit, depth), func(t *testing.T) {
					e := nestedOverCapExpression(depth, reversed)
					c := compiler{root: e, limit: limit, plans: make(map[*node]dnf)}
					require.Equal(t, `{"shared"}`, Conjunctions(c.lower(e).prune().conj).String())
					// This family needs linear proof storage in either order,
					// not a separate proof for every ancestor/literal pair.
					require.LessOrEqual(t, len(c.proofs), 32*depth)
					require.Equal(t, 2, c.witnessCost(e))
					require.Len(t, c.sourceConjunction(e), 2)
					require.LessOrEqual(t, len(c.costs), 4*depth+2)
				})
			}
		}
		e := nestedOverCapExpression(64, reversed)
		// A literal outside the nested subtree remains a candidate even after
		// memoized proofs for other literals have been populated.
		require.Equal(t, `{"hare"}`, Or(e, Lit("hare")).Compile(1).String())
		require.Equal(t, `{"shared"}`, e.Compile(1).String())
	}
}

func TestWitnessCost(t *testing.T) {
	for _, tc := range []struct {
		name string
		expr Expr
		cost int
	}{
		{"true", True(), 0},
		{"literals", AllOf("a", "b", "c"), 3},
		{"AND sums", And(AllOf("a", "b"), AllOf("c", "d", "e")), 5},
		{"OR takes minimum", Or(AllOf("a", "b", "c"), Lit("a")), 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := compiler{}
			require.Equal(t, tc.cost, c.witnessCost(tc.expr))
		})
	}

	// A shared DAG has a huge scalar cost without a huge stored witness.
	// Saturation must not wrap the sum and make it look cheaper than a leaf.
	e := Lit("foo")
	for range 100 {
		e = Expr{root: &node{op: and, children: []Expr{e, e}}}
	}
	c := compiler{}
	require.Equal(t, int(^uint(0)>>1), c.witnessCost(e))
	require.Len(t, c.costs, 102)
	require.Equal(t, []string{"foo"}, c.sourceConjunction(e))
	e = Or(e, AllOf("shared", "short"))
	require.Equal(t, 2, c.witnessCost(e))
	require.Equal(t, []string{"shared", "short"}, c.sourceConjunction(e))
}

func TestProofCheapestChild(t *testing.T) {
	// Exercise every position of a cheapest child in a non-binary OR. All
	// remaining operands must still be checked after the preferred one.
	for first := range 3 {
		branches := []Expr{AllOf("shared", "left"), AllOf("shared", "right"), Lit("only")}
		branches[first], branches[2] = branches[2], branches[first]
		e := Or(branches...)
		c := compiler{}
		c.witnessCost(e)
		require.Equal(t, first, c.cheapestChild(e.root))
		require.False(t, c.prove(e, "only", false))
		require.False(t, c.prove(e, "shared", false))
		require.True(t, c.prove(e, "shared", true))
		require.True(t, c.prove(e, "only", true))
	}
}

func TestSharedExpressionDAG(t *testing.T) {
	for _, op := range []operator{and, or} {
		t.Run(fmt.Sprint(op), func(t *testing.T) {
			e := Lit("foo")
			for range 20 {
				// Construct explicit shared nodes too, so the test does not
				// rely only on constructor deduplication.
				e = Expr{root: &node{op: op, children: []Expr{e, e}}}
			}
			calls := 0
			mapped := e.Map(func(s string) (string, bool) {
				calls++
				return strings.ToUpper(s), true
			})
			require.Equal(t, 1, calls)
			require.Equal(t, `{"FOO"}`, mapped.Compile(1).String())
			require.Equal(t, `{"foo"}`, e.Compile(1).String())
		})
	}

	leaf := Lit("foo")
	require.Same(t, leaf.root, And(leaf, leaf).root)
	require.Same(t, leaf.root, Or(leaf, leaf).root)
	// Shared nodes behind alternating operators also reuse mapped output.
	x := Or(Lit("bar"), leaf)
	e := And(x, Or(x, Lit("baz")))
	calls := 0
	mapped := e.Map(func(s string) (string, bool) {
		calls++
		return s, true
	})
	require.Equal(t, 3, calls)
	require.Same(t, mapped.root.children[0].root, mapped.root.children[1].root.children[0].root)
	require.Equal(t, `{"bar"} or {"foo"}`, mapped.Compile(2).String())

	// Repeated shared operands behind alternating operators cannot be
	// eliminated by associative flattening alone. Lowering is memoized too.
	e = leaf
	bar, baz := Lit("bar"), Lit("baz")
	for range 20 {
		e = Or(And(e, bar), And(e, baz))
	}
	calls = 0
	mapped = e.Map(func(s string) (string, bool) {
		calls++
		return strings.ToUpper(s), true
	})
	require.Equal(t, 3, calls)
	require.Equal(t, `{"BAR", "FOO"} or {"BAZ", "FOO"}`, mapped.Compile(2).String())
	require.Equal(t, `{"bar", "foo"} or {"baz", "foo"}`, e.Compile(2).String())
}

func TestLongNestedAndOr(t *testing.T) {
	const count = 20000
	conjunction, disjunction := True(), Lit("shared")
	for i := range count {
		lit := fmt.Sprintf("literal_%06d", i)
		conjunction = And(conjunction, Lit(lit))
		disjunction = Or(disjunction, AllOf("shared", lit))
	}
	identity := func(s string) (string, bool) { return s, true }
	conjunctions := conjunction.Map(identity).Compile(1)
	require.Len(t, conjunctions, 1)
	require.Len(t, conjunctions[0], count)
	require.Equal(t, "literal_000000", conjunctions[0][0])
	require.Equal(t, `{"shared"}`, disjunction.Map(identity).Compile(1).String())
}
