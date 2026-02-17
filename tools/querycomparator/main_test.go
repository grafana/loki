package main

import (
	"testing"
	"time"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/stretchr/testify/require"
)

func TestDebugCmd(t *testing.T) {
	t.Skip("Test for debugging purposes only")

	storageBucket = ""      // TODO: set bucket name for local engine support
	indexStoragePrefix = "" // TODO: set index storage prefix for local engine support
	orgID = ""              // TODO: set org ID for querying remote instances

	start := time.Date(2025, 10, 24, 0, 0, 0, 0, time.UTC)
	end := start.Add(45 * time.Minute)
	query := "{namespace=\"test\"}"

	params, err := logql.NewLiteralParams(query, start, end, 0, 0, logproto.BACKWARD, 1000, nil, nil)
	require.NoError(t, err)

	// Run subcommands as necessary
	require.NoError(t, doComparison(params, "localhost:3101", "localhost:3102"))
	require.NoError(t, queryMetastore(params))
	require.NoError(t, doExecuteLocallyV2(params))
}
