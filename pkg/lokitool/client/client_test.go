package client

import (
	"context"
	"encoding/base64"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// recordedRequest is a request received by a test server.
type recordedRequest struct {
	Method  string
	URL     string
	Path    string
	Query   string
	Header  http.Header
	Body    string
	Handled int
}

// newServer starts a server that records requests and responds with the status and body.
func newServer(t *testing.T, status int, body string) (*httptest.Server, func() []recordedRequest) {
	t.Helper()

	var mu sync.Mutex
	var requests []recordedRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		requests = append(requests, recordedRequest{
			Method:  r.Method,
			URL:     r.URL.String(),
			Path:    r.URL.EscapedPath(),
			Query:   r.URL.RawQuery,
			Header:  r.Header.Clone(),
			Body:    string(b),
			Handled: len(requests) + 1,
		})
		mu.Unlock()

		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	return srv, func() []recordedRequest {
		mu.Lock()
		defer mu.Unlock()
		return append([]recordedRequest(nil), requests...)
	}
}

// do sends a GET request and returns the received request.
func do(t *testing.T, cfg Config) recordedRequest {
	t.Helper()

	srv, requests := newServer(t, http.StatusOK, "ok")
	cfg.Address = srv.URL

	c, err := New(cfg)
	require.NoError(t, err)

	resp, err := c.Do(context.Background(), http.MethodGet, "/loki/api/v1/labels", "", nil)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	got := requests()
	require.Len(t, got, 1)
	return got[0]
}

func basic(username, password string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password))
}

func TestRequestHeader(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		got := do(t, Config{})

		require.Contains(t, got.Header.Get("User-Agent"), "lokitool/")
		require.Empty(t, got.Header.Get(HeaderOrgID))
		require.Empty(t, got.Header.Get(HeaderCacheControl))
		require.Empty(t, got.Header.Get(HeaderQueryTags))
		require.Empty(t, got.Header.Get("Authorization"))
	})

	t.Run("all", func(t *testing.T) {
		got := do(t, Config{
			OrgID:     "tenant",
			NoCache:   true,
			QueryTags: "Source=test",
			Headers:   []string{"X-Custom: value", "X-Other:  spaced  "},
		})

		require.Equal(t, "tenant", got.Header.Get(HeaderOrgID))
		require.Equal(t, "no-cache", got.Header.Get(HeaderCacheControl))
		require.Equal(t, "Source=test", got.Header.Get(HeaderQueryTags))
		require.Equal(t, "value", got.Header.Get("X-Custom"))
		require.Equal(t, "spaced", got.Header.Get("X-Other"))
	})

	t.Run("invalid custom header", func(t *testing.T) {
		for _, header := range []string{"no-colon", ": value"} {
			_, err := New(Config{Headers: []string{header}})
			require.ErrorContains(t, err, "invalid header")
		}
	})
}

func TestAuthentication(t *testing.T) {
	for _, tc := range []struct {
		name       string
		cfg        Config
		wantHeader string
		wantValue  string
	}{
		{
			name: "none",
			cfg:  Config{},
		},
		{
			name:       "basic auth",
			cfg:        Config{Username: "user", Password: "pass"},
			wantHeader: "Authorization", wantValue: basic("user", "pass"),
		},
		{
			name:       "password falls back to org id as username",
			cfg:        Config{Password: "pass", OrgID: "tenant"},
			wantHeader: "Authorization", wantValue: basic("tenant", "pass"),
		},
		{
			name:       "username without password",
			cfg:        Config{Username: "user"},
			wantHeader: "Authorization", wantValue: basic("user", ""),
		},
		{
			name:       "bearer token",
			cfg:        Config{BearerToken: "token"},
			wantHeader: "Authorization", wantValue: "Bearer token",
		},
		{
			name:       "custom header for basic auth",
			cfg:        Config{Username: "user", Password: "pass", AuthHeader: "X-Auth"},
			wantHeader: "X-Auth", wantValue: basic("user", "pass"),
		},
		{
			name:       "custom header for bearer token",
			cfg:        Config{BearerToken: "token", AuthHeader: "X-Auth"},
			wantHeader: "X-Auth", wantValue: "Bearer token",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := do(t, tc.cfg)

			if tc.wantHeader == "" {
				require.Empty(t, got.Header.Get("Authorization"))
				return
			}
			require.Equal(t, tc.wantValue, got.Header.Get(tc.wantHeader))
		})
	}

	t.Run("invalid combinations", func(t *testing.T) {
		_, err := New(Config{Username: "user", Password: "pass", BearerToken: "token"})
		require.Error(t, err)

		_, err = New(Config{Password: "pass"})
		require.ErrorContains(t, err, "password requires a username or org-id")
	})
}

func TestBearerTokenFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "token")
	require.NoError(t, os.WriteFile(file, []byte("first\n"), 0o600))

	srv, requests := newServer(t, http.StatusOK, "")
	c, err := New(Config{Address: srv.URL, BearerTokenFile: file})
	require.NoError(t, err)

	send := func() {
		resp, err := c.Do(context.Background(), http.MethodGet, "/", "", nil)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
	}

	send()
	// The token is read for every request.
	require.NoError(t, os.WriteFile(file, []byte("second\n"), 0o600))
	send()

	got := requests()
	require.Len(t, got, 2)
	require.Equal(t, "Bearer first", got[0].Header.Get("Authorization"))
	require.Equal(t, "Bearer second", got[1].Header.Get("Authorization"))

	t.Run("missing file", func(t *testing.T) {
		_, err := New(Config{BearerTokenFile: filepath.Join(t.TempDir(), "missing")})
		require.ErrorContains(t, err, "unable to read bearer token file")
	})
}

func TestDoURL(t *testing.T) {
	for _, tc := range []struct {
		name      string
		prefix    string
		path      string
		query     string
		wantPath  string
		wantQuery string
	}{
		{name: "no prefix", path: "/loki/api/v1/query", query: "query=up&limit=1", wantPath: "/loki/api/v1/query", wantQuery: "query=up&limit=1"},
		{name: "prefix", prefix: "/gateway", path: "/loki/api/v1/labels", wantPath: "/gateway/loki/api/v1/labels"},
		{name: "prefix with trailing slash", prefix: "/gateway/", path: "/loki/api/v1/labels", wantPath: "/gateway/loki/api/v1/labels"},
		{name: "escaped path", path: "/loki/api/v1/rules/name%2Fspace/group%20name", wantPath: "/loki/api/v1/rules/name%2Fspace/group%20name"},
		{name: "escaped prefix", prefix: "/a%2Fb", path: "/rules", wantPath: "/a%2Fb/rules"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, requests := newServer(t, http.StatusOK, "")
			c, err := New(Config{Address: srv.URL + tc.prefix})
			require.NoError(t, err)

			resp, err := c.Do(context.Background(), http.MethodGet, tc.path, tc.query, nil)
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())

			got := requests()
			require.Len(t, got, 1)
			require.Equal(t, tc.wantPath, got[0].Path)
			require.Equal(t, tc.wantQuery, got[0].Query)
		})
	}
}

func TestDoBody(t *testing.T) {
	srv, requests := newServer(t, http.StatusOK, "")
	c, err := New(Config{Address: srv.URL})
	require.NoError(t, err)

	resp, err := c.Do(context.Background(), http.MethodPost, "/rules/ns", "", []byte("payload"))
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	got := requests()
	require.Len(t, got, 1)
	require.Equal(t, http.MethodPost, got[0].Method)
	require.Equal(t, "payload", got[0].Body)
}

