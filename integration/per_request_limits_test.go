package integration

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/integration/client"
	"github.com/grafana/loki/integration/cluster"

	"github.com/grafana/loki/pkg/util/querylimits"
)

func TestPerRequestLimits(t *testing.T) {
	clu := cluster.New(nil)
	defer func() {
		assert.NoError(t, clu.Cleanup())
	}()

	var (
		tAll = clu.AddComponent(
			"all",
			"-target=all",
			"-log.level=debug",
			"-querier.per-request-limits-enabled=true",
		)
	)

	require.NoError(t, clu.Run())

	queryLimitsPolicy := client.InjectHeadersOption(map[string][]string{querylimits.HTTPHeaderQueryLimitsKey: {`{"maxQueryLength": "1m"}`}})
	cliTenant := client.New("org1", "", tAll.HTTPURL(), queryLimitsPolicy)

	// ingest log lines for tenant 1 and tenant 2.
	require.NoError(t, cliTenant.PushLogLineWithTimestamp("lineA", cliTenant.Now.Add(-45*time.Minute), map[string]string{"job": "fake"}))

	// check that per-rquest-limits are enforced
	_, err := cliTenant.RunRangeQuery(context.Background(), `{job="fake"}`)
	require.ErrorContains(t, err, "the query time range exceeds the limit (query length")

	_, err = cliTenant.LabelNames(context.Background())
	require.ErrorContains(t, err, "the query time range exceeds the limit (query length")

	// check without policy header
	cliTenant = client.New("org1", "", tAll.HTTPURL())
	resp, err := cliTenant.RunRangeQuery(context.Background(), `{job="fake"}`)
	require.NoError(t, err)
	var lines []string
	for _, stream := range resp.Data.Stream {
		for _, val := range stream.Values {
			lines = append(lines, val[1])
		}
	}
	require.ElementsMatch(t, []string{"lineA"}, lines)
}
