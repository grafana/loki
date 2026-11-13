package regexliteral

import (
	"fmt"
	"strings"
	"testing"

	"github.com/grafana/loki/v3/pkg/logline/needle"
)

var benchmarkRequirement needle.Expr

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
