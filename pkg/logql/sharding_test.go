package logql

import (
	"context"
	"testing"
	"time"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/stretchr/testify/require"
)

func TestMappingEquivalence(t *testing.T) {
	var (
		shards   = 3
		nStreams = 10
		rounds   = 20
		streams  = randomStreams(nStreams, rounds, shards, []string{"a", "b", "c", "d"})
		start    = time.Unix(0, 0)
		end      = time.Unix(0, int64(time.Millisecond*time.Duration(rounds)))
		limit    = 100
	)

	for _, tc := range []struct {
		query string
	}{
		{`{a="1"}`},
		{`{a="1"} |= "number: 10"`},
	} {
		q := NewMockQuerier(
			shards,
			streams,
		)

		opts := EngineOpts{}
		regular := NewEngine(opts, q)
		sharded, err := NewShardedEngine(opts, shards, MockDownstreamer{regular})
		require.Nil(t, err)

		t.Run(tc.query, func(t *testing.T) {
			params := NewLiteralParams(
				tc.query,
				start,
				end,
				time.Millisecond*10,
				logproto.FORWARD,
				uint32(limit),
				nil,
			)
			qry := regular.NewRangeQuery(params)
			shardedQry := sharded.NewRangeQuery(params)

			res, err := qry.Exec(context.Background())
			require.Nil(t, err)
			shardedRes, err := shardedQry.Exec(context.Background())
			require.Nil(t, err)

			require.Equal(t, res.Data, shardedRes.Data)

		})
	}
}
