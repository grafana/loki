package needle

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestImpliesDNF(t *testing.T) {
	for _, tc := range []struct {
		name string
		a, b Expr
		want bool
	}{
		{"both true", True(), True(), true},
		{"anything implies true", Lit("foo"), True(), true},
		{"true does not imply a literal", True(), Lit("foo"), false},
		{"substring", Lit("prefixfoo"), Lit("foo"), true},
		{"substring direction matters", Lit("foo"), Lit("prefixfoo"), false},
		{"each alternative is covered", Or(Lit("prefixfoo"), Lit("prefixbar")), Or(Lit("foo"), Lit("bar")), true},
		{"every source alternative must be covered", Or(Lit("prefixfoo"), Lit("other")), Lit("foo"), false},
		{"one target alternative suffices", Lit("prefixfoo"), Or(Lit("foo"), Lit("bar")), true},
		{"all target literals are required", Lit("prefixfoo"), AllOf("foo", "seed"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, b := dnf{conj: tc.a.Compile(64)}, dnf{conj: tc.b.Compile(64)}
			require.Equal(t, tc.want, impliesDNF(a, b))
		})
	}
}

func TestCompileAndAbsorbsImpliedFactors(t *testing.T) {
	basic := Or(Lit("foo"), Lit("bar"))
	joined := Or(Lit("prefixfoo"), Lit("prefixbar"))
	for _, tc := range []struct {
		name string
		expr Expr
		want string
	}{
		{"stronger incoming factor", And(basic, joined), `{"prefixbar"} or {"prefixfoo"}`},
		{"stronger first factor", And(joined, basic), `{"prefixbar"} or {"prefixfoo"}`},
		{"mandatory seed", And(Lit("seed"), basic, joined), `{"prefixbar", "seed"} or {"prefixfoo", "seed"}`},
		{"independent alternatives still collapse", And(basic, Or(Lit("left"), Lit("right"))), `{"bar"} or {"foo"}`},
		{"partial implication is insufficient", And(basic, Or(Lit("prefixfoo"), Lit("other"))), `{"bar"} or {"foo"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, tc.expr.Compile(2).String())
		})
	}
}

func TestCompileAndAbsorptionPreservesSourceLiterals(t *testing.T) {
	x := Or(Lit("bcX"), Lit("Ybc"), Lit("ZbcT"))
	y := Or(AllOf("bc", "guard"), Lit("guard"))
	acc := Or(AllOf("guard", "alpha"), AllOf("guard", "omega"))
	incoming := Or(And(x, y, Lit("alpha")), And(x, y, Lit("omega")))

	// Bounded lowering loses bc, but collapsing the source can recover it.
	require.Equal(t, `{"alpha", "guard"} or {"guard", "omega"}`, incoming.Compile(2).String())
	const want = `{"alpha", "bc", "guard"} or {"bc", "guard", "omega"}`
	require.Equal(t, want, And(acc, incoming).Compile(2).String())

	// A later replacement must also preserve literals recovered earlier.
	joined := Or(Lit("guardalpha"), Lit("guardomega"))
	require.Equal(t, want, And(acc, incoming, joined).Compile(2).String())
}
