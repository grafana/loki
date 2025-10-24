package main

import (
	"testing"
	"time"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/stretchr/testify/require"
)

func TestDebugCmd(t *testing.T) {
	t.Skip("Test for debugging only")

	storageBucket = "dev-us-central-0-loki-dev-005-data"
	orgID = "29"
	indexStoragePrefix = "loki_dev_005_tsdb_index_"

	// start := time.Date(2025, 10, 22, 0, 0, 0, 0, time.UTC)
	// end := start.Add(24 * time.Hour)
	// query := "{tenantId=\"9151\", service_name=\"asserts/api-server\"}    |~ \"(?i)relabel rule\" | json  | logfmt | drop __error__, __error_details__  "

	start := time.Date(2025, 10, 24, 0, 0, 0, 0, time.UTC)
	end := start.Add(45 * time.Minute)
	query := "{namespace=\"microservices-demo\"} !~ \"debug|DEBUG|info|INFO\" |~ \"error|ERROR|fatal|FATAL\""

	params, err := logql.NewLiteralParams(query, start, end, 0, 0, logproto.BACKWARD, 1000, nil, nil)
	require.NoError(t, err)

	require.NoError(t, doComparison(params, "localhost:3101", "localhost:3102"))
	// // require.NoError(t, queryMetastore(params))

	// require.NoError(t, doExecuteLocallyV2(params))
}
