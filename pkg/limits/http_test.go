package limits

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/quartz"
	"github.com/go-kit/log"
	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

func TestIngestLimits_ServeHTTP(t *testing.T) {
	clock := quartz.NewMock(t)
	store, err := newUsageStore(DefaultActiveWindow, DefaultRateWindow, DefaultBucketSize, 1, prometheus.NewRegistry())
	require.NoError(t, err)
	store.clock = clock
	store.setForTests("tenant1", streamUsage{
		hash:      0x1,
		totalSize: 100,
		rateBuckets: []rateBucket{{
			timestamp: clock.Now().UnixNano(),
			size:      1,
		}},
		lastSeenAt: clock.Now().UnixNano(),
	})
	s := Service{
		cfg: Config{
			ActiveWindow: time.Minute,
			RateWindow:   time.Minute,
			BucketSize:   30 * time.Second,
		},
		usage:  store,
		logger: log.NewNopLogger(),
	}

	// Set up a mux router for the test server otherwise mux.Vars() won't work.
	r := mux.NewRouter()
	r.Path("/{tenant}").Methods("GET").Handler(&s)
	ts := httptest.NewServer(r)
	defer ts.Close()

	// Known tenant should return current usage.
	resp, err := http.Get(ts.URL + "/tenant1")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	var data httpTenantLimitsResponse
	require.NoError(t, json.Unmarshal(b, &data))
	require.Equal(t, "tenant1", data.Tenant)
	require.Equal(t, uint64(1), data.Streams)
	require.Greater(t, data.Rate, 0.0)
	require.Less(t, data.Rate, 1.0)

	// Unknown tenant should have no usage.
	resp, err = http.Get(ts.URL + "/tenant2")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	b, err = io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(b, &data))
	require.Equal(t, "tenant2", data.Tenant)
	require.Equal(t, uint64(0), data.Streams)
	require.Equal(t, 0.0, data.Rate)
}
