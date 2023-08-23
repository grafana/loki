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

func TestSingleBinaryIngestQuery(t *testing.T) {
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

	tenantID := randStringRunes()
	cli := client.New(tenantID, "", tAll.HTTPURL())

	t.Run("ingest-logs-store", func(t *testing.T) {
		// ingest some log lines
		require.NoError(t, cli.PushLogLineWithTimestamp("lineA", cli.Now.Add(-45*time.Minute), map[string]string{"job": "fake"}))
		require.NoError(t, cli.PushLogLineWithTimestamp("lineB", cli.Now.Add(-45*time.Minute), map[string]string{"job": "fake"}))

		// TODO: Flushing is currently causing a panic, as the boltdb shipper is shared using a global variable in:
		// https://github.com/grafana/loki/blob/66a4692423582ed17cce9bd86b69d55663dc7721/pkg/storage/factory.go#L32-L35
		// require.NoError(t, cli.Flush())
	})

	t.Run("ingest-logs-ingester", func(t *testing.T) {
		// ingest some log lines
		require.NoError(t, cli.PushLogLine("lineC", map[string]string{"job": "fake"}))
		require.NoError(t, cli.PushLogLine("lineD", map[string]string{"job": "fake"}))
	})

	t.Run("query", func(t *testing.T) {
		resp, err := cli.RunRangeQuery(context.Background(), `{job="fake"}`)
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
		resp, err := cli.LabelNames(context.Background())
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"job"}, resp)
	})

	t.Run("label-values", func(t *testing.T) {
		resp, err := cli.LabelValues(context.Background(), "job")
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"fake"}, resp)
	})
}
