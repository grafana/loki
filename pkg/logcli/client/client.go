package client

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	json "github.com/json-iterator/go"
	"github.com/prometheus/common/config"

	"github.com/grafana/dskit/backoff"

	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/util"
	"github.com/grafana/loki/pkg/util/build"
)

const (
	queryPath         = "/loki/api/v1/query"
	queryRangePath    = "/loki/api/v1/query_range"
	labelsPath        = "/loki/api/v1/labels"
	labelValuesPath   = "/loki/api/v1/label/%s/values"
	seriesPath        = "/loki/api/v1/series"
	tailPath          = "/loki/api/v1/tail"
	defaultAuthHeader = "Authorization"
)

var userAgent = fmt.Sprintf("loki-logcli/%s", build.Version)

// Client contains all the methods to query a Loki instance, it's an interface to allow multiple implementations.
type Client interface {
	Query(queryStr string, limit int, time time.Time, direction logproto.Direction, quiet bool) (*loghttp.QueryResponse, error)
	QueryRange(queryStr string, limit int, start, end time.Time, direction logproto.Direction, step, interval time.Duration, quiet bool) (*loghttp.QueryResponse, error)
	ListLabelNames(quiet bool, start, end time.Time) (*loghttp.LabelResponse, error)
	ListLabelValues(name string, quiet bool, start, end time.Time) (*loghttp.LabelResponse, error)
	Series(matchers []string, start, end time.Time, quiet bool) (*loghttp.SeriesResponse, error)
	LiveTailQueryConn(queryStr string, delayFor time.Duration, limit int, start time.Time, quiet bool) (*websocket.Conn, error)
	GetOrgID() string
}

// Tripperware can wrap a roundtripper.
type Tripperware func(http.RoundTripper) http.RoundTripper
type BackoffConfig struct {
	MaxBackoff int
	MinBackoff int
}

// Client contains fields necessary to query a Loki instance
type DefaultClient struct {
	TLSConfig       config.TLSConfig
	Username        string
	Password        string
	Address         string
	OrgID           string
	Tripperware     Tripperware
	BearerToken     string
	BearerTokenFile string
	Retries         int
	QueryTags       string
	AuthHeader      string
	ProxyURL        string
	BackoffConfig   BackoffConfig
}

// Query uses the /api/v1/query endpoint to execute an instant query
// excluding interfacer b/c it suggests taking the interface promql.Node instead of logproto.Direction b/c it happens to have a String() method
// nolint:interfacer
func (c *DefaultClient) Query(queryStr string, limit int, time time.Time, direction logproto.Direction, quiet bool) (*loghttp.QueryResponse, error) {
	qsb := util.NewQueryStringBuilder()
	qsb.SetString("query", queryStr)
	qsb.SetInt("limit", int64(limit))
	qsb.SetInt("time", time.UnixNano())
	qsb.SetString("direction", direction.String())

	return c.doQuery(queryPath, qsb.Encode(), quiet)
}

// QueryRange uses the /api/v1/query_range endpoint to execute a range query
// excluding interfacer b/c it suggests taking the interface promql.Node instead of logproto.Direction b/c it happens to have a String() method
// nolint:interfacer
func (c *DefaultClient) QueryRange(queryStr string, limit int, start, end time.Time, direction logproto.Direction, step, interval time.Duration, quiet bool) (*loghttp.QueryResponse, error) {
	params := util.NewQueryStringBuilder()
	params.SetString("query", queryStr)
	params.SetInt32("limit", limit)
	params.SetInt("start", start.UnixNano())
	params.SetInt("end", end.UnixNano())
	params.SetString("direction", direction.String())

	// The step is optional, so we do set it only if provided,
	// otherwise we do leverage on the API defaults
	if step != 0 {
		params.SetFloat("step", step.Seconds())
	}

	if interval != 0 {
		params.SetFloat("interval", interval.Seconds())
	}

	return c.doQuery(queryRangePath, params.Encode(), quiet)
}

