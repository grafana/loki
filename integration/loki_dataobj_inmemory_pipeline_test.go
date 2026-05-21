//go:build integration

package integration

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/integration/client"
	"github.com/grafana/loki/v3/integration/cluster"
)

func TestInmemoryPipeline(t *testing.T) {
	clu := cluster.New(nil, cluster.SchemaWithTSDB, func(c *cluster.Cluster) {
		c.SetSchemaVer("v13")
	})
	defer func() {
		assert.NoError(t, clu.Cleanup())
	}()

	tAll := clu.AddComponent(
		"all",
		"-target=all",
		"-dataobj.enabled=true",
		"-dataobj-consumer.ingest-mode=inmemory",
		"-dataobj-consumer.target-page-size=2KB",
		"-dataobj-consumer.target-builder-memory-limit=1MB",
		"-dataobj-consumer.buffer-size=256KB",
		"-dataobj-consumer.target-section-size=512KB",
		"-dataobj-consumer.section-stripe-merge-limit=2",
		"-dataobj-consumer.sha-prefix-size=2",
		"-dataobj-consumer.idle-flush-timeout=100ms",
		"-distributor.ingester-writes-enabled=false",
		"-pattern-ingester.enabled=false",
		"-query-engine.enable=true",
		"-query-engine.storage-lag=0",
	)
	scratchPath := tAll.ClusterSharedPath() + "/scratch"
	require.NoError(t, os.MkdirAll(scratchPath, 0o755))
	tAll.WithExtraConfig(fmt.Sprintf(`
common:
  scratch_path: %s
  ring:
    kvstore:
      store: inmemory
ingest_limits:
  enabled: false
`, scratchPath))

	require.NoError(t, clu.Run())

	tenantID := randStringRunes()
	cli := client.New(tenantID, "", tAll.HTTPURL())
	cli.Now = time.Now().Add(-30 * time.Second)

	t.Run("round-trip", func(t *testing.T) {
		require.NoError(t, cli.PushLogLine("pipeline-line-1", cli.Now, nil, map[string]string{"job": "pipeline-test"}))
		require.NoError(t, cli.PushLogLine("pipeline-line-2", cli.Now.Add(-time.Second), nil, map[string]string{"job": "pipeline-test"}))

		require.NoError(t, cli.FlushDataobj())

		var lines []string
		require.Eventually(t, func() bool {
			qresp, err := cli.RunRangeQuery(context.Background(), `{job="pipeline-test"}`)
			if err != nil {
				return false
			}
			lines = nil
			for _, stream := range qresp.Data.Stream {
				for _, val := range stream.Values {
					lines = append(lines, val[1])
				}
			}
			return len(lines) >= 2
		}, 30*time.Second, 100*time.Millisecond, "pushed logs never appeared in query results")

		assert.ElementsMatch(t, []string{"pipeline-line-1", "pipeline-line-2"}, lines)
	})

	t.Run("idle-flush-timeout", func(t *testing.T) {
		tenantIdle := randStringRunes()
		cliIdle := client.New(tenantIdle, "", tAll.HTTPURL())
		cliIdle.Now = time.Now()

		require.NoError(t, cliIdle.PushLogLine("idle-flush-line", cliIdle.Now, nil, map[string]string{"job": "idle-test"}))

		// Do NOT call the flush endpoint — let the idle flush timeout (100ms) trigger naturally.
		var lines []string
		require.Eventually(t, func() bool {
			qresp, err := cliIdle.RunRangeQuery(context.Background(), `{job="idle-test"}`)
			if err != nil {
				return false
			}
			lines = nil
			for _, stream := range qresp.Data.Stream {
				for _, val := range stream.Values {
					lines = append(lines, val[1])
				}
			}
			return len(lines) >= 1
		}, 15*time.Second, 200*time.Millisecond, "idle flush did not trigger within timeout")

		assert.Contains(t, lines, "idle-flush-line")
	})
}
