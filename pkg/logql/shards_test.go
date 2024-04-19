package logql

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

func TestShardString(t *testing.T) {
	for _, rc := range []struct {
		shard Shard
		exp   string
	}{
		{
			shard: Shard{
				PowerOfTwo: &index.ShardAnnotation{
					Shard: 1,
					Of:    2,
				},
			},
			exp: "1_of_2",
		},
		{
			shard: Shard{
				Bounded: &logproto.Shard{
					Bounds: logproto.FPBounds{
						Min: 1,
						Max: 2,
					},
				},
			},
			exp: `{"bounds":{"min":1,"max":2},"stats":null}`,
		},
		{
			shard: Shard{
				Bounded: &logproto.Shard{
					Stats: &logproto.IndexStatsResponse{
						Bytes: 1,
					},
					Bounds: logproto.FPBounds{
						Min: 1,
						Max: 2,
					},
				},
			},
			exp: `{"bounds":{"min":1,"max":2},"stats":{"streams":0,"chunks":0,"bytes":1,"entries":0}}`,
		},
		{
			// when more than one are present,
			// return the newest successful version (v2)
			shard: Shard{
				Bounded: &logproto.Shard{
					Stats: &logproto.IndexStatsResponse{
						Bytes: 1,
					},
					Bounds: logproto.FPBounds{
						Min: 1,
						Max: 2,
					},
				},
				PowerOfTwo: &index.ShardAnnotation{
					Shard: 1,
					Of:    2,
				},
			},
			exp: `{"bounds":{"min":1,"max":2},"stats":{"streams":0,"chunks":0,"bytes":1,"entries":0}}`,
		},
	} {
		t.Run(fmt.Sprintf("%+v", rc.shard), func(t *testing.T) {
			require.Equal(t, rc.exp, rc.shard.String())
		})
	}
}

func TestParseShard(t *testing.T) {
	for _, rc := range []struct {
		str     string
		version ShardVersion
		exp     Shard
	}{
		{
			str:     "1_of_2",
			version: PowerOfTwoVersion,
			exp: Shard{
				PowerOfTwo: &index.ShardAnnotation{
					Shard: 1,
					Of:    2,
				},
			},
		},
		{
			str:     `{"bounds":{"min":1,"max":2},"stats":null}`,
			version: BoundedVersion,
			exp: Shard{
				Bounded: &logproto.Shard{
					Bounds: logproto.FPBounds{
						Min: 1,
						Max: 2,
					},
				},
			},
		},
		{
			str:     `{"bounds":{"min":1,"max":2},"stats":{"streams":0,"chunks":0,"bytes":1,"entries":0}}`,
			version: BoundedVersion,
			exp: Shard{
				Bounded: &logproto.Shard{
					Stats: &logproto.IndexStatsResponse{
						Bytes: 1,
					},
					Bounds: logproto.FPBounds{
						Min: 1,
						Max: 2,
					},
				},
			},
		},
	} {
		t.Run(rc.str, func(t *testing.T) {
			shard, version, err := ParseShard(rc.str)
			require.NoError(t, err)
			require.Equal(t, rc.version, version)
			require.Equal(t, rc.exp, shard)
		})
	}
}

func TestParseShards(t *testing.T) {
	for _, rc := range []struct {
		strs    []string
		version ShardVersion
		exp     Shards
		err     bool
	}{
		{
			strs:    []string{"1_of_2", "1_of_2"},
			version: PowerOfTwoVersion,
			exp: Shards{
				NewPowerOfTwoShard(index.ShardAnnotation{
					Shard: 1,
					Of:    2,
				}),
				NewPowerOfTwoShard(index.ShardAnnotation{
					Shard: 1,
					Of:    2,
				}),
			},
		},
		{
			strs:    []string{`{"bounds":{"min":1,"max":2},"stats":null}`, `{"bounds":{"min":1,"max":2},"stats":null}`},
			version: BoundedVersion,
			exp: Shards{
				NewBoundedShard(logproto.Shard{
					Bounds: logproto.FPBounds{
						Min: 1,
						Max: 2,
					},
				}),
				NewBoundedShard(logproto.Shard{
					Bounds: logproto.FPBounds{
						Min: 1,
						Max: 2,
					},
				}),
			},
		},
		{
			strs:    []string{`{"bounds":{"min":1,"max":2},"stats":null}`, "1_of_2"},
			version: PowerOfTwoVersion,
			err:     true,
		},
	} {
		t.Run(fmt.Sprintf("%+v", rc.strs), func(t *testing.T) {
			shards, version, err := ParseShards(rc.strs)
			if rc.err {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, rc.version, version)
			require.Equal(t, rc.exp, shards)
		})
	}
}
