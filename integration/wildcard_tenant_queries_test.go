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

// TestWildcardTenantQuery tests wildcard tenant query functionality.
// This test requires wildcard_tenant_queries_enabled and multi_tenant_queries_enabled
// to be true in the querier config.
func TestWildcardTenantQuery(t *testing.T) {
	// Wildcard tenant queries are an experimental feature

	clu := cluster.New(nil)
	defer func() {
		assert.NoError(t, clu.Cleanup())
	}()

	var (
		// Start Loki with multi-tenant and wildcard queries enabled
		tAll = clu.AddComponent(
			"all",
			"-target=all",
			"-querier.multi-tenant-queries-enabled=true",
			"-querier.wildcard-tenant-queries-enabled=true",
			"-querier.wildcard-tenant-cache-ttl=10s",
		)
	)

	require.NoError(t, clu.Run())

	// Create clients for different tenants
	cliTenant1 := client.New("tenant1", "", tAll.HTTPURL())
	cliTenant2 := client.New("tenant2", "", tAll.HTTPURL())
	cliTenant3 := client.New("tenant3", "", tAll.HTTPURL())

	// Wildcard client - queries all tenants
	cliWildcard := client.New("*", "", tAll.HTTPURL())

	// Wildcard with exclusion - all tenants except tenant2
	cliWildcardExclude := client.New("*|!tenant2", "", tAll.HTTPURL())

	// Ingest log lines for each tenant
	now := time.Now()
	require.NoError(t, cliTenant1.PushLogLine("log from tenant1", now.Add(-45*time.Minute), nil, map[string]string{"job": "app1", "level": "info"}))
	require.NoError(t, cliTenant2.PushLogLine("log from tenant2", now.Add(-44*time.Minute), nil, map[string]string{"job": "app2", "level": "error"}))
	require.NoError(t, cliTenant3.PushLogLine("log from tenant3", now.Add(-43*time.Minute), nil, map[string]string{"job": "app3", "level": "warn"}))

	// Wait for data to be ingested
	time.Sleep(2 * time.Second)

	// Test 1: Each tenant should only see their own logs
	t.Run("tenant isolation", func(t *testing.T) {
		require.ElementsMatch(t, queryLines(t, cliTenant1, `{job=~".+"}`), []string{"log from tenant1"})
		require.ElementsMatch(t, queryLines(t, cliTenant2, `{job=~".+"}`), []string{"log from tenant2"})
		require.ElementsMatch(t, queryLines(t, cliTenant3, `{job=~".+"}`), []string{"log from tenant3"})
	})

	// Test 2: Wildcard query should return logs from all tenants
	t.Run("wildcard query all tenants", func(t *testing.T) {
		lines := queryLines(t, cliWildcard, `{job=~".+"}`)
		require.ElementsMatch(t, lines, []string{
			"log from tenant1",
			"log from tenant2",
			"log from tenant3",
		})
	})

	// Test 3: Wildcard with exclusion should exclude specified tenants
	t.Run("wildcard with exclusion", func(t *testing.T) {
		lines := queryLines(t, cliWildcardExclude, `{job=~".+"}`)
		require.ElementsMatch(t, lines, []string{
			"log from tenant1",
			"log from tenant3",
		})
		// Verify tenant2's log is not included
		require.NotContains(t, lines, "log from tenant2")
	})

	// Test 4: Wildcard with label filter
	t.Run("wildcard with label filter", func(t *testing.T) {
		lines := queryLines(t, cliWildcard, `{level="error"}`)
		require.ElementsMatch(t, lines, []string{"log from tenant2"})
	})

	// Test 5: Wildcard with multiple exclusions
	t.Run("wildcard with multiple exclusions", func(t *testing.T) {
		cliMultiExclude := client.New("*|!tenant1|!tenant2", "", tAll.HTTPURL())
		lines := queryLines(t, cliMultiExclude, `{job=~".+"}`)
		require.ElementsMatch(t, lines, []string{"log from tenant3"})
	})

	// Test 6: Verify __tenant_id__ label is added
	t.Run("tenant label added", func(t *testing.T) {
		resp, err := cliWildcard.RunRangeQueryWithStartEnd(
			context.Background(),
			`{job=~".+"}`,
			now.Add(-1*time.Hour),
			now,
		)
		require.NoError(t, err)

		tenantLabels := make(map[string]bool)
		for _, stream := range resp.Data.Stream {
			if tenantID, ok := stream.Stream["__tenant_id__"]; ok {
				tenantLabels[tenantID] = true
			}
		}

		require.True(t, tenantLabels["tenant1"], "should have __tenant_id__=tenant1")
		require.True(t, tenantLabels["tenant2"], "should have __tenant_id__=tenant2")
		require.True(t, tenantLabels["tenant3"], "should have __tenant_id__=tenant3")
	})
}

// TestWildcardTenantLabelQuery tests that labels endpoint works with wildcard
func TestWildcardTenantLabelQuery(t *testing.T) {
	// Wildcard tenant queries are an experimental feature

	clu := cluster.New(nil)
	defer func() {
		assert.NoError(t, clu.Cleanup())
	}()

	var (
		tAll = clu.AddComponent(
			"all",
			"-target=all",
			"-querier.multi-tenant-queries-enabled=true",
			"-querier.wildcard-tenant-queries-enabled=true",
		)
	)

	require.NoError(t, clu.Run())

	// Create and push data for tenants
	cliTenant1 := client.New("tenant1", "", tAll.HTTPURL())
	cliTenant2 := client.New("tenant2", "", tAll.HTTPURL())

	now := time.Now()
	require.NoError(t, cliTenant1.PushLogLine("line1", now.Add(-30*time.Minute), nil, map[string]string{"app": "frontend", "env": "prod"}))
	require.NoError(t, cliTenant2.PushLogLine("line2", now.Add(-30*time.Minute), nil, map[string]string{"app": "backend", "env": "staging"}))

	time.Sleep(2 * time.Second)

	// Query labels with wildcard
	cliWildcard := client.New("*", "", tAll.HTTPURL())

	t.Run("label names include tenant label", func(t *testing.T) {
		labels, err := cliWildcard.LabelNames(context.Background())
		require.NoError(t, err)
		require.Contains(t, labels, "__tenant_id__")
		require.Contains(t, labels, "app")
		require.Contains(t, labels, "env")
	})

	t.Run("label values for app include all tenants", func(t *testing.T) {
		values, err := cliWildcard.LabelValues(context.Background(), "app")
		require.NoError(t, err)
		require.ElementsMatch(t, values, []string{"frontend", "backend"})
	})

	t.Run("label values for tenant returns all tenants", func(t *testing.T) {
		values, err := cliWildcard.LabelValues(context.Background(), "__tenant_id__")
		require.NoError(t, err)
		require.Contains(t, values, "tenant1")
		require.Contains(t, values, "tenant2")
	})
}

// queryLines is a helper that extracts log lines from a query response
func queryLines(t *testing.T, cli *client.Client, query string) []string {
	t.Helper()

	now := time.Now()
	resp, err := cli.RunRangeQueryWithStartEnd(
		context.Background(),
		query,
		now.Add(-1*time.Hour),
		now,
	)
	require.NoError(t, err)

	var lines []string
	for _, stream := range resp.Data.Stream {
		for _, val := range stream.Values {
			lines = append(lines, val[1])
		}
	}
	return lines
}
