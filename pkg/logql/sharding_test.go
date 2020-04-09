package logql

// func TestMappingEquivalence(t *testing.T) {
// 	var (
// 		shards   = 16
// 		nStreams = 500
// 		rounds   = 200
// 		streams  = randomStreams(nStreams, rounds, shards, []string{"a", "b", "c", "d"})
// 		start    = time.Unix(0, 0)
// 		end      = time.Unix(0, int64(time.Millisecond*time.Duration(rounds)))
// 		limit    = 100
// 	)

// 	for _, tc := range []struct {
// 		query string
// 	}{
// 		{`{foo="bar"}`},
// 	} {
// 		q := NewMockQuerier(
// 			shards,
// 			streams,
// 		)

// 		opts := EngineOpts{}
// 		regular := NewEngine(opts, q)
// 		sharded := regular
// 		// sharded := NewEngine(opts, func(_ EngineOpts) Evaluator {
// 		// 	return &DownstreamEvaluator{
// 		// 		MockDownstreamer{regular}, // downstream to the regular engine
// 		// 	}
// 		// })

// 		shardMapper, err := NewShardMapper(int(shards))
// 		require.Nil(t, err)

// 		t.Run(tc.query, func(t *testing.T) {
// 			params := NewLiteralParams(
// 				tc.query,
// 				start,
// 				end,
// 				time.Millisecond*10,
// 				logproto.FORWARD,
// 				uint32(limit),
// 				nil,
// 			)
// 			shardedParams := params.Copy()

// 			parsed, err := ParseExpr(tc.query)
// 			require.Nil(t, err)
// 			shardedQuery, err := shardMapper.Map(parsed)
// 			require.Nil(t, err)
// 			shardedParams.qs = shardedQuery.String()

// 			qry := regular.NewRangeQuery(params)
// 			shardedQry := sharded.NewRangeQuery(shardedParams)

// 			res, err := qry.Exec(context.Background())
// 			require.Nil(t, err)
// 			shardedRes, err := shardedQry.Exec(context.Background())
// 			require.Nil(t, err)

// 			require.Equal(t, res, shardedRes)
// 		})
// 	}
// }
