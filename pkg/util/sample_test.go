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

// BenchmarkSampleHashThreshold measures the two hashing strategies against each other so
// sampleHashBufferSize can be re-derived.
//
// The buffer strategy copies the labels and the line into one contiguous slice and hashes it in a
// single call. The streaming strategy feeds xxhash three writes and copies nothing. Copying wins
// while the copy is cheap, so the size at which they cross is where the constant belongs.
//
// Both are written out here rather than called through SampleHasher.Hash, which would consult the
// constant and pick one for us, measuring nothing. Run:
//
//	go test ./pkg/util/ -run '^$' -bench BenchmarkSampleHashThreshold -count=6
//
// and compare buffer against streaming at each total size. The crossover moves with the CPU, so
// treat the committed constant as one machine's answer.
func BenchmarkSampleHashThreshold(b *testing.B) {
	lbl := `{cluster="dev-002", namespace="loki-ops", pod="querier-7d9f8b6c4d-x2n4k", container="querier", job="loki-ops/querier", level="info"}`

	// Sizes are the labels+separator+line total the threshold is compared against, spanning the
	// crossover in both directions.
	for _, total := range []int{256, 384, 512, 640, 768, 896, 1024, 1536, 2048} {
		line := []byte(strings.Repeat("a", total-len(lbl)-1))

		b.Run(fmt.Sprintf("total=%d/buffer", total), func(b *testing.B) {
			b.ReportAllocs()
			// Deliberately larger than sampleHashBufferSize, so the strategy can be measured
			// past the point where it stops being the one Hash would choose.
			var buf [4096]byte
			var sink uint64
			for i := 0; i < b.N; i++ {
				s := append(buf[:0], lbl...)
				s = append(s, ':')
				s = append(s, line...)
				sink = xxhash.Sum64(s)
			}
			require.NotZero(b, sink)
		})

		b.Run(fmt.Sprintf("total=%d/streaming", total), func(b *testing.B) {
			b.ReportAllocs()
			var digest xxhash.Digest
			var sink uint64
			for i := 0; i < b.N; i++ {
				digest.Reset()
				_, _ = digest.WriteString(lbl)
				_, _ = digest.WriteString(":")
				_, _ = digest.Write(line)
				sink = digest.Sum64()
			}
			require.NotZero(b, sink)
		})
	}
}
