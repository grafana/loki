// +build requires_docker

package e2e

import (
	"path/filepath"
	"testing"

	"github.com/cortexproject/cortex/integration/e2e"
	e2eCortex "github.com/cortexproject/cortex/integration/e2e"
	e2edb "github.com/cortexproject/cortex/integration/e2e/db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/test/e2e/services"
)

const (
	singleProcessName = "loki-e2e-test-all"
	instanceName      = "test_instance"
)

//runLokiWithConfig runs a single instance of loki in a docker container with the provided config
func runLokiWithConfig(t *testing.T, s *e2eCortex.Scenario, cfg string) (loki *e2e.HTTPService, client *Client) {
	// Start dependencies.
	minio := e2edb.NewMinio(9000, bucketName)
	minioAdmin := e2edb.NewMinio(9001, bucketName)
	require.NoError(t, s.StartAndWaitReady(minio, minioAdmin))

	require.NoError(t, writeFileToSharedDir(s, "config.yaml", []byte(cfg)))

	// Start Enterprise Logs.
	loki = services.NewLoki(singleProcessName, map[string]string{
		"-config.file": filepath.Join(e2eCortex.ContainerSharedDir, "config.yaml"),
	})

	// In Drone the shared dir is mounted by the root user.
	loki.SetUser("root")
	require.NoError(t, s.StartAndWaitReady(loki))

	err := loki.WaitSumMetrics(e2eCortex.Equals(1), "loki_build_info")
	require.NoError(t, err)

	return loki, NewLogsClient(instanceName, loki.HTTPEndpoint())
}

func TestLokiAllTarget(t *testing.T) {
	s, err := e2eCortex.NewScenario(defaultNetworkName)
	require.NoError(t, err)

	loki, client := runLokiWithConfig(t, s, defaultConfig)
	assert.NotNil(t, loki)

	// Push a log line
	err = client.PushLogLine("line1")
	require.NoError(t, err)
	err = loki.WaitSumMetrics(e2eCortex.Equals(1), "loki_ingester_streams_created_total")
	require.NoError(t, err)

	// Run a range query.
	output, err := client.RunRangeQuery(`{job="varlog"} |= "line"`)
	require.NoError(t, err)
	require.Equal(t, "success", output.Status)
	require.Equal(t, "streams", output.Data.ResultType)
	require.Len(t, output.Data.Result, 1)
	require.Equal(t, "line1", output.Data.Result[0].Values[0][1])

	// Run an instant query.
	output, err = client.RunQuery(`{job="varlog"} |= "line"`)
	require.NoError(t, err)
	require.Equal(t, "success", output.Status)
	require.Equal(t, "streams", output.Data.ResultType)
	require.Len(t, output.Data.Result, 1)
	require.Equal(t, "line1", output.Data.Result[0].Values[0][1])
}
