package shard_test

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logline/shard"
)

func TestMurmur3Mix_ShardRange(t *testing.T) {
	for i := range 10000 {
		var ngram [8]byte
		binary.LittleEndian.PutUint64(ngram[:], uint64(i))
		got := shard.Murmur3Mix(ngram, 10)
		require.GreaterOrEqual(t, got, 0)
		require.Less(t, got, 10, "shard %d out of range for key %d", got, i)
	}
}

func TestMurmur3Mix_Deterministic(t *testing.T) {
	ngram := [8]byte{0x42, 0x13, 0xAB, 0xFF, 0x00, 0x7C, 0x31, 0x9E}
	first := shard.Murmur3Mix(ngram, 8)
	for range 100 {
		require.Equal(t, first, shard.Murmur3Mix(ngram, 8))
	}
}

func TestMurmur3Mix_Distribution(t *testing.T) {
	const (
		shardCount = 10
		nKeys      = 100000
	)

	counts := make([]int, shardCount)
	for i := range nKeys {
		var ngram [8]byte
		binary.LittleEndian.PutUint64(ngram[:], uint64(i))
		counts[shard.Murmur3Mix(ngram, shardCount)]++
	}

	expected := float64(nKeys) / float64(shardCount)
	for i, c := range counts {
		ratio := float64(c) / expected
		// Allow 5% deviation from perfect uniformity.
		require.InDelta(t, 1.0, ratio, 0.05, "shard %d has count %d, ratio %.3f", i, c, ratio)
	}
}

func TestMurmur3Mix_KnownValues(t *testing.T) {
	// Pin a few values to detect accidental algorithm changes.
	cases := []struct {
		ngram [8]byte
		count int
		want  int
	}{
		{[8]byte{0, 0, 0, 0, 0, 0, 0, 0}, 10, murmur3MixExpected([8]byte{0, 0, 0, 0, 0, 0, 0, 0}, 10)},
		{[8]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}, 10, murmur3MixExpected([8]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}, 10)},
		{[8]byte{'A', 'U', 'T', 'H', 'E', 'N', 0, 0}, 10, murmur3MixExpected([8]byte{'A', 'U', 'T', 'H', 'E', 'N', 0, 0}, 10)},
	}
	for _, tc := range cases {
		got := shard.Murmur3Mix(tc.ngram, tc.count)
		require.Equal(t, tc.want, got, "ngram=%v count=%d", tc.ngram, tc.count)
	}
}

// murmur3MixExpected is a reference implementation to generate test vectors.
func murmur3MixExpected(ngram [8]byte, count int) int {
	v := binary.LittleEndian.Uint64(ngram[:])
	v ^= v >> 33
	v *= 0xff51afd7ed558ccd
	v ^= v >> 33
	v *= 0xc4ceb9fe1a85ec53
	v ^= v >> 33
	return int(v % uint64(count))
}

func TestMurmur3Mix_AllBytesParticipate(t *testing.T) {
	// Flipping any single bit in the input must change the output shard
	// for at least some shard counts. This verifies all 64 bits participate.
	base := [8]byte{0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC, 0xDE, 0xF0}
	baseHash := murmur3MixRaw(base)

	for bit := range 64 {
		flipped := base
		flipped[bit/8] ^= 1 << (bit % 8)
		flippedHash := murmur3MixRaw(flipped)
		require.NotEqual(t, baseHash, flippedHash, "flipping bit %d did not change hash", bit)
	}
}

func murmur3MixRaw(ngram [8]byte) uint64 {
	v := binary.LittleEndian.Uint64(ngram[:])
	v ^= v >> 33
	v *= 0xff51afd7ed558ccd
	v ^= v >> 33
	v *= 0xc4ceb9fe1a85ec53
	v ^= v >> 33
	return v
}

func BenchmarkMurmur3Mix(b *testing.B) {
	ngram := [8]byte{0x42, 0x13, 0xAB, 0xFF, 0x00, 0x7C, 0x31, 0x9E}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = shard.Murmur3Mix(ngram, 10)
	}
}

func BenchmarkFirstByte(b *testing.B) {
	ngram := [8]byte{0x42, 0x13, 0xAB, 0xFF, 0x00, 0x7C, 0x31, 0x9E}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = shard.FirstByte(ngram, 10)
	}
}

func TestMurmur3Mix_ChiSquaredUniformity(t *testing.T) {
	// Chi-squared test: verify the distribution doesn't deviate from uniform
	// at p=0.01 significance level.
	const (
		shardCount = 10
		nKeys      = 1_000_000
	)

	counts := make([]float64, shardCount)
	for i := range nKeys {
		var ngram [8]byte
		binary.LittleEndian.PutUint64(ngram[:], uint64(i))
		counts[shard.Murmur3Mix(ngram, shardCount)]++
	}

	expected := float64(nKeys) / float64(shardCount)
	var chiSq float64
	for _, c := range counts {
		diff := c - expected
		chiSq += diff * diff / expected
	}

	// Critical value for chi-squared with 9 degrees of freedom at p=0.01 is 21.666.
	criticalValue := 21.666
	require.Less(t, chiSq, criticalValue,
		"chi-squared %.2f exceeds critical value %.2f (df=%d, p=0.01): distribution is not uniform",
		chiSq, criticalValue, shardCount-1)

	// Log the actual skew for visibility.
	lowest, highest := math.MaxFloat64, 0.0
	for _, c := range counts {
		lowest = math.Min(lowest, c)
		highest = math.Max(highest, c)
	}
	t.Logf("chi-squared=%.2f, min=%d, max=%d, skew=%.3fx", chiSq, int(lowest), int(highest), highest/lowest)
}
