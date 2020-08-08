package client

import (
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	json "github.com/json-iterator/go"
	"github.com/prometheus/common/config"

	"github.com/grafana/loki/pkg/build"
	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/util"
)

const (
	queryPath       = "/loki/api/v1/query"
	queryRangePath  = "/loki/api/v1/query_range"
	labelsPath      = "/loki/api/v1/labels"
	labelValuesPath = "/loki/api/v1/label/%s/values"
	seriesPath      = "/loki/api/v1/series"
	tailPath        = "/loki/api/v1/tail"
)

var (
	userAgent = fmt.Sprintf("loki-logcli/%s", build.Version)
)

// Client contains all the methods to query a Loki instance, it's an interface to allow multiple implementations.
type Client interface {
	Query(queryStr string, limit int, time time.Time, direction logproto.Direction, quiet bool) (*loghttp.QueryResponse, error)
	QueryRange(queryStr string, limit int, from, through time.Time, direction logproto.Direction, step, interval time.Duration, quiet bool) (*loghttp.QueryResponse, error)
	ListLabelNames(quiet bool, from, through time.Time) (*loghttp.LabelResponse, error)
	ListLabelValues(name string, quiet bool, from, through time.Time) (*loghttp.LabelResponse, error)
	Series(matchers []string, from, through time.Time, quiet bool) (*loghttp.SeriesResponse, error)
	LiveTailQueryConn(queryStr string, delayFor int, limit int, from int64, quiet bool) (*websocket.Conn, error)
	GetOrgID() string
}

// Client contains fields necessary to query a Loki instance
type DefaultClient struct {
	TLSConfig config.TLSConfig
	Username  string
	Password  string
	Address   string
	OrgID     string
}

// Query uses the /api/v1/query endpoint to execute an instant query
// excluding interfacer b/c it suggests taking the interface promql.Node instead of logproto.Direction b/c it happens to have a String() method
// nolint:interfacer
func (c *DefaultClient) Query(queryStr string, limit int, time time.Time, direction logproto.Direction, quiet bool) (*loghttp.QueryResponse, error) {
	qsb := util.NewQueryStringBuilder()
	qsb.SetString("query", queryStr)
	qsb.SetInt("limit", int64(limit))
	qsb.SetInt("start", time.UnixNano())
	qsb.SetString("direction", direction.String())

	return c.doQuery(queryPath, qsb.Encode(), quiet)
}

// QueryRange uses the /api/v1/query_range endpoint to execute a range query
// excluding interfacer b/c it suggests taking the interface promql.Node instead of logproto.Direction b/c it happens to have a String() method
// nolint:interfacer
func (c *DefaultClient) QueryRange(queryStr string, limit int, from, through time.Time, direction logproto.Direction, step, interval time.Duration, quiet bool) (*loghttp.QueryResponse, error) {
	params := util.NewQueryStringBuilder()
	params.SetString("query", queryStr)
	params.SetInt32("limit", limit)
	params.SetInt("start", from.UnixNano())
	params.SetInt("end", through.UnixNano())
	params.SetString("direction", direction.String())

	// The step is optional, so we do set it only if provided,
	// otherwise we do leverage on the API defaults
	if step != 0 {
		params.SetInt("step", int64(step.Seconds()))
	}

	if interval != 0 {
		params.SetInt("interval", int64(interval.Seconds()))
	}

	return c.doQuery(queryRangePath, params.Encode(), quiet)
}

// ListLabelNames uses the /api/v1/label endpoint to list label names
func (c *DefaultClient) ListLabelNames(quiet bool, from, through time.Time) (*loghttp.LabelResponse, error) {
	var labelResponse loghttp.LabelResponse
	params := util.NewQueryStringBuilder()
	params.SetInt("start", from.UnixNano())
	params.SetInt("end", through.UnixNano())

	if err := c.doRequest(labelsPath, params.Encode(), quiet, &labelResponse); err != nil {
		return nil, err
	}
	return &labelResponse, nil
}

// ListLabelValues uses the /api/v1/label endpoint to list label values
func (c *DefaultClient) ListLabelValues(name string, quiet bool, from, through time.Time) (*loghttp.LabelResponse, error) {
	path := fmt.Sprintf(labelValuesPath, url.PathEscape(name))
	var labelResponse loghttp.LabelResponse
	params := util.NewQueryStringBuilder()
	params.SetInt("start", from.UnixNano())
	params.SetInt("end", through.UnixNano())
	if err := c.doRequest(path, params.Encode(), quiet, &labelResponse); err != nil {
		return nil, err
	}
	return &labelResponse, nil
}

func (c *DefaultClient) Series(matchers []string, from, through time.Time, quiet bool) (*loghttp.SeriesResponse, error) {
	params := util.NewQueryStringBuilder()
	params.SetInt("start", from.UnixNano())
	params.SetInt("end", through.UnixNano())
	params.SetStringArray("match", matchers)

	var seriesResponse loghttp.SeriesResponse
	if err := c.doRequest(seriesPath, params.Encode(), quiet, &seriesResponse); err != nil {
		return nil, err
	}
	return &seriesResponse, nil
}

// LiveTailQueryConn uses /api/prom/tail to set up a websocket connection and returns it
func (c *DefaultClient) LiveTailQueryConn(queryStr string, delayFor int, limit int, from int64, quiet bool) (*websocket.Conn, error) {
	qsb := util.NewQueryStringBuilder()
	qsb.SetString("query", queryStr)
	qsb.SetInt("delay_for", int64(delayFor))
	qsb.SetInt("limit", int64(limit))
	qsb.SetInt("from", from)

	return c.wsConnect(tailPath, qsb.Encode(), quiet)
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

	req.SetBasicAuth(c.Username, c.Password)
	req.Header.Set("User-Agent", userAgent)

	if c.OrgID != "" {
		req.Header.Set("X-Scope-OrgID", c.OrgID)
	}

	// Parse the URL to extract the host
	clientConfig := config.HTTPClientConfig{
		TLSConfig: c.TLSConfig,
	}

	client, err := config.NewClientFromConfig(clientConfig, "logcli", false)
	if err != nil {
		return err
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Println("error closing body", err)
		}
	}()

	if resp.StatusCode/100 != 2 {
		buf, _ := ioutil.ReadAll(resp.Body) // nolint
		return fmt.Errorf("Error response from server: %s (%v)", string(buf), err)
	}

	return json.NewDecoder(resp.Body).Decode(out)
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

	if strings.HasPrefix(us, "https") {
		us = strings.Replace(us, "https", "wss", 1)
	} else if strings.HasPrefix(us, "http") {
		us = strings.Replace(us, "http", "ws", 1)
	}
	if !quiet {
		log.Println(us)
	}

	h := http.Header{"Authorization": {"Basic " + base64.StdEncoding.EncodeToString([]byte(c.Username+":"+c.Password))}}

	if c.OrgID != "" {
		h.Set("X-Scope-OrgID", c.OrgID)
	}

	ws := websocket.Dialer{
		TLSClientConfig: tlsConfig,
	}

	conn, resp, err := ws.Dial(us, h)

	if err != nil {
		if resp == nil {
			return nil, err
		}
		buf, _ := ioutil.ReadAll(resp.Body) // nolint
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
