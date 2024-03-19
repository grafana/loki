package logql

import (
	"fmt"
	"testing"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/querier/astmapper"
	"github.com/stretchr/testify/require"
)

func TestShardString(t *testing.T) {
	for _, rc := range []struct {
		shard Shard
		exp   string
	}{
		{
			shard: Shard{
				PowerOfTwo: &astmapper.ShardAnnotation{
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
				PowerOfTwo: &astmapper.ShardAnnotation{
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
		str string
		exp Shard
	}{
		{
			str: "1_of_2",
			exp: Shard{
				PowerOfTwo: &astmapper.ShardAnnotation{
					Shard: 1,
					Of:    2,
				},
			},
		},
		{
			str: `{"bounds":{"min":1,"max":2},"stats":null}`,
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
			str: `{"bounds":{"min":1,"max":2},"stats":{"streams":0,"chunks":0,"bytes":1,"entries":0}}`,
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
			shard, err := ParseShard(rc.str)
			require.NoError(t, err)
			require.Equal(t, rc.exp, shard)
		})
	}
}
