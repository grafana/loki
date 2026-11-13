package regexliteral

import (
	"fmt"
	"strings"
	"testing"

	"github.com/grafana/loki/v3/pkg/logline/needle"
)

var benchmarkRequirement needle.Expr
var benchmarkConjunctions needle.Conjunctions

func BenchmarkNeedlesCompileJoinedCross(b *testing.B) {
	for _, tc := range []struct {
		name    string
		pattern string
		mapLit  bool
		seed    bool
	}{
		{"joined", `(GET|PUT) /api (ok|fail)`, false, false},
		{"mapped_joined", `(GETGET|PUTPUT) /api (okayokay|failfail)`, true, false},
		{"seeded_joined", `(GET|PUT) /api (ok|fail)`, false, true},
	} {
		b.Run(tc.name, func(b *testing.B) {
			for _, cap := range []int{1, 4} {
				b.Run(fmt.Sprintf("cap=%d", cap), func(b *testing.B) {
					b.ReportAllocs()
					for b.Loop() {
						e := Needles(tc.pattern)
						if tc.seed {
							e = needle.And(needle.Lit("tenant=acme"), e)
						}
						if tc.mapLit {
							e = e.Map(func(s string) (string, bool) { return strings.ToUpper(s), len(s) >= 6 })
						}
						benchmarkConjunctions = e.Compile(cap)
					}
				})
			}
		})
	}
}

func BenchmarkNeedlesAdjacentCaptures(b *testing.B) {
	for _, captures := range []int{1000, 4000, 8000} {
		b.Run(fmt.Sprintf("captures=%d", captures), func(b *testing.B) {
			pattern := strings.Repeat("(a{256})", captures)
			b.ReportAllocs()
			for b.Loop() {
				benchmarkRequirement = Needles(pattern)
			}
		})
	}
}

func BenchmarkNeedlesSharedPrefix(b *testing.B) {
	pattern := strings.Repeat("(a{256})", 512) + "(?:left|rite)" + strings.Repeat("(b{256})", 512)
	b.ReportAllocs()
	for b.Loop() {
		benchmarkRequirement = Needles(pattern)
	}
}

func BenchmarkNeedlesNestedNonExact(b *testing.B) {
	for _, depth := range []int{32, 128, 512} {
		b.Run(fmt.Sprintf("depth=%d", depth), func(b *testing.B) {
			pattern := "L" + strings.Repeat("(", depth) + "foo.*bar" + strings.Repeat(")", depth) + "R"
			b.ReportAllocs()
			for b.Loop() {
				benchmarkRequirement = Needles(pattern)
			}
		})
	}
}

// Optional non-exact bodies can short-circuit at the first wildcard.
// Keep this case alongside nested captures to expose eager-analysis overhead.
func BenchmarkNeedlesOptionalNonExact(b *testing.B) {
	for _, captures := range []int{64, 1000} {
		b.Run(fmt.Sprintf("captures=%d", captures), func(b *testing.B) {
			pattern := "(?:.+" + strings.Repeat("(a{256})", captures) + ")?"
			b.ReportAllocs()
			for b.Loop() {
				benchmarkRequirement = Needles(pattern)
			}
		})
	}
}

func BenchmarkNeedlesTypical(b *testing.B) {
	ids := make([]string, 1000)
	for i := range ids {
		ids[i] = fmt.Sprintf("%016x", uint64(i)*0x9e3779b97f4a7c15)
	}
	for _, tc := range []struct {
		name    string
		pattern string
	}{
		{"literal", "database timeout"},
		{"alternation", "(GET|POST) /api/users"},
		{"trace_ids", "trace_id=(" + strings.Join(ids, "|") + ")"},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				benchmarkRequirement = Needles(tc.pattern)
			}
		})
	}
}
