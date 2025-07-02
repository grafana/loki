package integration

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/integration/client"
)

func TestCanWriteARequest(t *testing.T) {
	tenantID := "test-tenant-id"
	c := client.New(tenantID, "", "http://localhost:3100")
	require.NotNil(t, c)

	err := c.PushLogLine("test message", time.Now(), nil, nil)
	require.NoError(t, err)

	resp, err := c.RunRangeQuery(t.Context(), `{job=~".+"} |= "test message"`)
	require.NoError(t, err)
	require.NotNil(t, resp)
}
