package scheduler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-kit/log"
	"github.com/gorilla/mux"
	"github.com/grafana/dskit/server"
	"github.com/grafana/dskit/services"
	"github.com/stretchr/testify/require"

	schedulerapi "github.com/grafana/loki/v3/pkg/logline/scheduler/api"
)

func TestRegisterHandlers_ServesCurrentAndLegacyRoutes(t *testing.T) {
	svc, _, _ := newTestService(t)
	srv := &server.Server{HTTP: mux.NewRouter()}
	require.NoError(t, RegisterHandlers(srv, svc, log.NewNopLogger()))
	require.NoError(t, services.StartAndAwaitRunning(context.Background(), svc))
	t.Cleanup(func() {
		require.NoError(t, services.StopAndAwaitTerminated(context.Background(), svc))
	})

	for _, path := range []string{
		schedulerapi.JobPath,
		schedulerapi.LegacyJobPath,
	} {
		// Without worker_id the job handler answers 400, which proves the
		// route reached it.
		rec := httptest.NewRecorder()
		srv.HTTP.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusBadRequest, rec.Code, path)
	}
	for _, path := range []string{
		schedulerapi.AbandonPath,
		schedulerapi.LegacyAbandonPath,
	} {
		rec := httptest.NewRecorder()
		srv.HTTP.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, nil))
		require.Equal(t, http.StatusBadRequest, rec.Code, path)
	}
	for _, path := range []string{
		schedulerapi.StatusPath,
		schedulerapi.LegacyStatusPath,
		schedulerapi.StoreDumpPath,
		schedulerapi.LegacyStoreDumpPath,
	} {
		rec := httptest.NewRecorder()
		srv.HTTP.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusOK, rec.Code, path)
	}
}

func TestRegisterHandlers_UnavailableUntilRunning(t *testing.T) {
	svc, _, _ := newTestService(t)
	// Before starting() runs, the job store does not exist yet.
	svc.jobs = nil
	srv := &server.Server{HTTP: mux.NewRouter()}
	require.NoError(t, RegisterHandlers(srv, svc, log.NewNopLogger()))

	for _, req := range []struct{ method, path string }{
		{http.MethodGet, schedulerapi.JobPath + "?worker_id=w1"},
		{http.MethodGet, schedulerapi.LegacyJobPath + "?worker_id=w1"},
		{http.MethodPost, schedulerapi.AbandonPath + "?worker_id=w1&job_id=j1"},
		{http.MethodPost, schedulerapi.LegacyAbandonPath + "?worker_id=w1&job_id=j1"},
		{http.MethodGet, schedulerapi.StatusPath},
		{http.MethodGet, schedulerapi.LegacyStatusPath},
		{http.MethodGet, schedulerapi.StoreDumpPath},
		{http.MethodGet, schedulerapi.LegacyStoreDumpPath},
	} {
		rec := httptest.NewRecorder()
		srv.HTTP.ServeHTTP(rec, httptest.NewRequest(req.method, req.path, nil))
		require.Equal(t, http.StatusServiceUnavailable, rec.Code, req.path)
	}
}

func TestHandleStoreDump_EmptyStore_Markdown(t *testing.T) {
	svc, _, _ := newTestService(t)

	req := httptest.NewRequest(http.MethodGet, schedulerapi.StoreDumpPath, nil)
	rec := httptest.NewRecorder()

	svc.handleStoreDump(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "text/markdown; charset=utf-8", rec.Header().Get("Content-Type"))
	require.Contains(t, rec.Body.String(), "# Index Store")
	require.Contains(t, rec.Body.String(), "_No indexes._")
}

func TestHandleStoreDump_EmptyStore_JSON(t *testing.T) {
	svc, _, _ := newTestService(t)

	req := httptest.NewRequest(http.MethodGet, schedulerapi.StoreDumpPath, nil)
	req.Header.Set("Accept", "application/json")
	rec := httptest.NewRecorder()

	svc.handleStoreDump(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var entries []storeDumpEntry
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&entries))
	require.Empty(t, entries)
}

func TestHandleStoreDump_AllIndexes(t *testing.T) {
	svc, indexStore, bucket := newTestService(t)

	populateBucket(t, bucket, "2024-01-01", "hash1hash1hash1h", "hash2hash2hash2h")
	populateMergedBucket(t, bucket, "2024-01-01", "mrg1mrg1mrg1mrg1", []string{
		"2024-01-01/hash1hash1hash1h",
	})
	pollStore(t, indexStore)

	req := httptest.NewRequest(http.MethodGet, schedulerapi.StoreDumpPath, nil)
	req.Header.Set("Accept", "application/json")
	rec := httptest.NewRecorder()

	svc.handleStoreDump(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var entries []storeDumpEntry
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&entries))
	require.Len(t, entries, 3)

	// Verify states.
	states := make(map[string]string)
	for _, e := range entries {
		states[e.ID] = e.State
	}
	require.Equal(t, "compacted", states["2024-01-01/hash1hash1hash1h"])
	require.Equal(t, "active", states["2024-01-01/hash2hash2hash2h"])
	require.Equal(t, "active", states["2024-01-01/mrg1mrg1mrg1mrg1"])
}

