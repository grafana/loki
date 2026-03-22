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
		limits := ExtractQueryLimitsFromContext(r.Context())
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
		limits := ExtractQueryLimitsFromContext(r.Context())
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
		actual := ExtractQueryLimitsFromContext(r.Context())
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

func Test_MiddlewareWithoutContextHeader(t *testing.T) {
	nextHandler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		limitsCtx := ExtractQueryLimitsContextFromContext(r.Context())
		require.Nil(t, limitsCtx)
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

func Test_MiddlewareWithBrokenContextHeader(t *testing.T) {
	nextHandler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		limitsCtx := ExtractQueryLimitsContextFromContext(r.Context())
		require.Nil(t, limitsCtx)
	})
	m := NewQueryLimitsMiddleware(log.NewNopLogger())
	wrapped := m.Wrap(nextHandler)

	rr := httptest.NewRecorder()
	r, err := http.NewRequest("GET", "/example", nil)
	require.NoError(t, err)
	r.Header.Add(HTTPHeaderQueryLimitsContextKey, "{broken}")
	wrapped.ServeHTTP(rr, r)
	response := rr.Result()
	require.Equal(t, http.StatusOK, response.StatusCode)
}

func Test_MiddlewareWithContextHeader(t *testing.T) {
	limitsCtx := Context{
		Expr: "{app=\"test\"}",
		From: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
	}

	nextHandler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		actual := ExtractQueryLimitsContextFromContext(r.Context())
		require.Equal(t, limitsCtx, *actual)
	})
	m := NewQueryLimitsMiddleware(log.NewNopLogger())
	wrapped := m.Wrap(nextHandler)

	rr := httptest.NewRecorder()
	r, err := http.NewRequest("GET", "/example", nil)
	require.NoError(t, err)
	err = InjectQueryLimitsContextHTTP(r, &limitsCtx)
	require.NoError(t, err)
	wrapped.ServeHTTP(rr, r)
	response := rr.Result()
	require.Equal(t, http.StatusOK, response.StatusCode)
}
