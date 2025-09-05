package querytee

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestProxyBackend_ContextIsolation(t *testing.T) {
	// Create a test server that delays before responding
	var wg sync.WaitGroup
	wg.Add(2)

	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer wg.Done()
		// Check that the request context is not canceled
		select {
		case <-r.Context().Done():
			t.Error("Backend 1 request context was canceled unexpectedly")
			w.WriteHeader(http.StatusInternalServerError)
		case <-time.After(50 * time.Millisecond):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("backend1"))
		}
	}))
	defer server1.Close()

	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer wg.Done()
		// Check that the request context is not canceled
		select {
		case <-r.Context().Done():
			t.Error("Backend 2 request context was canceled unexpectedly")
			w.WriteHeader(http.StatusInternalServerError)
		case <-time.After(50 * time.Millisecond):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("backend2"))
		}
	}))
	defer server2.Close()

	u1, err := url.Parse(server1.URL)
	require.NoError(t, err)
	u2, err := url.Parse(server2.URL)
	require.NoError(t, err)

	backend1 := NewProxyBackend("backend1", u1, 5*time.Second, true)
	backend2 := NewProxyBackend("backend2", u2, 5*time.Second, false)

	// Create a parent context that we'll use for both requests
	parentCtx := context.Background()
	req := httptest.NewRequest("GET", "/test", nil)
	req = req.WithContext(parentCtx)

	// Run both backend requests in parallel
	var response1, response2 *BackendResponse
	var wgRequests sync.WaitGroup
	wgRequests.Add(2)

	go func() {
		defer wgRequests.Done()
		response1 = backend1.ForwardRequest(req, nil)
	}()

	go func() {
		defer wgRequests.Done()
		response2 = backend2.ForwardRequest(req, nil)
	}()

	// Wait for both requests to complete
	wgRequests.Wait()
	wg.Wait()

	// Both requests should succeed
	require.NoError(t, response1.err)
	require.Equal(t, 200, response1.status)
	require.Equal(t, []byte("backend1"), response1.body)

	require.NoError(t, response2.err)
	require.Equal(t, 200, response2.status)
	require.Equal(t, []byte("backend2"), response2.body)
}
