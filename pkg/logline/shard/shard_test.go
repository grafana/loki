package shard_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logline/shard"
)

func TestNew_UnknownAlgorithm(t *testing.T) {
	_, err := shard.New("nonexistent")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown shard algorithm")
}

func TestNew_EmptyAlgorithm(t *testing.T) {
	fn, err := shard.New("")
	require.NoError(t, err)
	require.Equal(t, 0, fn([8]byte{0xFF}, 4))
}

func TestNew_FirstByte(t *testing.T) {
	fn, err := shard.New("first_byte")
	require.NoError(t, err)
	require.Equal(t, "first_byte", shard.AlgorithmFirstByte)
	// 0x42 = 66, 66 % 8 = 2
	require.Equal(t, 2, fn([8]byte{0x42}, 8))
}

func TestNew_Murmur3Mix(t *testing.T) {
	fn, err := shard.New("murmur3_mix")
	require.NoError(t, err)
	require.Equal(t, "murmur3_mix", shard.AlgorithmMurmur3Mix)
	got := fn([8]byte{0x42}, 8)
	require.GreaterOrEqual(t, got, 0)
	require.Less(t, got, 8)
}
