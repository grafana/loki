package logqltest

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"

	"github.com/grafana/loki/pkg/push"
)

func TestTestingChunkStore_ShouldSupportWriteFlushAndQuery(t *testing.T) {
	s := newTestingChunkStore(t)
	defer s.close()

	// Generate entries between 10s and 60s, with a 10s step.
	var entries []push.Entry
	for i := 1; i <= 6; i++ {
		entries = append(entries, push.Entry{Timestamp: time.Unix(int64(i*10), 0).UTC(), Line: "hello"})
	}
	s.write(t, []logproto.Stream{{Labels: `{app="a"}`, Entries: entries}})
	s.flush(t)

	eng := logql.NewEngine(logql.EngineOpts{}, s.querier(), logql.NoLimits, log.NewNopLogger())
	params, err := logql.NewLiteralParams(`count_over_time({app="a"}[1m])`, time.Unix(60, 0).UTC(), time.Unix(60, 0).UTC(), 0, 0, logproto.FORWARD, 100, nil, nil)
	require.NoError(t, err)

	res, err := eng.Query(params).Exec(user.InjectOrgID(context.Background(), tenant))
	require.NoError(t, err)

	v, ok := res.Data.(promql.Vector)
	require.Truef(t, ok, "got %T", res.Data)
	require.Len(t, v, 1)
	require.Equal(t, float64(6), v[0].F)
}
