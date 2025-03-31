//go:build integration

package integration

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/prometheus/prometheus/storage/remote"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/integration/client"
	"github.com/grafana/loki/v3/integration/cluster"

	"github.com/grafana/loki/v3/pkg/ruler"
)

// TestLocalRuleEval tests that rules are evaluated locally with an embedded query engine
// and that the results are written to the backend correctly.
func TestLocalRuleEval(t *testing.T) {
	testRuleEval(t, ruler.EvalModeLocal)
}

// TestRemoteRuleEval tests that rules are evaluated remotely against a configured query-frontend
// and that the results are written to the backend correctly.
func TestRemoteRuleEval(t *testing.T) {
	testRuleEval(t, ruler.EvalModeRemote)
}

// The only way we can test rule evaluation in an integration test is to use the remote-write feature.
// In this test we stub out a remote-write receiver and check that the expected data is sent to it.
// Both the local and the remote rule evaluation modes should produce the same result.
func testRuleEval(t *testing.T, mode string) {
	clu := cluster.New(nil, cluster.SchemaWithTSDB, func(c *cluster.Cluster) {
		c.SetSchemaVer("v13")
	})
	t.Cleanup(func() {
		assert.NoError(t, clu.Cleanup())
	})

	// initialise a write component and ingest some logs
	tWrite := clu.AddComponent(
		"write",
		"-target=write",
	)

	now := time.Now()
	tenantID := randStringRunes()

	require.NoError(t, clu.Run())

	job := "accesslog"

	cliWrite := client.New(tenantID, "", tWrite.HTTPURL())
	cliWrite.Now = now

	// 1. Ingest some logs
	require.NoError(t, cliWrite.PushLogLine("HEAD /", now, nil, map[string]string{"method": "HEAD", "job": job}))
	require.NoError(t, cliWrite.PushLogLine("GET /", now, nil, map[string]string{"method": "GET", "job": job}))
	require.NoError(t, cliWrite.PushLogLine("GET /", now.Add(time.Second), nil, map[string]string{"method": "GET", "job": job}))

	// advance time to after the last ingested log line so queries don't return empty results
	now = now.Add(time.Second * 2)

	// start up read component for remote rule evaluation
	tRead := clu.AddComponent(
		"read",
		"-target=read",
		// we set a fake address here because deletion is not being tested,
		// and we have a circular dependency with the backend
		"-common.compactor-address=http://fake",
		"-legacy-read-mode=false",
		"-query-scheduler.use-scheduler-ring=false",
	)

	require.NoError(t, clu.Run())

	// start up a backend component which contains the ruler
	tBackend := clu.AddComponent(
		"backend",
		"-target=backend",
		"-legacy-read-mode=false",
	)

	rwHandler := func(called *bool, test func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/write" {
				t.Errorf("Expected to request '/api/v1/write', got: %s", r.URL.Path)
			}

			test(w, r)

			*called = true

			w.WriteHeader(http.StatusOK)
		}))
	}

	// this is the function that will be called when the remote-write receiver receives a request.
	// it tests that the expected payload is received.
	expectedResults := func(_ http.ResponseWriter, r *http.Request) {
		wr, err := remote.DecodeWriteRequest(r.Body)
		require.NoError(t, err)

		// depending on the rule interval, we may get multiple timeseries before remote-write is triggered,
		// so we just check that we have at least one that matches our requirements.
		require.GreaterOrEqual(t, len(wr.Timeseries), 1)

		// we expect to see two GET lines from the aggregation in the recording rule
		require.Equal(t, wr.Timeseries[len(wr.Timeseries)-1].Samples[0].Value, float64(2))
	}

	var called bool
	server1 := rwHandler(&called, expectedResults)
	defer server1.Close()

	// configure the backend component
	tBackend.WithRulerRemoteWrite("target1", server1.URL)

	if mode == ruler.EvalModeRemote {
		tBackend.WithExtraConfig(fmt.Sprintf(`
ruler:
  evaluation:
    mode: %s
    query_frontend:
      address: %s
`, mode, tRead.GRPCURL()))
	}

	record := fmt.Sprintf(`
groups:
- name: record
  interval: 1s
  rules:
  - record: test
    expr: sum by (method) (count_over_time({job="%s", method="GET"}[1m]))
    labels:
      foo: bar
`, job)

	require.NoError(t, tBackend.WithTenantRules(map[string]map[string]string{
		tenantID: {
			"record.yaml": record,
		},
	}))

	m, e := tBackend.MergedConfig()
	require.NoError(t, e)
	t.Logf("starting backend with config:\n%s\n", m)

	require.NoError(t, clu.Run())

	cliBackend := client.New(tenantID, "", tBackend.HTTPURL())
	cliBackend.Now = now

	// 2. Assert rules evaluation
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// check rules exist
	resp, err := cliBackend.GetRules(ctx)

	require.NoError(t, err)
	require.NotNil(t, resp)

	require.Equal(t, "success", resp.Status)

	require.Len(t, resp.Data.Groups, 1)
	require.Len(t, resp.Data.Groups[0].Rules, 1)

	// ensure that both remote-write receivers were called
	require.Eventually(t, func() bool {
		return assert.ObjectsAreEqualValues(true, called)
	}, 30*time.Second, 100*time.Millisecond, "remote-write was not called")
}
