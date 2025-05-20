package limits

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
)

func TestIngestLimits_ServeHTTP(t *testing.T) {
	l := IngestLimits{
		cfg: Config{
			ActiveWindow:   time.Minute,
			RateWindow:     time.Minute,
			BucketDuration: 30 * time.Second,
		},
		usage: &UsageStore{
			stripes: []map[string]tenantUsage{
				{
					"tenant": {
						0: {
							0x1: {
								Hash:      0x1,
								TotalSize: 100,
								RateBuckets: []RateBucket{{
									Timestamp: time.Now().UnixNano(),
									Size:      1,
								}},
								LastSeenAt: time.Now().UnixNano(),
							},
						},
					},
				},
			},
			locks: make([]stripeLock, 1),
		},
		logger: log.NewNopLogger(),
		partitionManager: &PartitionManager{
			partitions: map[int32]partitionEntry{
				0: {
					assignedAt: time.Now().UnixNano(),
				},
			},
		},
	}

	// Set up a mux router for the test server otherwise mux.Vars() won't work.
	r := mux.NewRouter()
	r.Path("/{tenant}").Methods("GET").Handler(&l)
	ts := httptest.NewServer(r)
	defer ts.Close()

	// Unknown tenant should have no usage.
	resp, err := http.Get(ts.URL + "/unknown_tenant")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	var data httpTenantLimitsResponse
	require.NoError(t, json.Unmarshal(b, &data))
	require.Equal(t, "unknown_tenant", data.Tenant)
	require.Equal(t, uint64(0), data.ActiveStreams)
	require.Equal(t, 0.0, data.Rate)

	// Known tenant should return current usage.
	resp, err = http.Get(ts.URL + "/tenant")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	b, err = io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(b, &data))
	require.Equal(t, "tenant", data.Tenant)
	require.Equal(t, uint64(1), data.ActiveStreams)
	require.Greater(t, data.Rate, 0.0)
	require.Less(t, data.Rate, 1.0)
}
