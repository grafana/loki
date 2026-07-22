package tsdb

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
)

// mergeVariants is the set of merge implementations under test. Every
// implementation must produce byte-identical output for the same input.
var mergeVariants = []struct {
	name string
	fn   func(res []logproto.ChunkRefWithSizingInfo, xs [][]logproto.ChunkRefWithSizingInfo) []logproto.ChunkRefWithSizingInfo
}{
	{"sort", mergeChunkRefsSort},
	{"heap", mergeChunkRefsHeap},
	{"losertree", mergeChunkRefsLoserTree},
}

func TestMergeChunkRefs_Empty(t *testing.T) {
	for _, v := range mergeVariants {
		t.Run(v.name, func(t *testing.T) {
			require.Empty(t, v.fn(nil, nil))
			require.Empty(t, v.fn(nil, [][]logproto.ChunkRefWithSizingInfo{}))
			require.Empty(t, v.fn(nil, [][]logproto.ChunkRefWithSizingInfo{nil, nil}))
		})
	}
}

func TestMergeChunkRefs_EquivalentAcrossVariants(t *testing.T) {
	// Compare every non-baseline variant against the sort.Slice baseline
	// across the same parameter grid the benchmarks use (minus the 1M cells).
	for _, k := range []int{1, 2, 4, 8, 16} {
		for _, n := range []int{0, 1, 100, 10_000} {
			for _, overlap := range []int{0, 10, 50, 100} {
				k, n, overlap := k, n, overlap
				t.Run(fmt.Sprintf("K=%d/N=%d/overlap=%d", k, n, overlap), func(t *testing.T) {
					src := benchInputs(k, n, overlap, 1)
					baseline := mergeChunkRefsSort(nil, cloneInputs(src))
					for _, v := range mergeVariants {
						if v.name == "sort" {
							continue
						}
						got := v.fn(nil, cloneInputs(src))
						require.Equalf(t, baseline, got, "variant %s diverged from sort baseline", v.name)
					}
				})
			}
		}
	}
}

// TestMergeChunkRefs_DedupsInLineDuplicates ensures that a duplicate that
// only appears within a single input list (not across lists) is also
// dropped, matching the map-based dedup guarantee of the baseline.
func TestMergeChunkRefs_DedupsInLineDuplicates(t *testing.T) {
	ref := logproto.ChunkRefWithSizingInfo{
		ChunkRef: logproto.ChunkRef{Fingerprint: 42, From: 100, Through: 200, Checksum: 7},
	}
	other := logproto.ChunkRefWithSizingInfo{
		ChunkRef: logproto.ChunkRef{Fingerprint: 43, From: 100, Through: 200, Checksum: 8},
	}
	xs := [][]logproto.ChunkRefWithSizingInfo{
		{ref, ref, other},
		{ref},
	}
	want := []logproto.ChunkRefWithSizingInfo{ref, other}
	for _, v := range mergeVariants {
		t.Run(v.name, func(t *testing.T) {
			got := v.fn(nil, cloneInputs(xs))
			require.Equal(t, want, got)
		})
	}
}
