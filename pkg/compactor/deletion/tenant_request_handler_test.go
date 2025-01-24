package deletion

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/grafana/dskit/user"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/validation"
)

func TestDeleteRequestHandlerDeletionMiddleware(t *testing.T) {
	fl := &fakeLimits{
		tenantLimits: map[string]limit{
			"1": {deletionMode: "filter-only"},
			"2": {deletionMode: "disabled"},
		},
	}

	// Setup handler
	middle := TenantMiddleware(fl, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))

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

	// User header is not given
	req = httptest.NewRequest(http.MethodGet, "http://www.your-domain.com", nil)

	res = httptest.NewRecorder()
	middle.ServeHTTP(res, req)

	require.Equal(t, http.StatusBadRequest, res.Result().StatusCode)
}

type limit struct {
	deletionMode    string
	retentionPeriod time.Duration
	streamRetention []validation.StreamRetention
}

type fakeLimits struct {
	tenantLimits map[string]limit
	defaultLimit limit
}

func (f *fakeLimits) getLimitForUser(userID string) limit {
	limit := f.defaultLimit
	if override, ok := f.tenantLimits[userID]; ok {
		limit = override
	}

	return limit
}

func (f *fakeLimits) DeletionMode(userID string) string {
	return f.getLimitForUser(userID).deletionMode
}

func (f *fakeLimits) RetentionPeriod(userID string) time.Duration {
	return f.getLimitForUser(userID).retentionPeriod
}

func (f *fakeLimits) StreamRetention(userID string) []validation.StreamRetention {
	return f.getLimitForUser(userID).streamRetention
}
