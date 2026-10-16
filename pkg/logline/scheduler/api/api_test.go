package schedulerapi

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewJobRequest_WithLastCompletedJobID(t *testing.T) {
	req, err := NewJobRequest(context.Background(), "http://scheduler:3100", "w1", "job123")
	require.NoError(t, err)

	require.Equal(t, LegacyJobPath, req.URL.Path)
	require.Equal(t, "w1", req.URL.Query().Get(ParamWorkerID))
	require.Equal(t, "job123", req.URL.Query().Get(ParamLastCompletedJobID))
}

func TestNewJobRequest_WithoutLastCompletedJobID(t *testing.T) {
	req, err := NewJobRequest(context.Background(), "http://scheduler:3100", "w1", "")
	require.NoError(t, err)

	require.Equal(t, "w1", req.URL.Query().Get(ParamWorkerID))
	require.Equal(t, "", req.URL.Query().Get(ParamLastCompletedJobID))
}

func TestLastCompletedJobID(t *testing.T) {
	req := httptest.NewRequest("GET", JobPath+"?"+ParamWorkerID+"=w1&"+ParamLastCompletedJobID+"=job456", nil)
	require.Equal(t, "job456", LastCompletedJobID(req))
}

func TestLastCompletedJobID_Missing(t *testing.T) {
	req := httptest.NewRequest("GET", JobPath+"?"+ParamWorkerID+"=w1", nil)
	require.Equal(t, "", LastCompletedJobID(req))
}

func TestNewAbandonRequest(t *testing.T) {
	req, err := NewAbandonRequest(context.Background(), "http://scheduler:3100", "w1", "job789")
	require.NoError(t, err)

	require.Equal(t, "POST", req.Method)
	require.Equal(t, "w1", req.URL.Query().Get(ParamWorkerID))
	require.Equal(t, "job789", req.URL.Query().Get(ParamJobID))
	require.Equal(t, LegacyAbandonPath, req.URL.Path)
}

func TestJobID(t *testing.T) {
	req := httptest.NewRequest("POST", AbandonPath+"?"+ParamWorkerID+"=w1&"+ParamJobID+"=job123", nil)
	require.Equal(t, "job123", JobID(req))
}

func TestJobID_Missing(t *testing.T) {
	req := httptest.NewRequest("POST", AbandonPath+"?"+ParamWorkerID+"=w1", nil)
	require.Equal(t, "", JobID(req))
}
