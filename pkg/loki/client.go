package loki

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

	"github.com/gorilla/websocket"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/prometheus/common/config"
)

const (
	queryPath       = "/api/prom/query?query=%s&limit=%d&start=%d&end=%d&direction=%s&regexp=%s"
	labelsPath      = "/api/prom/label"
	labelValuesPath = "/api/prom/label/%s/values"
	tailPath        = "/api/prom/tail?query=%s&regexp=%s&delay_for=%d&limit=%d&start=%d"
)

// Client provides functions for logcli and similar programs to
// interact with the Loki HTTP API
type Client struct {
	Name               string
	Address            string
	TLS                *config.TLSConfig
	Username, Password string
}

// Query runs a regular query to the Loki server
func (c Client) Query(
	query, regexp string,
	limit int,
	from, through time.Time,
	direction logproto.Direction,
) (*logproto.QueryResponse, error) {
	path := fmt.Sprintf(queryPath,
		url.QueryEscape(query),  // request
		limit,                   // limit
		from.UnixNano(),         // start
		through.UnixNano(),      // end
		direction.String(),      // direction
		url.QueryEscape(regexp), // regexp
	)

	var resp logproto.QueryResponse
	if err := c.request(path, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

// LabelNames returns all labels known to the loki server
func (c Client) LabelNames() (*logproto.LabelResponse, error) {
	var labelResponse logproto.LabelResponse
	if err := c.request(labelsPath, &labelResponse); err != nil {
		return nil, err
	}
	return &labelResponse, nil
}

// LabelValues returns all values for the label known to the loki server
func (c Client) LabelValues(name string) (*logproto.LabelResponse, error) {
	path := fmt.Sprintf(labelValuesPath, url.PathEscape(name))
	var labelResponse logproto.LabelResponse
	if err := c.request(path, &labelResponse); err != nil {
		return nil, err
	}
	return &labelResponse, nil
}

// TailConn returns an open webSocket subscribed to the live log messages
func (c Client) TailConn(query, regexp string, delayFor, limit int, start time.Time) (*websocket.Conn, error) {
	path := fmt.Sprintf(tailPath,
		url.QueryEscape(query),  // request
		url.QueryEscape(regexp), // regexp
		delayFor,                // delay_for
		limit,                   // limit
		start.UnixNano(),        // start
	)
	return c.wsConnect(path)
}

func (c Client) request(path string, out interface{}) error {
	us := c.Address + path

	// TODO: readd these
	// if !*quiet {
	// 	log.Print(us)
	// }

	req, err := http.NewRequest("GET", us, nil)
	if err != nil {
		return err
	}

	req.SetBasicAuth(c.Username, c.Password)

	clientConfig := config.HTTPClientConfig{TLSConfig: *c.TLS}
	client, err := config.NewClientFromConfig(clientConfig, c.Name)
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

func (c Client) wsConnect(path string) (*websocket.Conn, error) {
	us := c.Address + path

	// TODO: readd these
	// if !*quiet {
	// 	log.Print(us)
	// }

	tlsConfig, err := config.NewTLSConfig(c.TLS)
	if err != nil {
		return nil, err
	}

	if strings.HasPrefix(us, "https") {
		us = strings.Replace(us, "https", "wss", 1)
	} else if strings.HasPrefix(us, "http") {
		us = strings.Replace(us, "http", "ws", 1)
	}

	authHeader := http.Header{
		"Authorization": {"Basic " + base64.StdEncoding.EncodeToString([]byte(c.Username+":"+c.Password))},
	}

	ws := websocket.Dialer{
		TLSClientConfig: tlsConfig,
	}

	conn, resp, err := ws.Dial(us, authHeader)

	if err != nil {
		if resp == nil {
			return nil, err
		}
		buf, _ := ioutil.ReadAll(resp.Body) // nolint
		return nil, fmt.Errorf("Error response from server: %s (%v)", string(buf), err)
	}

	return conn, nil
}
