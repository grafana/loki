package main

import (
	"testing"
	"time"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/stretchr/testify/require"
)

func TestQueryComparatorCmd(t *testing.T) {
	t.Skip("Test for debugging purposes only")

	bucket := MustGCSDataobjBucket("")
	orgID = "" // TODO: set org ID for querying remote instances

	start := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(1 * time.Minute)
	query := `{container="distributor"}`

	params, err := logql.NewLiteralParams(query, start, end, 0, 0, logproto.BACKWARD, 1000, nil, nil)
	require.NoError(t, err)

	// Run subcommands as necessary
	require.NoError(t, doComparison(params, "localhost:3101", "localhost:3102"))
	require.NoError(t, queryMetastore(params, bucket))
	require.NoError(t, doExecuteLocallyV2SchedulerRemote(params, bucket))
}