// ListLabelNames uses the /api/v1/label endpoint to list label names
func (c *DefaultClient) ListLabelNames(quiet bool, start, end time.Time) (*loghttp.LabelResponse, error) {
	var labelResponse loghttp.LabelResponse
	params := util.NewQueryStringBuilder()
	params.SetInt("start", start.UnixNano())
	params.SetInt("end", end.UnixNano())

	if err := c.doRequest(labelsPath, params.Encode(), quiet, &labelResponse); err != nil {
		return nil, err
	}
	return &labelResponse, nil
}

// ListLabelValues uses the /api/v1/label endpoint to list label values
func (c *DefaultClient) ListLabelValues(name string, quiet bool, start, end time.Time) (*loghttp.LabelResponse, error) {
	path := fmt.Sprintf(labelValuesPath, url.PathEscape(name))
	var labelResponse loghttp.LabelResponse
	params := util.NewQueryStringBuilder()
	params.SetInt("start", start.UnixNano())
	params.SetInt("end", end.UnixNano())
	if err := c.doRequest(path, params.Encode(), quiet, &labelResponse); err != nil {
		return nil, err
	}
	return &labelResponse, nil
}

func (c *DefaultClient) Series(matchers []string, start, end time.Time, quiet bool) (*loghttp.SeriesResponse, error) {
	params := util.NewQueryStringBuilder()
	params.SetInt("start", start.UnixNano())
	params.SetInt("end", end.UnixNano())
	params.SetStringArray("match", matchers)

	var seriesResponse loghttp.SeriesResponse
	if err := c.doRequest(seriesPath, params.Encode(), quiet, &seriesResponse); err != nil {
		return nil, err
	}
	return &seriesResponse, nil
}

// LiveTailQueryConn uses /api/prom/tail to set up a websocket connection and returns it
func (c *DefaultClient) LiveTailQueryConn(queryStr string, delayFor time.Duration, limit int, start time.Time, quiet bool) (*websocket.Conn, error) {
	params := util.NewQueryStringBuilder()
	params.SetString("query", queryStr)
	if delayFor != 0 {
		params.SetInt("delay_for", int64(delayFor.Seconds()))
	}
	params.SetInt("limit", int64(limit))
	params.SetInt("start", start.UnixNano())

	return c.wsConnect(tailPath, params.Encode(), quiet)
}

func (c *DefaultClient) GetOrgID() string {
	return c.OrgID
}

func (c *DefaultClient) doQuery(path string, query string, quiet bool) (*loghttp.QueryResponse, error) {
	var err error
	var r loghttp.QueryResponse

	if err = c.doRequest(path, query, quiet, &r); err != nil {
		return nil, err
	}

	return &r, nil
}

