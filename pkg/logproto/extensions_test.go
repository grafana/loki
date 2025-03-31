package logproto

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestShard_SpaceFor(t *testing.T) {
	target := uint64(100)
	shard := Shard{
		Stats: &IndexStatsResponse{
			Bytes: 50,
		},
	}

	for _, tc := range []struct {
		desc  string
		bytes uint64
		exp   bool
	}{
		{
			desc:  "full shard",
			bytes: 50,
			exp:   true,
		},
		{
			desc:  "overflow equal to underflow accepts",
			bytes: 100,
			exp:   true,
		},
		{
			desc:  "overflow",
			bytes: 101,
			exp:   false,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			require.Equal(t, shard.SpaceFor(&IndexStatsResponse{Bytes: tc.bytes}, target), tc.exp)
		})
	}
}