func TestHandleStoreDump_ActiveOnly(t *testing.T) {
	svc, indexStore, bucket := newTestService(t)

	populateBucket(t, bucket, "2024-01-01", "hash1hash1hash1h", "hash2hash2hash2h")
	populateMergedBucket(t, bucket, "2024-01-01", "mrg1mrg1mrg1mrg1", []string{
		"2024-01-01/hash1hash1hash1h",
	})
	pollStore(t, indexStore)

	req := httptest.NewRequest(http.MethodGet, schedulerapi.StoreDumpPath+"?"+schedulerapi.ParamState+"=active", nil)
	req.Header.Set("Accept", "application/json")
	rec := httptest.NewRecorder()

	svc.handleStoreDump(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var entries []storeDumpEntry
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&entries))
	require.Len(t, entries, 2)

	for _, e := range entries {
		require.Equal(t, "active", e.State)
	}
}

func TestHandleStoreDump_CompactedOnly(t *testing.T) {
	svc, indexStore, bucket := newTestService(t)

	populateBucket(t, bucket, "2024-01-01", "hash1hash1hash1h", "hash2hash2hash2h")
	populateMergedBucket(t, bucket, "2024-01-01", "mrg1mrg1mrg1mrg1", []string{
		"2024-01-01/hash1hash1hash1h",
	})
	pollStore(t, indexStore)

	req := httptest.NewRequest(http.MethodGet, schedulerapi.StoreDumpPath+"?"+schedulerapi.ParamState+"=compacted", nil)
	req.Header.Set("Accept", "application/json")
	rec := httptest.NewRecorder()

	svc.handleStoreDump(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var entries []storeDumpEntry
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&entries))
	require.Len(t, entries, 1)
	require.Equal(t, "2024-01-01/hash1hash1hash1h", entries[0].ID)
	require.Equal(t, "compacted", entries[0].State)
}

func TestHandleStoreDump_InvalidState(t *testing.T) {
	svc, _, _ := newTestService(t)

	req := httptest.NewRequest(http.MethodGet, schedulerapi.StoreDumpPath+"?"+schedulerapi.ParamState+"=bogus", nil)
	rec := httptest.NewRecorder()

	svc.handleStoreDump(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "invalid state")
}

func TestHandleStoreDump_MarkdownHasTable(t *testing.T) {
	svc, indexStore, bucket := newTestService(t)

	populateBucket(t, bucket, "2024-01-01", "hash1hash1hash1h")
	pollStore(t, indexStore)

	req := httptest.NewRequest(http.MethodGet, schedulerapi.StoreDumpPath, nil)
	rec := httptest.NewRecorder()

	svc.handleStoreDump(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, "# Index Store")
	require.Contains(t, body, "| ID |")
	require.Contains(t, body, "hash1hash1hash1h")
	require.Contains(t, body, "**Count:** 1")
}

func TestHandleStoreDump_CompactedFromInJSON(t *testing.T) {
	svc, indexStore, bucket := newTestService(t)

	sources := []string{"2024-01-01/hash1hash1hash1h", "2024-01-01/hash2hash2hash2h"}
	populateBucket(t, bucket, "2024-01-01", "hash1hash1hash1h", "hash2hash2hash2h")
	populateMergedBucket(t, bucket, "2024-01-01", "mrg1mrg1mrg1mrg1", sources)
	pollStore(t, indexStore)

	req := httptest.NewRequest(http.MethodGet, schedulerapi.StoreDumpPath, nil)
	req.Header.Set("Accept", "application/json")
	rec := httptest.NewRecorder()

	svc.handleStoreDump(rec, req)

	var entries []storeDumpEntry
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&entries))

	// Find the merged entry and verify CompactedFrom.
	var merged *storeDumpEntry
	for i, e := range entries {
		if e.ID == "2024-01-01/mrg1mrg1mrg1mrg1" {
			merged = &entries[i]
			break
		}
	}
	require.NotNil(t, merged)
	require.ElementsMatch(t, sources, merged.CompactedFrom)
}

func TestHandleStoreDump_SortedByDateThenID(t *testing.T) {
	svc, indexStore, bucket := newTestService(t)

	populateBucket(t, bucket, "2024-01-02", "zzzzzzzzzzzzzzzz")
	populateBucket(t, bucket, "2024-01-01", "bbbbbbbbbbbbbbbb", "aaaaaaaaaaaaaaaa")
	pollStore(t, indexStore)

	req := httptest.NewRequest(http.MethodGet, schedulerapi.StoreDumpPath, nil)
	req.Header.Set("Accept", "application/json")
	rec := httptest.NewRecorder()

	svc.handleStoreDump(rec, req)

	var entries []storeDumpEntry
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&entries))
	require.Len(t, entries, 3)

	require.Equal(t, "2024-01-01/aaaaaaaaaaaaaaaa", entries[0].ID)
	require.Equal(t, "2024-01-01/bbbbbbbbbbbbbbbb", entries[1].ID)
	require.Equal(t, "2024-01-02/zzzzzzzzzzzzzzzz", entries[2].ID)
}

func TestFormatBytes(t *testing.T) {
	require.Equal(t, "—", formatBytes(0))
	require.Equal(t, "512 B", formatBytes(512))
	require.Equal(t, "1.5 KiB", formatBytes(1536))
	require.Equal(t, "2.0 MiB", formatBytes(2*1024*1024))
	require.Equal(t, "1.0 GiB", formatBytes(1024*1024*1024))
}
