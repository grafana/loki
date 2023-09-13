package integration

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/integration/client"
	"github.com/grafana/loki/integration/cluster"
)

func TestMultiTenantQuery(t *testing.T) {
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
	require.NoError(t, cliTenant1.PushLogLineWithTimestamp("lineA", cliTenant1.Now.Add(-45*time.Minute), map[string]string{"job": "fake1"}))
	require.NoError(t, cliTenant2.PushLogLineWithTimestamp("lineB", cliTenant2.Now.Add(-45*time.Minute), map[string]string{"job": "fake2"}))

	// check that tenant1 only have access to log line A.
	matchLines(t, cliTenant1, `{job="fake2"}`, []string{})
	matchLines(t, cliTenant1, `{job=~"fake.*"}`, []string{"lineA"})
	matchLines(t, cliTenant1, `{job="fake1"}`, []string{"lineA"})

	// check that tenant2 only have access to log line B.
	matchLines(t, cliTenant2, `{job="fake1"}`, []string{})
	matchLines(t, cliTenant2, `{job=~"fake.*"}`, []string{"lineB"})
	matchLines(t, cliTenant2, `{job="fake2"}`, []string{"lineB"})

	// check that multitenant has access to all log lines on same query.
	matchLines(t, cliMultitenant, `{job=~"fake.*"}`, []string{"lineA", "lineB"})
	matchLines(t, cliMultitenant, `{job="fake1"}`, []string{"lineA"})
	matchLines(t, cliMultitenant, `{job="fake2"}`, []string{"lineB"})
	matchLines(t, cliMultitenant, `{job="fake3"}`, []string{})
}

func matchLines(t *testing.T, client *client.Client, labels string, expectedLines []string) {
	resp, err := client.RunRangeQuery(context.Background(), labels)
	require.NoError(t, err)

	var lines []string
	for _, stream := range resp.Data.Stream {
		for _, val := range stream.Values {
			lines = append(lines, val[1])
		}
	}
	require.ElementsMatch(t, expectedLines, lines)
}
