package base

import (
	"fmt"
	"testing"

	"github.com/prometheus/prometheus/promql"
)

var result *promql.Result

func BenchmarkChunkQueryable(b *testing.B) {
	for _, query := range queries {
		for _, encoding := range encodings {
			for _, testcase := range testcases {
				b.Run(fmt.Sprintf("%s/step_%s/%s/%s/", query.query, query.step, encoding.name, testcase.name), func(b *testing.B) {
					store, from := makeMockChunkStore(b, 24*30, encoding.e)
					b.ResetTimer()

					var r *promql.Result
					for n := 0; n < b.N; n++ {
						queryable := newChunkStoreQueryable(store, testcase.f)
						r = testRangeQuery(b, queryable, from, query)
					}
					result = r
				})
			}
		}
	}
}

func BenchmarkChunkQueryableFromTar(b *testing.B) {
	query, from, through, step, store := getTarDataFromEnv(b)
	b.Run(fmt.Sprintf("query=%s,from=%d,to=%d,step=%f", query, from.Unix(), through.Unix(), step.Seconds()), func(b *testing.B) {
		b.ResetTimer()
		runRangeQuery(b, query, from, through, step, store)
	})
}