func (c *DefaultClient) doRequest(path, query string, quiet bool, out interface{}) error {
	us, err := buildURL(c.Address, path, query)
	if err != nil {
		return err
	}
	if !quiet {
		log.Print(us)
	}

	req, err := http.NewRequest("GET", us, nil)
	if err != nil {
		return err
	}

	h, err := c.getHTTPRequestHeader()
	if err != nil {
		return err
	}
	req.Header = h

	// Parse the URL to extract the host
	clientConfig := config.HTTPClientConfig{
		TLSConfig: c.TLSConfig,
	}

	if c.ProxyURL != "" {
		prox, err := url.Parse(c.ProxyURL)
		if err != nil {
			return err
		}
		clientConfig.ProxyURL = config.URL{URL: prox}
	}

	client, err := config.NewClientFromConfig(clientConfig, "promtail", config.WithHTTP2Disabled())
	if err != nil {
		return err
	}
	if c.Tripperware != nil {
		client.Transport = c.Tripperware(client.Transport)
	}

	var resp *http.Response

	success := false

	bkcfg := backoff.Config{
		MinBackoff: time.Duration(c.BackoffConfig.MinBackoff) * time.Second,
		MaxBackoff: time.Duration(c.BackoffConfig.MaxBackoff) * time.Second,
		// 0 max-retries for backoff means infinite number of retries.
		MaxRetries: c.Retries + 1,
	}
	backoff := backoff.New(context.Background(), bkcfg)

	for {
		if !backoff.Ongoing() {
			break
		}
		resp, err = client.Do(req)
		if err != nil {
			log.Println("error sending request", err)
			backoff.Wait()
			continue
		}
		if resp.StatusCode/100 != 2 {
			buf, _ := io.ReadAll(resp.Body) // nolint
			log.Printf("Error response from server: %s (%v) attempts remaining: %d", string(buf), err, c.Retries-backoff.NumRetries())
			if err := resp.Body.Close(); err != nil {
				log.Println("error closing body", err)
			}
			backoff.Wait()
			continue
		}
		success = true

		break

	}
	if !success {
		return fmt.Errorf("run out of attempts while querying the server")
	}

	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Println("error closing body", err)
		}
	}()
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *DefaultClient) getHTTPRequestHeader() (http.Header, error) {
	h := make(http.Header)

	if c.Username != "" && c.Password != "" {
		if c.AuthHeader == "" {
			c.AuthHeader = defaultAuthHeader
		}
		h.Set(
			c.AuthHeader,
			"Basic "+base64.StdEncoding.EncodeToString([]byte(c.Username+":"+c.Password)),
		)
	}

	h.Set("User-Agent", userAgent)

	if c.OrgID != "" {
		h.Set("X-Scope-OrgID", c.OrgID)
	}

	if c.QueryTags != "" {
		h.Set("X-Query-Tags", c.QueryTags)
	}

	if (c.Username != "" || c.Password != "") && (len(c.BearerToken) > 0 || len(c.BearerTokenFile) > 0) {
		return nil, fmt.Errorf("at most one of HTTP basic auth (username/password), bearer-token & bearer-token-file is allowed to be configured")
	}

	if len(c.BearerToken) > 0 && len(c.BearerTokenFile) > 0 {
		return nil, fmt.Errorf("at most one of the options bearer-token & bearer-token-file is allowed to be configured")
	}

	if c.BearerToken != "" {
		if c.AuthHeader == "" {
			c.AuthHeader = defaultAuthHeader
		}

		h.Set(c.AuthHeader, "Bearer "+c.BearerToken)
	}

	if c.BearerTokenFile != "" {
		b, err := os.ReadFile(c.BearerTokenFile)
		if err != nil {
			return nil, fmt.Errorf("unable to read authorization credentials file %s: %s", c.BearerTokenFile, err)
		}
		bearerToken := strings.TrimSpace(string(b))
		if c.AuthHeader == "" {
			c.AuthHeader = defaultAuthHeader
		}
		h.Set(c.AuthHeader, "Bearer "+bearerToken)
	}
	return h, nil
}

func (c *DefaultClient) wsConnect(path, query string, quiet bool) (*websocket.Conn, error) {
	us, err := buildURL(c.Address, path, query)
	if err != nil {
		return nil, err
	}

	tlsConfig, err := config.NewTLSConfig(&c.TLSConfig)
	if err != nil {
		return nil, err
	}

	if strings.HasPrefix(us, "http") {
		us = strings.Replace(us, "http", "ws", 1)
	}

	if !quiet {
		log.Println(us)
	}

	h, err := c.getHTTPRequestHeader()
	if err != nil {
		return nil, err
	}

	ws := websocket.Dialer{
		TLSClientConfig: tlsConfig,
	}

	if c.ProxyURL != "" {
		ws.Proxy = func(req *http.Request) (*url.URL, error) {
			return url.Parse(c.ProxyURL)
		}
	}

	conn, resp, err := ws.Dial(us, h)
	if err != nil {
		if resp == nil {
			return nil, err
		}
		buf, _ := io.ReadAll(resp.Body) // nolint
		return nil, fmt.Errorf("Error response from server: %s (%v)", string(buf), err)
	}

	return conn, nil
}

// buildURL concats a url `http://foo/bar` with a path `/buzz`.
func buildURL(u, p, q string) (string, error) {
	url, err := url.Parse(u)
	if err != nil {
		return "", err
	}
	url.Path = path.Join(url.Path, p)
	url.RawQuery = q
	return url.String(), nil
}
