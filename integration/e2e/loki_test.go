//go:build requires_docker

// These tests need a running Docker daemon plus a Loki image.
//
// For example:
//
//	LOKI_IMAGE=grafana/loki:main-89ea0d4 go test -tags requires_docker ./integration/e2e/...
package e2e

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/grafana/e2e"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/integration/client"
)

// TestSingleBinaryPushAndQuery starts an all-in-one Loki container, pushes a log
// line over the HTTP push API, and queries it back via query_range.
func TestSingleBinaryPushAndQuery(t *testing.T) {
	s, err := e2e.NewScenario("loki-e2e")
	require.NoError(t, err)
	defer s.Close()

	// Write the Loki config into the shared directory mounted at /shared.
	require.NoError(t, os.WriteFile(filepath.Join(s.SharedDir(), "config.yaml"), []byte(singleBinaryConfig), 0o644))

	loki := NewSingleBinary("loki")
	require.NoError(t, s.StartAndWaitReady(loki))

	c := client.New("fake", "", "http://"+loki.HTTPEndpoint())

	const logLine = "hello e2e world"
	labels := map[string]string{"task": "e2e-test"}
	pushedAt := time.Now()

	require.NoError(t, c.PushLogLine(logLine, pushedAt, nil, labels))

	// The push is acknowledged once it's in the ingester, but it may take a
	// moment to become queryable, so poll.
	require.Eventually(t, func() bool {
		resp, err := c.RunRangeQueryWithStartEnd(t.Context(), `{task="e2e-test"}`, pushedAt.Add(-time.Minute), pushedAt.Add(time.Minute))
		if err != nil {
			t.Logf("query_range error (will retry): %v", err)
			return false
		}
		assert.Equal(t, "streams", resp.Data.ResultType)
		for _, stream := range resp.Data.Stream {
			for _, val := range stream.Values {
				if val[1] == logLine {
					return true
				}
			}
		}
		return false
	}, 30*time.Second, 500*time.Millisecond, "pushed log line was not returned by query_range")
}
