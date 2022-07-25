package deletion

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/user"

	"github.com/grafana/loki/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper/storage"
	"github.com/grafana/loki/pkg/validation"
)

type retentionLimit struct {
	compactorDeletionEnabled bool
	retentionPeriod          time.Duration
	streamRetention          []validation.StreamRetention
}

func (r retentionLimit) convertToValidationLimit() *validation.Limits {
	return &validation.Limits{
		CompactorDeletionEnabled: r.compactorDeletionEnabled,
		RetentionPeriod:          model.Duration(r.retentionPeriod),
		StreamRetention:          r.streamRetention,
	}
}

type fakeLimits struct {
	defaultLimit retentionLimit
	perTenant    map[string]retentionLimit
}

func (f fakeLimits) RetentionPeriod(userID string) time.Duration {
	return f.perTenant[userID].retentionPeriod
}

func (f fakeLimits) StreamRetention(userID string) []validation.StreamRetention {
	return f.perTenant[userID].streamRetention
}

func (f fakeLimits) CompactorDeletionEnabled(userID string) bool {
	return f.perTenant[userID].compactorDeletionEnabled
}

func (f fakeLimits) DefaultLimits() *validation.Limits {
	return f.defaultLimit.convertToValidationLimit()
}

func (f fakeLimits) AllByUserID() map[string]*validation.Limits {
	res := make(map[string]*validation.Limits)
	for userID, ret := range f.perTenant {
		res[userID] = ret.convertToValidationLimit()
	}
	return res
}

func TestDeleteRequestHandlerDeletionMiddleware(t *testing.T) {
	// build the store
	tempDir := t.TempDir()

	workingDir := filepath.Join(tempDir, "working-dir")
	objectStorePath := filepath.Join(tempDir, "object-store")

	objectClient, err := local.NewFSObjectClient(local.FSConfig{
		Directory: objectStorePath,
	})
	require.NoError(t, err)
	testDeleteRequestsStore, err := NewDeleteStore(workingDir, storage.NewIndexStorageClient(objectClient, ""))
	require.NoError(t, err)

	// limits
	fl := &fakeLimits{
		perTenant: map[string]retentionLimit{
			"1": {compactorDeletionEnabled: true},
			"2": {compactorDeletionEnabled: false},
		},
	}

	// Setup handler
	drh := NewDeleteRequestHandler(testDeleteRequestsStore, 10*time.Second, fl, nil)
	middle := drh.deletionMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	// User that has deletion enabled
	req := httptest.NewRequest(http.MethodGet, "http://www.your-domain.com", nil)
	req = req.WithContext(user.InjectOrgID(req.Context(), "1"))

	res := httptest.NewRecorder()
	middle.ServeHTTP(res, req)

	require.Equal(t, http.StatusOK, res.Result().StatusCode)

	// User that does not have deletion enabled
	req = httptest.NewRequest(http.MethodGet, "http://www.your-domain.com", nil)
	req = req.WithContext(user.InjectOrgID(req.Context(), "2"))

	res = httptest.NewRecorder()
	middle.ServeHTTP(res, req)

	require.Equal(t, http.StatusForbidden, res.Result().StatusCode)

	// User without override, this should use the default value which is false
	req = httptest.NewRequest(http.MethodGet, "http://www.your-domain.com", nil)
	req = req.WithContext(user.InjectOrgID(req.Context(), "3"))

	res = httptest.NewRecorder()
	middle.ServeHTTP(res, req)

	require.Equal(t, http.StatusForbidden, res.Result().StatusCode)

	// User without override, after the default value is set to true
	fl.defaultLimit.compactorDeletionEnabled = true

	req = httptest.NewRequest(http.MethodGet, "http://www.your-domain.com", nil)
	req = req.WithContext(user.InjectOrgID(req.Context(), "3"))

	res = httptest.NewRecorder()
	middle.ServeHTTP(res, req)

	require.Equal(t, http.StatusOK, res.Result().StatusCode)

	// User header is not given
	req = httptest.NewRequest(http.MethodGet, "http://www.your-domain.com", nil)

	res = httptest.NewRecorder()
	middle.ServeHTTP(res, req)

	require.Equal(t, http.StatusBadRequest, res.Result().StatusCode)
}
