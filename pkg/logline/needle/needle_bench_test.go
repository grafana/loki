package needle

import (
	"fmt"
	"testing"
)

var benchmarkPlan Plan

// BenchmarkCompileWorkload includes construction and final pruning in both APIs.
func BenchmarkCompileWorkload(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		benchmarkPlan = And(
			AllOf("service=api", "level=error"),
			Or(Lit("timeout"), Lit("refused"), Lit("unavailable")),
			Or(Lit("region=east"), Lit("region=west")),
		).Compile(16)
	}
}

func BenchmarkIncrementalChain(b *testing.B) {
	lits := make([]string, 1000)
	for i := range lits {
		lits[i] = fmt.Sprintf("literal_%06d", i)
	}
	b.ReportAllocs()
	for b.Loop() {
		e := True()
		for _, lit := range lits {
			e = And(e, Lit(lit))
		}
		benchmarkPlan = e.Compile(16)
	}
}

func BenchmarkIncrementalOrChain(b *testing.B) {
	xs := make([]Expr, 1000)
	for i := range xs {
		xs[i] = AllOf("shared", fmt.Sprintf("literal_%06d", i))
	}
	b.ReportAllocs()
	for b.Loop() {
		e := xs[0]
		for _, x := range xs[1:] {
			e = Or(e, x)
		}
		benchmarkPlan = e.Compile(16)
	}
}

func BenchmarkSharedConjunctions(b *testing.B) {
	for _, shared := range []int{1000, 4000} {
		for _, branches := range []int{4, 64} {
			if shared == 4000 && branches == 64 {
				continue
			}
			b.Run(fmt.Sprintf("literals=%d/branches=%d", shared, branches), func(b *testing.B) {
				xs := make([]Expr, branches)
				for branch := range xs {
					lits := make([]string, shared+1)
					for i := range shared {
						lits[i] = fmt.Sprintf("a%06d", i)
					}
					lits[shared] = fmt.Sprintf("z%06d", branch)
					xs[branch] = AllOf(lits...)
				}
				e := Or(xs...)
				b.ReportAllocs()
				for b.Loop() {
					benchmarkPlan = e.Compile(branches)
				}
			})
		}
	}
}

func BenchmarkSubstringFallback(b *testing.B) {
	xs := []Expr{
		AllOf("shared", "prefix_needle_a_suffix", "another_literal"),
		AllOf("shared", "prefix_needle_b_suffix", "another_literal"),
		AllOf("shared", "needle"),
	}
	b.ReportAllocs()
	for b.Loop() {
		benchmarkPlan = Or(xs...).Compile(1)
	}
}

func BenchmarkNestedOverCap(b *testing.B) {
	for _, reversed := range []bool{false, true} {
		for _, limit := range []int{1, 4} {
			for _, depth := range []int{100, 400, 800} {
				b.Run(fmt.Sprintf("reversed=%t/cap=%d/depth=%d", reversed, limit, depth), func(b *testing.B) {
					e := nestedOverCapExpression(depth, reversed)
					b.ReportAllocs()
					for b.Loop() {
						benchmarkPlan = e.Compile(limit)
					}
					c := compiler{root: e, limit: limit, plans: make(map[*node]Plan)}
					c.lower(e)
					b.ReportMetric(float64(len(c.proofs)), "proofs/op")
					b.ReportMetric(float64(c.witnessCost(e)), "witness-cost")
					b.ReportMetric(float64(len(c.costs)), "cost-nodes/op")
				})
			}
		}
	}
}

func BenchmarkPruneSharedLiterals(b *testing.B) {
	for _, count := range []int{1000, 4000} {
		b.Run(fmt.Sprintf("literals=%d", count), func(b *testing.B) {
			lits := make([]string, count)
			for i := range lits {
				lits[i] = fmt.Sprintf("a%06d", i)
			}
			e := AllOf(lits...)
			b.ReportAllocs()
			for b.Loop() {
				benchmarkPlan = e.Compile(1)
			}
		})
	}
}
