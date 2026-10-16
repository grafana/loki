package client

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestLiveTailQueryConnContextCancellation(t *testing.T) {
	requestStarted := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(requestStarted)
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)

	c, err := New(Config{Address: server.URL})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	errChan := make(chan error, 1)
	go func() {
		_, err := c.LiveTailQueryConnContext(ctx, "", 0, 0, time.Time{})
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
			c, err := New(Config{Address: server.URL})
			require.NoError(t, err)
			conn, err := c.LiveTailQueryConnContext(ctx, "", 0, 0, time.Time{})
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

func TestLiveTailQueryConnRequest(t *testing.T) {
	var request *http.Request
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request = r.Clone(context.Background())
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		_ = conn.Close()
	}))
	t.Cleanup(server.Close)

	c, err := New(Config{Address: server.URL + "/gateway", OrgID: "tenant", Username: "user", Password: "pass"})
	require.NoError(t, err)

	conn, err := c.LiveTailQueryConnContext(context.Background(), `{app="foo"}`, 5*time.Second, 10, time.Unix(1700000000, 0))
	require.NoError(t, err)
	require.NoError(t, conn.Close())

	require.Equal(t, "/gateway/loki/api/v1/tail", request.URL.Path)
	require.Equal(t, "tenant", request.Header.Get(HeaderOrgID))
	require.Equal(t, basic("user", "pass"), request.Header.Get("Authorization"))
	require.Contains(t, request.Header.Get("User-Agent"), "lokitool/")

	params := request.URL.Query()
	require.Equal(t, `{app="foo"}`, params.Get("query"))
	require.Equal(t, "5", params.Get("delay_for"))
	require.Equal(t, "10", params.Get("limit"))
	require.Equal(t, "1700000000000000000", params.Get("start"))
}

func TestLiveTailQueryConnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "tail is not enabled", http.StatusNotFound)
	}))
	t.Cleanup(server.Close)

	c, err := New(Config{Address: server.URL})
	require.NoError(t, err)

	_, err = c.LiveTailQueryConnContext(context.Background(), "", 0, 0, time.Time{})
	require.ErrorContains(t, err, "tail is not enabled")
}

func TestLiveTailQueryConnProxy(t *testing.T) {
	t.Run("proxy url", func(t *testing.T) {
		c, err := New(Config{ProxyURL: "http://proxy.example.com:3128"})
		require.NoError(t, err)

		proxyURL, err := c.proxyFunc()(&http.Request{URL: &url.URL{Scheme: "http", Host: "loki.example.com"}})
		require.NoError(t, err)
		require.Equal(t, "http://proxy.example.com:3128", proxyURL.String())
	})

	t.Run("environment", func(t *testing.T) {
		c, err := New(Config{EnvProxy: true})
		require.NoError(t, err)
		require.NotNil(t, c.proxyFunc())
	})

	t.Run("none", func(t *testing.T) {
		c, err := New(Config{})
		require.NoError(t, err)
		require.Nil(t, c.proxyFunc())
	})
}
