//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/integration/client"
	"github.com/grafana/loki/v3/integration/cluster"
)

func TestMultiTenantQuery(t *testing.T) {
	t.Skip("This test is flaky on CI but it's hardly reproducible locally.")
	clu := cluster.New(nil)
	defer func() {
		assert.NoError(t, clu.Cleanup())
	}()

	var (
		tAll = clu.AddComponent(
			"all",
			"-target=all",
		)
	)

	require.NoError(t, clu.Run())

	cliTenant1 := client.New("org1", "", tAll.HTTPURL())
	cliTenant2 := client.New("org2", "", tAll.HTTPURL())
	cliMultitenant := client.New("org1|org2", "", tAll.HTTPURL())

	// ingest log lines for tenant 1 and tenant 2.
	require.NoError(t, cliTenant1.PushLogLine("lineA", cliTenant1.Now.Add(-45*time.Minute), nil, map[string]string{"job": "fake1"}))
	require.NoError(t, cliTenant2.PushLogLine("lineB", cliTenant2.Now.Add(-45*time.Minute), nil, map[string]string{"job": "fake2"}))

	// check that tenant1 only have access to log line A.
	require.ElementsMatch(t, query(t, cliTenant1, `{job="fake2"}`), []string{})
	require.ElementsMatch(t, query(t, cliTenant1, `{job=~"fake.*"}`), []string{"lineA"})
	require.ElementsMatch(t, query(t, cliTenant1, `{job="fake1"}`), []string{"lineA"})

	// check that tenant2 only have access to log line B.
	require.ElementsMatch(t, query(t, cliTenant2, `{job="fake1"}`), []string{})
	require.ElementsMatch(t, query(t, cliTenant2, `{job=~"fake.*"}`), []string{"lineB"})
	require.ElementsMatch(t, query(t, cliTenant2, `{job="fake2"}`), []string{"lineB"})

	// check that multitenant has access to all log lines on same query.
	require.ElementsMatch(t, query(t, cliMultitenant, `{job=~"fake.*"}`), []string{"lineA", "lineB"})
	require.ElementsMatch(t, query(t, cliMultitenant, `{job="fake1"}`), []string{"lineA"})
	require.ElementsMatch(t, query(t, cliMultitenant, `{job="fake2"}`), []string{"lineB"})
	require.ElementsMatch(t, query(t, cliMultitenant, `{job="fake3"}`), []string{})
}

func query(t *testing.T, client *client.Client, labels string) []string {
	t.Helper()
	resp, err := client.RunRangeQueryWithStartEnd(context.Background(), labels, client.Now.Add(-1*time.Hour), client.Now)
	require.NoError(t, err)

	var lines []string
	for _, stream := range resp.Data.Stream {
		for _, val := range stream.Values {
			lines = append(lines, val[1])
		}
	}
	return lines
}
