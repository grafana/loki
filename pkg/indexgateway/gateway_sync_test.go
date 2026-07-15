package indexgateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/stores/index"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

// syncerQuerierMock is an IndexQuerier that also implements index.Syncer, so the
// gateway's type assertion succeeds and the sync handlers exercise the real path.
type syncerQuerierMock struct {
	IndexQuerier
	triggerResult bool
	triggered     int
	statuses      []index.SyncStatus
}

func (m *syncerQuerierMock) TriggerSync() bool {
	m.triggered++
	return m.triggerResult
}

func (m *syncerQuerierMock) SyncStatuses() []index.SyncStatus { return m.statuses }

func newSyncTestGateway(t *testing.T, q IndexQuerier) *Gateway {
	t.Helper()
	gateway, err := NewIndexGateway(Config{}, mockLimits{}, util_log.Logger, nil, q, nil, nil)
	require.NoError(t, err)
	return gateway
}

func TestSyncIndexesHandler(t *testing.T) {
	t.Run("starts a sync", func(t *testing.T) {
		m := &syncerQuerierMock{triggerResult: true, statuses: []index.SyncStatus{{Name: "p1"}}}
		rec := httptest.NewRecorder()
		newSyncTestGateway(t, m).SyncIndexesHandler(rec, httptest.NewRequest(http.MethodPut, "/sync-indexes", nil))

		require.Equal(t, http.StatusAccepted, rec.Code)
		require.Equal(t, 1, m.triggered)
	})

	t.Run("already in progress", func(t *testing.T) {
		m := &syncerQuerierMock{triggerResult: false, statuses: []index.SyncStatus{{Name: "p1"}}}
		rec := httptest.NewRecorder()
		newSyncTestGateway(t, m).SyncIndexesHandler(rec, httptest.NewRequest(http.MethodPut, "/sync-indexes", nil))

		require.Equal(t, http.StatusConflict, rec.Code)
		require.Equal(t, 1, m.triggered)
	})

	t.Run("store does not support syncing", func(t *testing.T) {
		// indexQuerierMock does not implement index.Syncer.
		rec := httptest.NewRecorder()
		newSyncTestGateway(t, newIngesterQuerierMock()).SyncIndexesHandler(rec, httptest.NewRequest(http.MethodPut, "/sync-indexes", nil))

		require.Equal(t, http.StatusServiceUnavailable, rec.Code)
		require.Contains(t, rec.Body.String(), "not supported")
	})

	t.Run("store has no syncable indexes", func(t *testing.T) {
		// A syncer reporting an empty status list (e.g. a non-TSDB backend) is unsupported.
		rec := httptest.NewRecorder()
		newSyncTestGateway(t, &syncerQuerierMock{}).SyncIndexesHandler(rec, httptest.NewRequest(http.MethodPut, "/sync-indexes", nil))

		require.Equal(t, http.StatusServiceUnavailable, rec.Code)
		require.Contains(t, rec.Body.String(), "not supported")
	})
}

// syncStatusJSON mirrors the JSON shape index.SyncStatus.MarshalJSON emits, for
// decoding in tests. Pointer fields let us assert a field was omitted.
type syncStatusJSON struct {
	Name            string  `json:"name"`
	InProgress      bool    `json:"in_progress"`
	LastTrigger     string  `json:"last_trigger"`
	CurrentDuration *string `json:"current_duration"`
	LastDuration    string  `json:"last_duration"`
}

func TestSyncIndexStatusHandler(t *testing.T) {
	get := func(t *testing.T, q IndexQuerier) []syncStatusJSON {
		t.Helper()
		rec := httptest.NewRecorder()
		newSyncTestGateway(t, q).SyncIndexStatusHandler(rec, httptest.NewRequest(http.MethodGet, "/sync-indexes", nil))
		require.Equal(t, http.StatusOK, rec.Code)
		require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
		var resp []syncStatusJSON
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		return resp
	}

	t.Run("one labeled entry per index, conditional fields", func(t *testing.T) {
		resp := get(t, &syncerQuerierMock{statuses: []index.SyncStatus{
			{Name: "p1", InProgress: true, LastTrigger: "manual", CurrentDuration: 12 * time.Second, LastDuration: 90 * time.Second},
			{Name: "p2", LastTrigger: "periodic", LastDuration: 5 * time.Second},
		}})

		require.Len(t, resp, 2)

		// In-progress index: current duration present (Go duration string).
		require.Equal(t, "p1", resp[0].Name)
		require.True(t, resp[0].InProgress)
		require.Equal(t, "manual", resp[0].LastTrigger)
		require.NotNil(t, resp[0].CurrentDuration)
		require.Equal(t, "12s", *resp[0].CurrentDuration)
		require.Equal(t, "1m30s", resp[0].LastDuration)

		// Idle index: current duration absent.
		require.Equal(t, "p2", resp[1].Name)
		require.False(t, resp[1].InProgress)
		require.Equal(t, "periodic", resp[1].LastTrigger)
		require.Nil(t, resp[1].CurrentDuration)
		require.Equal(t, "5s", resp[1].LastDuration)
	})

	t.Run("never-synced index reports never_triggered", func(t *testing.T) {
		// An index that exists but has not synced yet renders an explicit
		// never_triggered trigger and a zero last duration (not an omitted field).
		resp := get(t, &syncerQuerierMock{statuses: []index.SyncStatus{{Name: "p1"}}})

		require.Len(t, resp, 1)
		require.Equal(t, "p1", resp[0].Name)
		require.False(t, resp[0].InProgress)
		require.Equal(t, index.SyncTriggerNever, resp[0].LastTrigger)
		require.Equal(t, "0s", resp[0].LastDuration)
		require.Nil(t, resp[0].CurrentDuration)
	})

	t.Run("store does not support syncing", func(t *testing.T) {
		rec := httptest.NewRecorder()
		newSyncTestGateway(t, newIngesterQuerierMock()).SyncIndexStatusHandler(rec, httptest.NewRequest(http.MethodGet, "/sync-indexes", nil))

		require.Equal(t, http.StatusServiceUnavailable, rec.Code)
		require.Contains(t, rec.Body.String(), "not supported")
	})

	t.Run("store has no syncable indexes", func(t *testing.T) {
		rec := httptest.NewRecorder()
		newSyncTestGateway(t, &syncerQuerierMock{}).SyncIndexStatusHandler(rec, httptest.NewRequest(http.MethodGet, "/sync-indexes", nil))

		require.Equal(t, http.StatusServiceUnavailable, rec.Code)
		require.Contains(t, rec.Body.String(), "not supported")
	})
}
