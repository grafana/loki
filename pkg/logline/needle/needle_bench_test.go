package needle

import (
	"fmt"
	"testing"
)

var benchmarkExpr Expr

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
				b.ReportAllocs()
				for b.Loop() {
					benchmarkExpr = Or(branches, xs...)
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
		benchmarkExpr = Or(4, xs...).Collapse().Prune()
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
				benchmarkExpr = e.Prune()
			}
		})
	}
}
