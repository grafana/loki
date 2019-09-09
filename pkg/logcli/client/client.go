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

	"github.com/grafana/loki/pkg/logql"

	"github.com/gorilla/websocket"
	"github.com/prometheus/common/config"
	"github.com/prometheus/prometheus/promql"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/querier"
)

const (
	queryPath       = "/api/v1/query_range?query=%s&limit=%d&start=%d&end=%d&direction=%s"
	labelsPath      = "/api/v1/label"
	labelValuesPath = "/api/v1/label/%s/values"
	tailPath        = "/api/prom/tail?query=%s&delay_for=%d&limit=%d&start=%d"
)

type Client struct {
	TLSConfig config.TLSConfig
	Username  string
	Password  string
	Address   string
}

func (c *Client) QueryRange(queryStr string, limit int, from, through time.Time, direction logproto.Direction, quiet bool) (*querier.QueryResponse, error) {
	var err error

	path := fmt.Sprintf(queryPath,
		url.QueryEscape(queryStr), // query
		limit,                     // limit
		from.UnixNano(),           // start
		through.UnixNano(),        // end
		direction.String(),        // direction
	)

	unmarshal := struct {
		Type   promql.ValueType `json:"resultType"`
		Result json.RawMessage  `json:"result"`
	}{}

	if err = c.doRequest(path, quiet, &unmarshal); err != nil {
		return nil, err
	}

	var value promql.Value

	// unmarshal results
	switch unmarshal.Type {
	case logql.ValueTypeStreams:
		var s logql.Streams
		err = json.Unmarshal(unmarshal.Result, &s)
		value = s
	case promql.ValueTypeMatrix:
		var m promql.Matrix
		err = json.Unmarshal(unmarshal.Result, &m)
		value = m
	default:
		return nil, fmt.Errorf("Unknown type: %s", unmarshal.Type)
	}

	if err != nil {
		return nil, err
	}

	return &querier.QueryResponse{
		ResultType: unmarshal.Type,
		Result:     value,
	}, nil
}

func (c *Client) ListLabelNames(quiet bool) (*logproto.LabelResponse, error) {
	var labelResponse logproto.LabelResponse
	if err := c.doRequest(labelsPath, quiet, &labelResponse); err != nil {
		return nil, err
	}
	return &labelResponse, nil
}

func (c *Client) ListLabelValues(name string, quiet bool) (*logproto.LabelResponse, error) {
	path := fmt.Sprintf(labelValuesPath, url.PathEscape(name))
	var labelResponse logproto.LabelResponse
	if err := c.doRequest(path, quiet, &labelResponse); err != nil {
		return nil, err
	}
	return &labelResponse, nil
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
