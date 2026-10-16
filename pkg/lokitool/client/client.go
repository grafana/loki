// Package client provides a client for the HTTP APIs of Loki.
package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/grafana/dskit/backoff"
	"github.com/prometheus/common/config"
	log "github.com/sirupsen/logrus"

	"github.com/grafana/loki/v3/pkg/util/build"
)

// ErrResourceNotFound is returned when the server responds with 404 Not Found.
var ErrResourceNotFound = errors.New("requested resource not found")

// Headers set by the client.
const (
	HeaderOrgID        = "X-Scope-OrgID"
	HeaderQueryTags    = "X-Query-Tags"
	HeaderCacheControl = "Cache-Control"
)

// Client sends requests to the HTTP APIs of Loki.
type Client struct {
	cfg      Config
	endpoint *url.URL
	http     *http.Client
}

// New returns a client for the given configuration.
// Environment variables and defaults are applied to the configuration as described in Config.Resolve.
func New(cfg Config) (*Client, error) {
	cfg.Resolve()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	endpoint, err := url.Parse(cfg.Address)
	if err != nil {
		return nil, fmt.Errorf("invalid address %q: %w", cfg.Address, err)
	}

	httpClient, err := newHTTPClient(cfg)
	if err != nil {
		return nil, err
	}

	c := &Client{cfg: cfg, endpoint: endpoint, http: httpClient}

	// Fail early if the headers are invalid.
	if _, err := c.requestHeader(); err != nil {
		return nil, err
	}

	log.WithFields(log.Fields{"address": cfg.Address, "org_id": cfg.OrgID}).Debug("created client")

	return c, nil
}

func newHTTPClient(cfg Config) (*http.Client, error) {
	clientConfig := config.HTTPClientConfig{TLSConfig: cfg.TLS}

	switch {
	case cfg.ProxyURL != "":
		proxyURL, err := url.Parse(cfg.ProxyURL)
		if err != nil {
			return nil, fmt.Errorf("invalid proxy URL %q: %w", cfg.ProxyURL, err)
		}
		clientConfig.ProxyURL = config.URL{URL: proxyURL}
	case cfg.EnvProxy:
		clientConfig.ProxyFromEnvironment = true
	}

	httpClient, err := config.NewClientFromConfig(clientConfig, "lokitool", config.WithHTTP2Disabled())
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}
	httpClient.Timeout = 0

	if cfg.Compress {
		// The transport created above disables compression.
		if transport, ok := httpClient.Transport.(*http.Transport); ok {
			transport.DisableCompression = false
		}
	}

	return httpClient, nil
}

// requestHeader returns the headers for a request.
func (c *Client) requestHeader() (http.Header, error) {
	h := make(http.Header)
	h.Set("User-Agent", "lokitool/"+build.Version)

	if c.cfg.OrgID != "" {
		h.Set(HeaderOrgID, c.cfg.OrgID)
	}
	if c.cfg.NoCache {
		h.Set(HeaderCacheControl, "no-cache")
	}
	if c.cfg.QueryTags != "" {
		h.Set(HeaderQueryTags, c.cfg.QueryTags)
	}

	for _, header := range c.cfg.Headers {
		name, value, ok := strings.Cut(header, ":")
		name = strings.TrimSpace(name)
		if !ok || name == "" {
			return nil, fmt.Errorf("invalid header %q: expected format 'Header-Name: value'", header)
		}
		h.Set(name, strings.TrimSpace(value))
	}

	authHeader := c.cfg.AuthHeader
	if authHeader == "" {
		authHeader = defaultAuthHeader
	}

	switch {
	case c.cfg.Username != "" || c.cfg.Password != "":
		username := c.cfg.Username
		if username == "" {
			username = c.cfg.OrgID
		}
		credentials := base64.StdEncoding.EncodeToString([]byte(username + ":" + c.cfg.Password))
		h.Set(authHeader, "Basic "+credentials)
	case c.cfg.BearerToken != "":
		h.Set(authHeader, "Bearer "+c.cfg.BearerToken)
	case c.cfg.BearerTokenFile != "":
		b, err := os.ReadFile(c.cfg.BearerTokenFile)
		if err != nil {
			return nil, fmt.Errorf("unable to read bearer token file %s: %w", c.cfg.BearerTokenFile, err)
		}
		h.Set(authHeader, "Bearer "+strings.TrimSpace(string(b)))
	}

	return h, nil
}

// Do sends a request and returns the response if the status code is 2xx.
// The caller must close the body of the response.
//
// The path is appended to the path of the configured address and must start
// with a slash. It may contain escaped characters.
// The request is retried if it fails, as configured. The last error is
// returned when no attempts are left. A 404 response is returned as ErrResourceNotFound.
func (c *Client) Do(ctx context.Context, method, path, rawQuery string, body []byte) (*http.Response, error) {
	u, err := c.buildURL(path, rawQuery)
	if err != nil {
		return nil, err
	}

	header, err := c.requestHeader()
	if err != nil {
		return nil, err
	}

	retries := backoff.New(ctx, backoff.Config{
		MinBackoff: time.Duration(c.cfg.MinBackoff) * time.Second,
		MaxBackoff: time.Duration(c.cfg.MaxBackoff) * time.Second,
		// A maximum of 0 means infinite retries.
		MaxRetries: c.cfg.Retries + 1,
	})

	var lastErr error
	for retries.Ongoing() {
		req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header = header.Clone()

		log.WithFields(log.Fields{"method": method, "url": u}).Debug("sending request")

		resp, err := c.http.Do(req)
		if err == nil {
			err = checkResponse(resp)
			if err == nil {
				return resp, nil
			}
		}
		lastErr = err

		retries.Wait()
		if retries.Ongoing() {
			log.WithError(err).Warnf("request failed, %d retries remaining", c.cfg.Retries+1-retries.NumRetries())
		}
	}

	if lastErr == nil {
		lastErr = ctx.Err()
	}
	return nil, lastErr
}

func (c *Client) buildURL(path, rawQuery string) (string, error) {
	// The path may contain escaped characters, parse it again to separate path and query.
	p, err := url.Parse(path)
	if err != nil {
		return "", err
	}

	u := *c.endpoint
	// If the path or the endpoint contains escaping that requires RawPath, join the raw paths as well.
	if p.RawPath != "" || u.RawPath != "" {
		u.RawPath = joinPath(u.EscapedPath(), p.EscapedPath())
	}
	u.Path = joinPath(u.Path, p.Path)
	u.RawQuery = rawQuery

	return u.String(), nil
}

// joinPath appends the target path to the base path. The target path must start with a slash.
func joinPath(base, target string) string {
	return strings.TrimSuffix(base, "/") + target
}

// checkResponse returns an error and closes the body if the status code is not 2xx.
func checkResponse(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode <= 299 {
		return nil
	}
	defer resp.Body.Close()

	var msg string
	scanner := bufio.NewScanner(io.LimitReader(resp.Body, 512))
	if scanner.Scan() {
		msg = scanner.Text()
	}

	errMsg := fmt.Sprintf("server returned HTTP status %s", resp.Status)
	if msg != "" {
		errMsg += ": " + msg
	}

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("%w (%s)", ErrResourceNotFound, errMsg)
	}
	return errors.New(errMsg)
}
