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
		"shardable vector aggregation":         {`sum(rate({app="a"}[1m]))`, true},
		"top-level quantile shards":            {`quantile_over_time(0.99, {app="a"} | unwrap v [1m]) by (pod)`, true},
		"nested quantile does not shard":       {`max(quantile_over_time(0.99, {app="a"} | unwrap v [1m]))`, false},
		"non-shardable range op":               {`stddev_over_time({app="a"} | unwrap v [1m])`, false},
		"vector() literal does not shard":      {`vector(1)`, false},
		"unparseable query is unsupported":     {`}{ not a query`, false},
		"bare log selector shards":             {`{app="a"}`, true},
		"log selector with line filter shards": {`{app="a"} |= "100"`, true},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.want, isQueryShardingSupported(tc.query))
		})
	}
}
