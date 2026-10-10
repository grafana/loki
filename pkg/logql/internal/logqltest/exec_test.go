package logqltest

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsQueryShardingSupported(t *testing.T) {
	for name, tc := range map[string]struct {
		query string
		want  bool
	}{
		"shardable vector aggregation":                                              {`sum(rate({app="a"}[1m]))`, true},
		"top-level quantile shards":                                                 {`quantile_over_time(0.99, {app="a"} | unwrap v [1m]) by (pod)`, true},
		"nested quantile does not shard":                                            {`max(quantile_over_time(0.99, {app="a"} | unwrap v [1m]))`, false},
		"top-level approx_count_distinct shards":                                    {`approx_count_distinct(id, {app="a"} | logfmt [1m]) by (pod)`, true},
		"non-shardable range op":                                                    {`stddev_over_time({app="a"} | unwrap v [1m])`, false},
		"avg_over_time with an __error__ unwrap post filter does not shard":         {`avg_over_time({app="a"} | unwrap v | __error__="" [1m]) by (pod)`, false},
		"avg_over_time with an __error_details__ unwrap post filter does not shard": {`avg_over_time({app="a"} | unwrap v | __error_details__="" [1m]) by (pod)`, false},
		"avg_over_time with another unwrap post filter shards":                      {`avg_over_time({app="a"} | unwrap v | pod="p1" [1m]) by (pod)`, true},
		"vector() literal does not shard":                                           {`vector(1)`, false},
		"unparseable query is unsupported":                                          {`}{ not a query`, false},
		"bare log selector shards":                                                  {`{app="a"}`, true},
		"log selector with line filter shards":                                      {`{app="a"} |= "100"`, true},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.want, isQueryShardingSupported(tc.query))
		})
	}
}
