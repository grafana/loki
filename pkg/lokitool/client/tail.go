package client

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/prometheus/common/config"
	log "github.com/sirupsen/logrus"

	"github.com/grafana/loki/v3/pkg/util"
)

const tailPath = "/loki/api/v1/tail"

// TailClient creates the websocket connection to the tail endpoint of Loki.
type TailClient interface {
	// LiveTailQueryConnContext sets up a websocket connection for tailing the logs that match the query.
	// Canceling ctx interrupts the connection setup but does not close an established connection.
	// The caller must close the returned connection when it is no longer needed.
	LiveTailQueryConnContext(ctx context.Context, queryStr string, delayFor time.Duration, limit int, start time.Time) (*websocket.Conn, error)
}

var _ TailClient = (*Client)(nil)

// LiveTailQueryConnContext uses /loki/api/v1/tail to set up a websocket connection and returns it.
// Canceling ctx interrupts connection setup but does not close an established connection.
// The caller must close the returned connection when it is no longer needed.
func (c *Client) LiveTailQueryConnContext(ctx context.Context, queryStr string, delayFor time.Duration, limit int, start time.Time) (*websocket.Conn, error) {
	params := util.NewQueryStringBuilder()
	params.SetString("query", queryStr)
	if delayFor != 0 {
		params.SetInt("delay_for", int64(delayFor.Seconds()))
	}
	params.SetInt("limit", int64(limit))
	params.SetInt("start", start.UnixNano())

	return c.wsConnect(ctx, tailPath, params.Encode())
}

func (c *Client) wsConnect(ctx context.Context, path, query string) (*websocket.Conn, error) {
	us, err := c.buildURL(path, query)
	if err != nil {
		return nil, err
	}

	tlsConfig, err := config.NewTLSConfig(&c.cfg.TLS)
	if err != nil {
		return nil, err
	}

	// Replace the scheme of the HTTP URL: http becomes ws, https becomes wss.
	if strings.HasPrefix(us, "http") {
		us = strings.Replace(us, "http", "ws", 1)
	}

	log.WithField("url", us).Debug("opening websocket connection")

	header, err := c.requestHeader()
	if err != nil {
		return nil, err
	}

	dialCtx, cancelDial := context.WithCancel(ctx)
	defer cancelDial()
	stopWatching := func() {}
	ws := websocket.Dialer{
		TLSClientConfig: tlsConfig,
		NetDialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			conn, err := (&net.Dialer{}).DialContext(ctx, network, address)
			if err != nil {
				return nil, err
			}
			// DialContext alone does not interrupt a blocked HTTP upgrade read
			// when a context without a deadline is canceled.
			done := make(chan struct{})
			stop := context.AfterFunc(ctx, func() {
				_ = conn.Close()
				close(done)
			})
			stopWatching = func() {
				if !stop() {
					<-done
				}
			}
			return conn, nil
		},
		Proxy: c.proxyFunc(),
	}

	conn, resp, err := ws.DialContext(dialCtx, us, header)
	// Transfer ownership only after any cancellation callback has finished.
	stopWatching()
	if ctx.Err() != nil {
		if conn != nil {
			_ = conn.Close()
		}
		return nil, ctx.Err()
	}
	if err != nil {
		if resp == nil {
			return nil, err
		}
		defer resp.Body.Close()
		buf, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("error response from server: %s (%v)", string(buf), err)
	}

	return conn, nil
}

// proxyFunc returns the function that selects the proxy for a request, the same way as for HTTP requests.
// It returns nil if no proxy is configured.
func (c *Client) proxyFunc() func(*http.Request) (*url.URL, error) {
	switch {
	case c.cfg.ProxyURL != "":
		// The proxy URL was validated when the client was created.
		proxyURL, _ := url.Parse(c.cfg.ProxyURL)
		return http.ProxyURL(proxyURL)
	case c.cfg.EnvProxy:
		return http.ProxyFromEnvironment
	default:
		return nil
	}
}
