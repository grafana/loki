package shard_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logline/shard"
)

func TestFirstByte_ShardRange(t *testing.T) {
	for b := range 256 {
		ngram := [8]byte{byte(b)}
		got := shard.FirstByte(ngram, 4)
		require.GreaterOrEqual(t, got, 0)
		require.Less(t, got, 4, "shard %d out of range for byte %d", got, b)
	}
}

func TestFirstByte_Deterministic(t *testing.T) {
	ngram := [8]byte{0x42}
	first := shard.FirstByte(ngram, 8)
	for range 100 {
		require.Equal(t, first, shard.FirstByte(ngram, 8))
	}
}

func TestFirstByte_DistributionNotAllZero(t *testing.T) {
	counts := make([]int, 4)
	for b := range 256 {
		ngram := [8]byte{byte(b)}
		counts[shard.FirstByte(ngram, 4)]++
	}
	for i, c := range counts {
		require.Equal(t, 64, c, "shard %d has unexpected count", i)
	}
}

func TestFirstByte_KnownValues(t *testing.T) {
	cases := []struct {
		firstByte byte
		want      int
	}{
		{0x00, 0}, // 0 % 4 = 0
		{0x01, 1}, // 1 % 4 = 1
		{0x04, 0}, // 4 % 4 = 0
		{0xFF, 3}, // 255 % 4 = 3
	}
	for _, tc := range cases {
		ngram := [8]byte{tc.firstByte}
		require.Equal(t, tc.want, shard.FirstByte(ngram, 4), "first_byte=0x%02X", tc.firstByte)
	}
}
