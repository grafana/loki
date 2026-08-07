package util

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cespare/xxhash/v2"
	"github.com/stretchr/testify/require"
)

func TestSampleHasher(t *testing.T) {
	t.Run("matches the concatenated hash", func(t *testing.T) {
		lbl := `{cluster="dev-002", namespace="loki-ops", pod="querier-7d9f8b6c4d-x2n4k"}`

		var hasher SampleHasher

		// Run the test on len(line) > sampleHashBufferSize twice, to exercise the xxhash
		// internal reset too.
		for _, n := range []int{sampleHashBufferSize / 4, sampleHashBufferSize * 4, sampleHashBufferSize * 4} {
			line := []byte(strings.Repeat("a", n))

			// The value goes on the wire between ingesters and queriers, so it has to stay
			// exactly the hash of the concatenation.
			want := xxhash.Sum64String(lbl + ":" + string(line))

			require.Equal(t, want, hasher.Hash(lbl, line), "line=%d", n)
			require.Equal(t, want, UniqueSampleHash(lbl, line), "line=%d", n)
		}
	})

	t.Run("does not allocate", func(t *testing.T) {
		var hasher SampleHasher
		lbl := `{cluster="dev-002", namespace="loki-ops", pod="querier-7d9f8b6c4d-x2n4k"}`

		for _, n := range []int{sampleHashBufferSize / 4, sampleHashBufferSize * 4} {
			line := []byte(strings.Repeat("a", n))
			t.Run(fmt.Sprintf("line=%d", n), func(t *testing.T) {
				var sink uint64
				allocs := testing.AllocsPerRun(100, func() { sink = hasher.Hash(lbl, line) })
				require.Zero(t, allocs)
				require.NotZero(t, sink)
			})
		}
	})
}

func BenchmarkSampleHash(b *testing.B) {
	lbl := `{cluster="dev-002", namespace="loki-ops", pod="querier-7d9f8b6c4d-x2n4k", container="querier", job="loki-ops/querier", level="info"}`

	for _, n := range []int{50, 100, 200, 300, 400, 500, 1000, 4000} {
		line := []byte(strings.Repeat("a", n))

		b.Run(fmt.Sprintf("line=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			var hasher SampleHasher
			var sink uint64
			for i := 0; i < b.N; i++ {
				sink = hasher.Hash(lbl, line)
			}
			_ = sink
		})
	}
}
