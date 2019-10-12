package client

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/util"

	"github.com/gorilla/websocket"
	"github.com/prometheus/common/config"

	"github.com/grafana/loki/pkg/logproto"
)

const (
	queryPath       = "/loki/api/v1/query?query=%s&limit=%d&time=%d&direction=%s"
	queryRangePath  = "/loki/api/v1/query_range"
	labelsPath      = "/loki/api/v1/label"
	labelValuesPath = "/loki/api/v1/label/%s/values"
	tailPath        = "/loki/api/v1/tail?query=%s&delay_for=%d&limit=%d&start=%d"
)

// Client contains fields necessary to query a Loki instance
type Client struct {
	TLSConfig config.TLSConfig
	Username  string
	Password  string
	Address   string
}

// Query uses the /api/v1/query endpoint to execute an instant query
// excluding interfacer b/c it suggests taking the interface promql.Node instead of logproto.Direction b/c it happens to have a String() method
// nolint:interfacer
func (c *Client) Query(queryStr string, limit int, time time.Time, direction logproto.Direction, quiet bool) (*loghttp.QueryResponse, error) {
	path := fmt.Sprintf(queryPath,
		url.QueryEscape(queryStr), // query
		limit,                     // limit
		time.UnixNano(),           // start
		direction.String(),        // direction
	)

	return c.doQuery(path, quiet)
}

// QueryRange uses the /api/v1/query_range endpoint to execute a range query
// excluding interfacer b/c it suggests taking the interface promql.Node instead of logproto.Direction b/c it happens to have a String() method
// nolint:interfacer
func (c *Client) QueryRange(queryStr string, limit int, from, through time.Time, direction logproto.Direction, step time.Duration, quiet bool) (*loghttp.QueryResponse, error) {
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

	return c.doQuery(params.EncodeWithPath(queryRangePath), quiet)
}

// ListLabelNames uses the /api/v1/label endpoint to list label names
func (c *Client) ListLabelNames(quiet bool) (*loghttp.LabelResponse, error) {
	var labelResponse loghttp.LabelResponse
	if err := c.doRequest(labelsPath, quiet, &labelResponse); err != nil {
		return nil, err
	}
	return &labelResponse, nil
}

// ListLabelValues uses the /api/v1/label endpoint to list label values
func (c *Client) ListLabelValues(name string, quiet bool) (*loghttp.LabelResponse, error) {
	path := fmt.Sprintf(labelValuesPath, url.PathEscape(name))
	var labelResponse loghttp.LabelResponse
	if err := c.doRequest(path, quiet, &labelResponse); err != nil {
		return nil, err
	}
	return &labelResponse, nil
}

func (c *Client) doQuery(path string, quiet bool) (*loghttp.QueryResponse, error) {
	var err error
	var r loghttp.QueryResponse

	if err = c.doRequest(path, quiet, &r); err != nil {
		return nil, err
	}

	return &r, nil
}

func (c *Client) doRequest(path string, quiet bool, out interface{}) error {
	us := c.Address + path
	if !quiet {
		log.Print(us)
	}

	req, err := http.NewRequest("GET", us, nil)
	if err != nil {
		return err
	}

	req.SetBasicAuth(c.Username, c.Password)

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

// LiveTailQueryConn uses /api/prom/tail to set up a websocket connection and returns it
func (c *Client) LiveTailQueryConn(queryStr string, delayFor int, limit int, from int64, quiet bool) (*websocket.Conn, error) {
	path := fmt.Sprintf(tailPath,
		url.QueryEscape(queryStr), // query
		delayFor,                  // delay_for
		limit,                     // limit
		from,                      // start
	)
	return c.wsConnect(path, quiet)
}

func (c *Client) wsConnect(path string, quiet bool) (*websocket.Conn, error) {
	us := c.Address + path

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
