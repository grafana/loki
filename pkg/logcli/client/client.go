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

	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/logql"

	"github.com/gorilla/websocket"
	"github.com/prometheus/common/config"
	"github.com/prometheus/prometheus/promql"

	"github.com/grafana/loki/pkg/logproto"
)

const (
	queryPath       = "/loki/api/v1/query?query=%s&limit=%d&time=%d&direction=%s"
	queryRangePath  = "/loki/api/v1/query_range?query=%s&limit=%d&start=%d&end=%d&direction=%s"
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

// QueryResult contains fields necessary to return data from Loki endpoints
type QueryResult struct {
	ResultType promql.ValueType
	Result     interface{}
}

// Query uses the /api/v1/query endpoint to execute an instant query
// excluding interfacer b/c it suggests taking the interface promql.Node instead of logproto.Direction b/c it happens to have a String() method
// nolint:interfacer
func (c *Client) Query(queryStr string, limit int, time time.Time, direction logproto.Direction, quiet bool) (*QueryResult, error) {
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
func (c *Client) QueryRange(queryStr string, limit int, from, through time.Time, direction logproto.Direction, quiet bool) (*QueryResult, error) {
	path := fmt.Sprintf(queryRangePath,
		url.QueryEscape(queryStr), // query
		limit,                     // limit
		from.UnixNano(),           // start
		through.UnixNano(),        // end
		direction.String(),        // direction
	)

	return c.doQuery(path, quiet)
}

// ListLabelNames uses the /api/v1/label endpoint to list label names
func (c *Client) ListLabelNames(quiet bool) (*logproto.LabelResponse, error) {
	var labelResponse logproto.LabelResponse
	if err := c.doRequest(labelsPath, quiet, &labelResponse); err != nil {
		return nil, err
	}
	return &labelResponse, nil
}

// ListLabelValues uses the /api/v1/label endpoint to list label values
func (c *Client) ListLabelValues(name string, quiet bool) (*logproto.LabelResponse, error) {
	path := fmt.Sprintf(labelValuesPath, url.PathEscape(name))
	var labelResponse logproto.LabelResponse
	if err := c.doRequest(path, quiet, &labelResponse); err != nil {
		return nil, err
	}
	return &labelResponse, nil
}

func (c *Client) doQuery(path string, quiet bool) (*QueryResult, error) {
	var err error

	unmarshal := struct {
		Type   promql.ValueType `json:"resultType"`
		Result json.RawMessage  `json:"result"`
	}{}

	if err = c.doRequest(path, quiet, &unmarshal); err != nil {
		return nil, err
	}

	var value interface{}

	// unmarshal results
	switch unmarshal.Type {
	case logql.ValueTypeStreams:
		var s logql.Streams
		err = json.Unmarshal(unmarshal.Result, &s)
		value = s
	case promql.ValueTypeMatrix:
		var m model.Matrix
		err = json.Unmarshal(unmarshal.Result, &m)
		value = m
	case promql.ValueTypeVector:
		var m model.Vector
		err = json.Unmarshal(unmarshal.Result, &m)
		value = m
	default:
		return nil, fmt.Errorf("Unknown type: %s", unmarshal.Type)
	}

	if err != nil {
		return nil, err
	}

	return &QueryResult{
		ResultType: unmarshal.Type,
		Result:     value,
	}, nil
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

	client, err := config.NewClientFromConfig(clientConfig, "logcli")
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
