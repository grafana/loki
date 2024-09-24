package querylimits

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
)

func Test_MiddlewareWithoutHeader(t *testing.T) {
	nextHandler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		limits := ExtractQueryLimitsContext(r.Context())
		require.Nil(t, limits)
	})
	m := NewQueryLimitsMiddleware(log.NewNopLogger())
	wrapped := m.Wrap(nextHandler)

	rr := httptest.NewRecorder()
	r, err := http.NewRequest("GET", "/example", nil)
	require.NoError(t, err)
	wrapped.ServeHTTP(rr, r)
	response := rr.Result()
	require.Equal(t, http.StatusOK, response.StatusCode)
}

func Test_MiddlewareWithBrokenHeader(t *testing.T) {
	nextHandler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		limits := ExtractQueryLimitsContext(r.Context())
		require.Nil(t, limits)
	})
	m := NewQueryLimitsMiddleware(log.NewNopLogger())
	wrapped := m.Wrap(nextHandler)

	rr := httptest.NewRecorder()
	r, err := http.NewRequest("GET", "/example", nil)
	require.NoError(t, err)
	r.Header.Add(HTTPHeaderQueryLimitsKey, "{broken}")
	wrapped.ServeHTTP(rr, r)
	response := rr.Result()
	require.Equal(t, http.StatusOK, response.StatusCode)
}

func Test_MiddlewareWithHeader(t *testing.T) {
	limits := QueryLimits{
		model.Duration(1 * time.Second),
		model.Duration(1 * time.Second),
		model.Duration(1 * time.Second),
		1,
		model.Duration(1 * time.Second),
		[]string{"foo", "bar"},
		10,
		10,
	}

	nextHandler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		actual := ExtractQueryLimitsContext(r.Context())
		require.Equal(t, limits, *actual)
	})
	m := NewQueryLimitsMiddleware(log.NewNopLogger())
	wrapped := m.Wrap(nextHandler)

	rr := httptest.NewRecorder()
	r, err := http.NewRequest("GET", "/example", nil)
	require.NoError(t, err)
	err = InjectQueryLimitsHTTP(r, &limits)
	require.NoError(t, err)
	wrapped.ServeHTTP(rr, r)
	response := rr.Result()
	require.Equal(t, http.StatusOK, response.StatusCode)
}
