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

func TestSimpleScalable_IngestQuery(t *testing.T) {
	clu := cluster.New(nil, cluster.SchemaWithTSDB, func(c *cluster.Cluster) {
		c.SetSchemaVer("v13")
	})
	defer func() {
		assert.NoError(t, clu.Cleanup())
	}()

	var (
		tWrite = clu.AddComponent(
			"write",
			"-target=write",
		)
		tBackend = clu.AddComponent(
			"backend",
			"-target=backend",
			"-legacy-read-mode=false",
		)
	)
	require.NoError(t, clu.Run())

	tRead := clu.AddComponent(
		"read",
		"-target=read",
		"-common.compactor-address="+tBackend.HTTPURL(),
		"-legacy-read-mode=false",
	)
	require.NoError(t, clu.Run())

	tenantID := randStringRunes()

	now := time.Now()
	cliWrite := client.New(tenantID, "", tWrite.HTTPURL())
	cliWrite.Now = now
	cliRead := client.New(tenantID, "", tRead.HTTPURL())
	cliRead.Now = now
	cliBackend := client.New(tenantID, "", tBackend.HTTPURL())
	cliBackend.Now = now

	t.Run("ingest logs", func(t *testing.T) {
		// ingest some log lines
		require.NoError(t, cliWrite.PushLogLine("lineA", now.Add(-45*time.Minute), nil, map[string]string{"job": "fake"}))
		require.NoError(t, cliWrite.PushLogLine("lineB", now.Add(-45*time.Minute), nil, map[string]string{"job": "fake"}))

		require.NoError(t, cliWrite.PushLogLine("lineC", now, nil, map[string]string{"job": "fake"}))
		require.NoError(t, cliWrite.PushLogLine("lineD", now, nil, map[string]string{"job": "fake"}))
	})

	t.Run("query", func(t *testing.T) {
		resp, err := cliRead.RunRangeQuery(context.Background(), `{job="fake"}`)
		require.NoError(t, err)
		assert.Equal(t, "streams", resp.Data.ResultType)

		var lines []string
		for _, stream := range resp.Data.Stream {
			for _, val := range stream.Values {
				lines = append(lines, val[1])
			}
		}
		assert.ElementsMatch(t, []string{"lineA", "lineB", "lineC", "lineD"}, lines)
	})

	t.Run("label-names", func(t *testing.T) {
		resp, err := cliRead.LabelNames(context.Background())
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"job"}, resp)
	})

	t.Run("label-values", func(t *testing.T) {
		resp, err := cliRead.LabelValues(context.Background(), "job")
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"fake"}, resp)
	})
}
