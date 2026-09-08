package client

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
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
			require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
			_, message, err := conn.ReadMessage()
			require.NoError(t, err)
			require.Equal(t, "connected", string(message))
			if cancelConnection {
				cancel()
			} else {
				require.NoError(t, conn.Close())
			}
			select {
			case <-closed:
			case <-time.After(5 * time.Second):
				t.Fatal("established websocket was not closed")
			}
			// The connection's cancellation watcher must stop even if the
			// caller closes the socket without canceling the context.
			server.Close()
			goleak.VerifyNone(t, leaks)
		})
	}
}
