package tokenization

import (
	"fmt"
	"testing"
)

var sampleLogBenchCount = 0

func BenchmarkTokenizationTestCases(b *testing.B) {
	for i, tc := range tokenizationRealisticTestCases {
		line := []byte(tc.line)
		b.Run(fmt.Sprintf("test-case-%d", i), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				PreprocessAndTokenize(line)
			}
		})
	}
}

func BenchmarkTokenizationPlayground(b *testing.B) {
	line := []byte(`level=info ts=2023-09-06T00:59:59.982171323Z caller=metrics.go:160 component=frontend org_id=29 traceID=4b93729ff3efabd0 latency=fast query="{stream=\"stdout\",pod=\"loki-canary-nl54q\"} " query_hash=1280418884 query_type=limited range_type=range length=20s start_delta=2h54m30.690801022s end_delta=2h54m10.690801238s step=1s duration=13.926955ms status=200 limit=1000 returned_lines=0 throughput=16MB total_bytes=219kB total_bytes_non_indexed_labels=2.1kB lines_per_second=14935 total_lines=208 post_filter_lines=208 total_entries=41 store_chunks_download_time=1.592805ms queue_time=127µs splits=0 shards=0 chunk_refs_fetch_time=3.599883ms cache_chunk_req=1 cache_chunk_hit=1 cache_chunk_bytes_stored=0 cache_chunk_bytes_fetched=480079 cache_chunk_download_time=1.307396ms cache_index_req=0 cache_index_hit=0 cache_index_download_time=0s cache_stats_results_req=1 cache_stats_results_hit=1 cache_stats_results_download_time=361.913µs cache_result_req=0 cache_result_hit=0 cache_result_download_time=0s token_id=gcom-1234`)
	for i := 0; i < b.N; i++ {
		PreprocessAndTokenize(line)
	}
}
