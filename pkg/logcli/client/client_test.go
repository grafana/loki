package client

import (
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func Test_buildURL(t *testing.T) {
	tests := []struct {
		name    string
		u, p, q string
		want    string
		wantErr bool
	}{
		{"err", "8://2", "/bar", "", "", true},
		{"strip /", "http://localhost//", "//bar", "a=b", "http://localhost/bar?a=b", false},
		{"sub path", "https://localhost/loki/", "/bar/foo", "c=d&e=f", "https://localhost/loki/bar/foo?c=d&e=f", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildURL(tt.u, tt.p, tt.q)
			if (err != nil) != tt.wantErr {
				t.Errorf("buildURL() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("buildURL() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLiveTailQueryConnContextCancellation(t *testing.T) {
	requestStarted := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(requestStarted)
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)

	c := &DefaultClient{Address: server.URL}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	errChan := make(chan error, 1)
	go func() {
		_, err := c.LiveTailQueryConnContext(ctx, "", 0, 0, time.Time{}, true)
		errChan <- err
	}()

	select {
	case <-requestStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("tail connection request did not start")
	}
	cancel()

	select {
	case err := <-errChan:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("tail connection did not return after cancellation")
	}
}

func Test_getHTTPRequestHeader(t *testing.T) {
	tests := []struct {
		name    string
		client  DefaultClient
		want    http.Header
		wantErr bool
	}{
		{"empty", DefaultClient{}, http.Header{}, false},
		{"partial-headers", DefaultClient{
			OrgID:     "124",
			QueryTags: "source=abc",
		}, http.Header{
			"X-Scope-OrgID": []string{"124"},
			"X-Query-Tags":  []string{"source=abc"},
		}, false},
		{"basic-auth", DefaultClient{
			Username: "123",
			Password: "secure",
		}, http.Header{
			"Authorization": []string{"Basic " + base64.StdEncoding.EncodeToString([]byte("123:secure"))},
		}, false},
		{"bearer-token", DefaultClient{
			BearerToken: "secureToken",
		}, http.Header{
			"Authorization": []string{"Bearer " + "secureToken"},
		}, false},
		{"custom-headers", DefaultClient{
			CustomHeaders: []string{"X-Custom-Header: custom-value", "X-Another-Header: another-value"},
		}, http.Header{
			"X-Custom-Header":  []string{"custom-value"},
			"X-Another-Header": []string{"another-value"},
		}, false},
		{"custom-headers-with-spaces", DefaultClient{
			CustomHeaders: []string{"X-Custom-Header:  custom-value  ", "X-Another-Header:  another-value  "},
		}, http.Header{
			"X-Custom-Header":  []string{"custom-value"},
			"X-Another-Header": []string{"another-value"},
		}, false},
		{"custom-headers-invalid-format", DefaultClient{
			CustomHeaders: []string{"InvalidHeader"},
		}, nil, true},
		{"custom-headers-empty-name", DefaultClient{
			CustomHeaders: []string{" : value"},
		}, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.client.getHTTPRequestHeader()
			if (err != nil) != tt.wantErr {
				t.Errorf("getHTTPRequestHeader() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// If we expect an error, we shouldn't have any headers
			if tt.wantErr {
				assert.Nil(t, got)
				return
			}

			// User-Agent should be set all the time.
			assert.Equal(t, got["User-Agent"], []string{userAgent})

			for k := range tt.want {
				ck := http.CanonicalHeaderKey(k)
				assert.Equal(t, tt.want[k], got[ck])
			}
		})
	}
}

func TestLiveTailQueryConnContextEstablishedConnection(t *testing.T) {
	for _, cancelConnection := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancel=%t", cancelConnection), func(t *testing.T) {
			leaks := goleak.IgnoreCurrent()
			t.Cleanup(func() { goleak.VerifyNone(t, leaks) })
			closed := make(chan struct{})
			upgrader := websocket.Upgrader{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := upgrader.Upgrade(w, r, nil)
				if err != nil {
					return
				}
				defer conn.Close()
				if err := conn.WriteMessage(websocket.TextMessage, []byte("connected")); err != nil {
					return
				}
				messageType, message, err := conn.ReadMessage()
				if err != nil {
					return
				}
				if err := conn.WriteMessage(messageType, message); err != nil {
					return
				}
				_, _, _ = conn.ReadMessage()
				close(closed)
			}))
			t.Cleanup(server.Close)
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			c := &DefaultClient{Address: server.URL}
			conn, err := c.LiveTailQueryConnContext(ctx, "", 0, 0, time.Time{}, true)
			require.NoError(t, err)
			t.Cleanup(func() { _ = conn.Close() })
			if cancelConnection {
				cancel()
			}
			require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
			_, message, err := conn.ReadMessage()
			require.NoError(t, err)
			require.Equal(t, "connected", string(message))
			require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte("still connected")))
			_, message, err = conn.ReadMessage()
			require.NoError(t, err)
			require.Equal(t, "still connected", string(message))
			require.NoError(t, conn.Close())
			select {
			case <-closed:
			case <-time.After(5 * time.Second):
				t.Fatal("established websocket was not closed")
			}
			// Connection setup must leave no cancellation watcher behind.
			server.Close()
			goleak.VerifyNone(t, leaks)
		})
	}
}

func TestLiveTailProxy(t *testing.T) {
	for _, key := range []string{
		"HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy",
		"NO_PROXY", "no_proxy", "ALL_PROXY", "all_proxy", "REQUEST_METHOD",
	} {
		t.Setenv(key, "")
	}

	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		_ = conn.Close()
	}))
	t.Cleanup(server.Close)
	// The fully qualified localhost name resolves locally and exercises proxy matching.
	address := fmt.Sprintf("http://localhost.:%d", server.Listener.Addr().(*net.TCPAddr).Port)

	tests := []struct {
		name             string
		environmentProxy bool
		noProxy          string
		explicitProxy    bool
		wantProxy        bool
	}{
		{name: "environment enabled", environmentProxy: true, wantProxy: true},
		{name: "environment disabled"},
		{name: "NO_PROXY exclusion", environmentProxy: true, noProxy: "localhost."},
		{name: "explicit proxy URL", explicitProxy: true, wantProxy: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var proxyMethod atomic.Value
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				proxyMethod.Store(r.Method)
				w.WriteHeader(http.StatusBadGateway)
			}))
			t.Cleanup(proxy.Close)

			c := &DefaultClient{Address: address, EnvironmentProxy: tt.environmentProxy}
			if tt.explicitProxy {
				c.ProxyURL = proxy.URL
			} else {
				t.Setenv("HTTP_PROXY", proxy.URL)
				t.Setenv("NO_PROXY", tt.noProxy)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			conn, err := c.LiveTailQueryConnContext(ctx, "", 0, 0, time.Time{}, true)
			if conn != nil {
				t.Cleanup(func() { _ = conn.Close() })
			}

			if tt.wantProxy {
				require.Error(t, err)
				require.Equal(t, http.MethodConnect, proxyMethod.Load())
				return
			}
			require.NoError(t, err)
			require.NotNil(t, conn)
			require.Nil(t, proxyMethod.Load(), "tail connection used the proxy")
		})
	}
}