func TestDoErrors(t *testing.T) {
	t.Run("status and message", func(t *testing.T) {
		srv, requests := newServer(t, http.StatusBadRequest, "parse error\nsecond line")
		c, err := New(Config{Address: srv.URL})
		require.NoError(t, err)

		resp, err := c.Do(context.Background(), http.MethodGet, "/", "", nil)
		require.Nil(t, resp)
		require.EqualError(t, err, "server returned HTTP status 400 Bad Request: parse error")
		require.Len(t, requests(), 1, "retries are disabled by default")
	})

	t.Run("not found", func(t *testing.T) {
		srv, _ := newServer(t, http.StatusNotFound, "no group found")
		c, err := New(Config{Address: srv.URL})
		require.NoError(t, err)

		_, err = c.Do(context.Background(), http.MethodGet, "/", "", nil)
		require.ErrorIs(t, err, ErrResourceNotFound)
		require.ErrorContains(t, err, "no group found")
	})

	t.Run("redirects are not followed", func(t *testing.T) {
		target, targetRequests := newServer(t, http.StatusOK, "")
		srv := httptest.NewServer(http.RedirectHandler(target.URL, http.StatusFound))
		t.Cleanup(srv.Close)

		c, err := New(Config{Address: srv.URL})
		require.NoError(t, err)

		_, err = c.Do(context.Background(), http.MethodGet, "/", "", nil)
		require.ErrorContains(t, err, "302")
		require.Empty(t, targetRequests())
	})

	t.Run("canceled context", func(t *testing.T) {
		srv, _ := newServer(t, http.StatusOK, "")
		c, err := New(Config{Address: srv.URL})
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err = c.Do(ctx, http.MethodGet, "/", "", nil)
		require.ErrorIs(t, err, context.Canceled)
	})
}

func TestDoRetries(t *testing.T) {
	// failFirst starts a server that fails the first n requests with status 500.
	failFirst := func(t *testing.T, n int32) (*httptest.Server, *atomic.Int32, func() []string) {
		t.Helper()

		var count atomic.Int32
		var mu sync.Mutex
		var bodies []string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			mu.Lock()
			bodies = append(bodies, string(b))
			mu.Unlock()

			if count.Add(1) <= n {
				http.Error(w, "unavailable", http.StatusInternalServerError)
				return
			}
			_, _ = w.Write([]byte("ok"))
		}))
		t.Cleanup(srv.Close)

		return srv, &count, func() []string {
			mu.Lock()
			defer mu.Unlock()
			return append([]string(nil), bodies...)
		}
	}

	t.Run("succeeds after retries", func(t *testing.T) {
		srv, count, bodies := failFirst(t, 2)
		c, err := New(Config{Address: srv.URL, Retries: 2})
		require.NoError(t, err)

		resp, err := c.Do(context.Background(), http.MethodPost, "/", "", []byte("payload"))
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())

		require.EqualValues(t, 3, count.Load())
		require.Equal(t, []string{"payload", "payload", "payload"}, bodies(), "body is sent with every attempt")
	})

	t.Run("returns last error when attempts are used up", func(t *testing.T) {
		srv, count, _ := failFirst(t, 10)
		c, err := New(Config{Address: srv.URL, Retries: 2})
		require.NoError(t, err)

		_, err = c.Do(context.Background(), http.MethodGet, "/", "", nil)
		require.EqualError(t, err, "server returned HTTP status 500 Internal Server Error: unavailable")
		require.EqualValues(t, 3, count.Load())
	})

	t.Run("no retries by default", func(t *testing.T) {
		srv, count, _ := failFirst(t, 1)
		c, err := New(Config{Address: srv.URL})
		require.NoError(t, err)

		_, err = c.Do(context.Background(), http.MethodGet, "/", "", nil)
		require.Error(t, err)
		require.EqualValues(t, 1, count.Load())
	})

	t.Run("connection errors are retried", func(t *testing.T) {
		srv := httptest.NewServer(http.NotFoundHandler())
		address := srv.URL
		srv.Close()

		c, err := New(Config{Address: address, Retries: 1})
		require.NoError(t, err)

		start := time.Now()
		_, err = c.Do(context.Background(), http.MethodGet, "/", "", nil)
		require.Error(t, err)
		require.Less(t, time.Since(start), 10*time.Second)
	})
}

