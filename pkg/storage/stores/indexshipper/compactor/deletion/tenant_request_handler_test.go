package deletion

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/dskit/user"
	"github.com/stretchr/testify/require"
)

func TestDeleteRequestHandlerDeletionMiddleware(t *testing.T) {
	fl := &fakeLimits{
		limits: map[string]string{
			"1": "filter-only",
			"2": "disabled",
		},
	}

	// Setup handler
	middle := TenantMiddleware(fl, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

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

type fakeLimits struct {
	limits map[string]string
	mode   string
}

func (f *fakeLimits) DeletionMode(userID string) string {
	if f.mode != "" {
		return f.mode
	}

	return f.limits[userID]
}
