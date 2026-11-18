package loki

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
)

// recordingIngesterHandlers records which handler served a request.
type recordingIngesterHandlers struct {
	called string
}

func (r *recordingIngesterHandlers) record(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		r.called = name
		w.WriteHeader(http.StatusNoContent)
	}
}

func (r *recordingIngesterHandlers) FlushHandler(w http.ResponseWriter, req *http.Request) {
	r.record("flush")(w, req)
}

func (r *recordingIngesterHandlers) FlushTenantHandler(w http.ResponseWriter, req *http.Request) {
	r.record("flush_tenant")(w, req)
}

func (r *recordingIngesterHandlers) ShutdownHandler(w http.ResponseWriter, req *http.Request) {
	r.record("shutdown")(w, req)
}

func (r *recordingIngesterHandlers) PrepareShutdown(w http.ResponseWriter, req *http.Request) {
	r.record("prepare_shutdown")(w, req)
}

func (r *recordingIngesterHandlers) PreparePartitionDownscaleHandler(w http.ResponseWriter, req *http.Request) {
	r.record("prepare_partition_downscale")(w, req)
}

func TestRegisterIngesterHTTPRoutes(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		getForFlushAndShutdown bool
		method                 string
		path                   string
		// expectedHandler is empty when the request must be rejected.
		expectedHandler string
	}{
		{"GET /flush when enabled", true, http.MethodGet, "/flush", "flush"},
		{"GET /flush when disabled", false, http.MethodGet, "/flush", ""},
		{"POST /flush when disabled", false, http.MethodPost, "/flush", "flush"},
		{"GET /ingester/shutdown when enabled", true, http.MethodGet, "/ingester/shutdown", "shutdown"},
		{"GET /ingester/shutdown when disabled", false, http.MethodGet, "/ingester/shutdown", ""},
		{"POST /ingester/shutdown when disabled", false, http.MethodPost, "/ingester/shutdown", "shutdown"},
		{"GET /flush/tenant is never accepted", true, http.MethodGet, "/flush/tenant", ""},
		{"GET /ingester/prepare_shutdown is unaffected", false, http.MethodGet, "/ingester/prepare_shutdown", "prepare_shutdown"},
		{"GET /ingester/prepare_partition_downscale is unaffected", false, http.MethodGet, "/ingester/prepare_partition_downscale", "prepare_partition_downscale"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handlers := &recordingIngesterHandlers{}
			router := mux.NewRouter()
			registerIngesterHTTPRoutes(router, handlers, tc.getForFlushAndShutdown)

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))

			require.Equal(t, tc.expectedHandler, handlers.called)
			if tc.expectedHandler == "" {
				// gorilla/mux answers 405 for a method mismatch, but 404 when a
				// later route accepts the method, so either code means rejected.
				require.Contains(t, []int{http.StatusNotFound, http.StatusMethodNotAllowed}, rec.Code)
			} else {
				require.Equal(t, http.StatusNoContent, rec.Code)
			}
		})
	}
}
