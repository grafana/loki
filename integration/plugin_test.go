package integration

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/integration/client"
)

func TestCanWriteARequest(t *testing.T) {
	tenantID := "test-tenant-id"
	c := client.New(tenantID, "", "http://localhost:3100")
	require.NotNil(t, c)

	now := time.Now()
	err := c.PushLogLine("test message", time.Now(), nil, nil)
	require.NoError(t, err)

	resp, err := c.RunRangeQueryWithStartEnd(t.Context(), `{job=~".+"} |= "test message"`, now, time.Now())
	require.NoError(t, err)
	require.NotNil(t, resp)

	json, err := json.Marshal(resp)
	t.Logf("Response: %v", string(json))
}
