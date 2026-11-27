package loki

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/ingester"
)

// recordingIngester records which HTTP handler served a request. The embedded
// interface is nil: only the HTTP handlers below are called.
type recordingIngester struct {
	ingester.Interface
	called string
}

func (r *recordingIngester) record(name string, w http.ResponseWriter) {
	r.called = name
	w.WriteHeader(http.StatusNoContent)
}

func (r *recordingIngester) FlushHandler(w http.ResponseWriter, _ *http.Request) {
	r.record("flush", w)
}

func (r *recordingIngester) FlushTenantHandler(w http.ResponseWriter, _ *http.Request) {
	r.record("flush_tenant", w)
}

func (r *recordingIngester) ShutdownHandler(w http.ResponseWriter, _ *http.Request) {
	r.record("shutdown", w)
}

func (r *recordingIngester) PrepareShutdown(w http.ResponseWriter, _ *http.Request) {
	r.record("prepare_shutdown", w)
}

func (r *recordingIngester) PreparePartitionDownscaleHandler(w http.ResponseWriter, _ *http.Request) {
	r.record("prepare_partition_downscale", w)
}

func TestRegisterIngesterHTTPRoutes(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		getForFlushAndShutdown bool
		method                 string
		path                   string
		expectedHandler        string // empty when the request must be rejected
		expectedCode           int
	}{
		{"GET /flush when enabled", true, http.MethodGet, "/flush", "flush", http.StatusNoContent},
		{"GET /flush when disabled", false, http.MethodGet, "/flush", "", http.StatusMethodNotAllowed},
		{"POST /flush when disabled", false, http.MethodPost, "/flush", "flush", http.StatusNoContent},
		{"GET /ingester/shutdown when enabled", true, http.MethodGet, "/ingester/shutdown", "shutdown", http.StatusNoContent},
		{"GET /ingester/shutdown when disabled", false, http.MethodGet, "/ingester/shutdown", "", http.StatusMethodNotAllowed},
		{"POST /ingester/shutdown when disabled", false, http.MethodPost, "/ingester/shutdown", "shutdown", http.StatusNoContent},
		{"GET /ingester/prepare_shutdown is unaffected", false, http.MethodGet, "/ingester/prepare_shutdown", "prepare_shutdown", http.StatusNoContent},
		{"GET /ingester/prepare_partition_downscale is unaffected", false, http.MethodGet, "/ingester/prepare_partition_downscale", "prepare_partition_downscale", http.StatusNoContent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i := &recordingIngester{}
			router := mux.NewRouter()
			registerIngesterHTTPRoutes(router, i, tc.getForFlushAndShutdown)

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))

			require.Equal(t, tc.expectedHandler, i.called)
			require.Equal(t, tc.expectedCode, rec.Code)
		})
	}
}

func TestRejectGETUnlessAllowedSetsAllowHeader(t *testing.T) {
	h := rejectGETUnlessAllowed(false).Wrap(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("the wrapped handler must not run for a rejected GET request")
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/flush", nil))

	require.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	require.Equal(t, http.MethodPost, rec.Header().Get("Allow"))
}