// proxy starts a server that acts as an HTTP proxy and records the requests.
// Requests to 127.0.0.1 and localhost never use a proxy, so tests send
// requests to a host name that can not be resolved.
func proxy(t *testing.T) (string, func() []recordedRequest) {
	t.Helper()

	srv, requests := newServer(t, http.StatusOK, "proxied")
	return srv.URL, requests
}

const unresolvableAddress = "http://loki.invalid:3100"

func TestProxy(t *testing.T) {
	send := func(t *testing.T, cfg Config) error {
		t.Helper()

		cfg.Address = unresolvableAddress
		c, err := New(cfg)
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		resp, err := c.Do(ctx, http.MethodGet, "/loki/api/v1/labels", "", nil)
		if err == nil {
			require.NoError(t, resp.Body.Close())
		}
		return err
	}

	setEnvProxy := func(t *testing.T, url string) {
		t.Helper()
		for _, name := range []string{"HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy"} {
			t.Setenv(name, url)
		}
		for _, name := range []string{"NO_PROXY", "no_proxy"} {
			t.Setenv(name, "")
		}
	}

	t.Run("environment", func(t *testing.T) {
		proxyURL, requests := proxy(t)
		setEnvProxy(t, proxyURL)

		require.NoError(t, send(t, Config{EnvProxy: true}))

		got := requests()
		require.Len(t, got, 1)
		require.Equal(t, unresolvableAddress+"/loki/api/v1/labels", got[0].URL)
	})

	t.Run("environment is ignored if disabled", func(t *testing.T) {
		proxyURL, requests := proxy(t)
		setEnvProxy(t, proxyURL)

		require.Error(t, send(t, Config{EnvProxy: false}))
		require.Empty(t, requests())
	})

	t.Run("proxy url", func(t *testing.T) {
		proxyURL, requests := proxy(t)
		setEnvProxy(t, "")

		require.NoError(t, send(t, Config{ProxyURL: proxyURL}))
		require.Len(t, requests(), 1)
	})

	t.Run("proxy url takes precedence over environment", func(t *testing.T) {
		envProxyURL, envRequests := proxy(t)
		setEnvProxy(t, envProxyURL)
		proxyURL, requests := proxy(t)

		require.NoError(t, send(t, Config{ProxyURL: proxyURL, EnvProxy: true}))
		require.Len(t, requests(), 1)
		require.Empty(t, envRequests())
	})

	t.Run("invalid proxy url", func(t *testing.T) {
		_, err := New(Config{ProxyURL: "http://[::1"})
		require.ErrorContains(t, err, "invalid proxy URL")
	})
}

func TestCompression(t *testing.T) {
	for _, compress := range []bool{false, true} {
		got := do(t, Config{Compress: compress})
		require.Equal(t, compress, got.Header.Get("Accept-Encoding") != "", "compress=%v", compress)
	}
}

func TestTLS(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(srv.Close)

	caFile := filepath.Join(t.TempDir(), "ca.pem")
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	require.NoError(t, os.WriteFile(caFile, ca, 0o600))

	send := func(cfg Config) error {
		cfg.Address = srv.URL
		c, err := New(cfg)
		if err != nil {
			return err
		}
		resp, err := c.Do(context.Background(), http.MethodGet, "/", "", nil)
		if err == nil {
			require.NoError(t, resp.Body.Close())
		}
		return err
	}

	t.Run("unknown authority", func(t *testing.T) {
		require.ErrorContains(t, send(Config{}), "certificate")
	})

	t.Run("ca file", func(t *testing.T) {
		cfg := Config{}
		cfg.TLS.CAFile = caFile
		require.NoError(t, send(cfg))
	})

	t.Run("skip verify", func(t *testing.T) {
		cfg := Config{}
		cfg.TLS.InsecureSkipVerify = true
		require.NoError(t, send(cfg))
	})

	t.Run("missing ca file", func(t *testing.T) {
		cfg := Config{}
		cfg.TLS.CAFile = filepath.Join(t.TempDir(), "missing.pem")
		require.Error(t, send(cfg))
	})
}
