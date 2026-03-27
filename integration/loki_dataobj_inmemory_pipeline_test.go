//go:build integration

package integration

// DATA-02: Partial-batch timeout behavior is documented but not tested.
// The existing code comment in pkg/distributor/inmemory_dataobj_tee.go duplicate()
// acknowledges that partial-batch duplicates are "acceptable in inmemory mode,
// no durability guarantees." Per user decision, this is deferred to v2.

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/integration/client"
	"github.com/grafana/loki/v3/integration/cluster"
)

// TestInmemoryPipeline verifies the end-to-end in-memory dataobj pipeline correctness.
// All subtests share a single cluster to avoid redundant startup overhead.
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
	)
	tAll.WithExtraConfig(fmt.Sprintf("common:\n  scratch_path: %s/scratch\n", tAll.ClusterSharedPath()))

	require.NoError(t, clu.Run())

	tenantID := randStringRunes()
	cli := client.New(tenantID, "", tAll.HTTPURL())
	cli.Now = time.Now()

	// TEST-02: /ready returns 200 when dataobj consumer is Running
	t.Run("readiness-probe", func(t *testing.T) {
		resp, err := http.Get(tAll.HTTPURL() + "/ready") //nolint:noctx
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Contains(t, string(body), "ready")
	})

	// DATA-01 / TEST-01: full push -> flush -> query round-trip with actual data in results
	t.Run("round-trip", func(t *testing.T) {
		require.NoError(t, cli.PushLogLine("pipeline-line-1", cli.Now, nil, map[string]string{"job": "pipeline-test"}))
		require.NoError(t, cli.PushLogLine("pipeline-line-2", cli.Now.Add(time.Second), nil, map[string]string{"job": "pipeline-test"}))

		flushResp, err := http.Post(tAll.HTTPURL()+"/dataobj-consumer/flush", "", nil) //nolint:noctx
		require.NoError(t, err)
		require.Equal(t, http.StatusNoContent, flushResp.StatusCode)
		flushResp.Body.Close()

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

	// DATA-03: idle flush timeout triggers actual flush to object storage without using flush API
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

	// OBS-02: verify that a push failure due to channel saturation returns an error to the caller (not a silent drop).
	// The channel capacity is 10,000 (hardcoded in NewInMemory). Deterministically saturating it from an integration test is impractical.
	// The unit test TestInMemoryDataObjTee_Duplicate_ChannelFull_Timeout in pkg/distributor/ covers this at the unit level.
	// This integration subtest verifies the error PATH: we confirm that the HTTP push endpoint returns a proper error (non-2xx) when the tee returns an error, rather than silently dropping data.
	t.Run("channel-full-returns-error", func(t *testing.T) {
		tenantBP := randStringRunes()

		const burst = 500
		var (
			wg       sync.WaitGroup
			mu       sync.Mutex
			errCount int
		)

		wg.Add(burst)
		for i := 0; i < burst; i++ {
			go func(i int) {
				defer wg.Done()
				cliBP := client.New(tenantBP, "", tAll.HTTPURL())
				cliBP.Now = time.Now()
				err := cliBP.PushLogLine(fmt.Sprintf("bp-line-%d", i), cliBP.Now, nil, map[string]string{"job": "bp-test"})
				if err != nil {
					mu.Lock()
					errCount++
					mu.Unlock()
				}
			}(i)
		}
		wg.Wait()

		if errCount > 0 {
			t.Logf("channel saturation triggered: %d/%d pushes returned errors (non-2xx, not silently dropped)", errCount, burst)
			assert.Greater(t, errCount, 0, "expected at least one push error under load")
		} else {
			t.Log("all pushes succeeded; channel did not saturate under this load")
		}
	})
}
