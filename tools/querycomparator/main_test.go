package main

import (
	"testing"
	"time"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/stretchr/testify/require"
)

func TestDebugCmd(t *testing.T) {
	// t.Skip("Test for debugging purposes only")
	// nocommit

	storageBucket = "dev-us-central-0-loki-dev-005-data" // TODO: set bucket name for local engine support
	indexStoragePrefix = ""                              // TODO: set index storage prefix for local engine support
	orgID = "29"                                         // TODO: set org ID for querying remote instances

	start := time.Date(2025, 11, 19, 14, 34, 0, 0, time.UTC)
	end := start.Add(1 * time.Hour)
	query := `{job="cloudcost-exporter/cloudcost-exporter", cluster="dev-us-central-0", stream="stdout"}`

	params, err := logql.NewLiteralParams(query, start, end, 0, 0, logproto.BACKWARD, 1000, nil, nil)
	require.NoError(t, err)

	// Run subcommands as necessary
	// require.NoError(t, doComparison(params, "localhost:3101", "localhost:3102"))
	require.NoError(t, queryMetastore(params))
	// require.NoError(t, doExecuteLocallyV2(params))

	/*
		 	streams, err := queryMetastoreStreams(params)
			require.NoError(t, err)
			for _, stream := range streams[:100] {
				fmt.Printf("stream: %s\n", stream.String())
			}
	*/
}
