package main

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
	"github.com/prometheus/common/config"

	"github.com/grafana/loki/pkg/logproto"
)

const (
	queryPath       = "/api/prom/query?query=%s&limit=%d&start=%d&end=%d&direction=%s"
	labelsPath      = "/api/prom/label"
	labelValuesPath = "/api/prom/label/%s/values"
	tailPath        = "/api/prom/tail?query=%s&delay_for=%d&limit=%d&start=%d"
)

func query(from, through time.Time, direction logproto.Direction) (*logproto.QueryResponse, error) {
	path := fmt.Sprintf(queryPath,
		url.QueryEscape(*queryStr), // query
		*limit,                     // limit
		from.UnixNano(),            // start
		through.UnixNano(),         // end
		direction.String(),         // direction
	)

	var resp logproto.QueryResponse
	if err := doRequest(path, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

func listLabelNames() (*logproto.LabelResponse, error) {
	var labelResponse logproto.LabelResponse
	if err := doRequest(labelsPath, &labelResponse); err != nil {
		return nil, err
	}
	return &labelResponse, nil
}

func listLabelValues(name string) (*logproto.LabelResponse, error) {
	path := fmt.Sprintf(labelValuesPath, url.PathEscape(name))
	var labelResponse logproto.LabelResponse
	if err := doRequest(path, &labelResponse); err != nil {
		return nil, err
	}
	return &labelResponse, nil
}

func doRequest(path string, out interface{}) error {
	us := *addr + path
	if !*quiet {
		log.Print(us)
	}

	req, err := http.NewRequest("GET", us, nil)
	if err != nil {
		return err
	}

	req.SetBasicAuth(*username, *password)

	// Parse the URL to extract the host
	u, err := url.Parse(us)
	if err != nil {
		return err
	}
	clientConfig := config.HTTPClientConfig{
		TLSConfig: config.TLSConfig{
			CAFile:             *tlsCACertPath,
			CertFile:           *tlsClientCertPath,
			KeyFile:            *tlsClientCertKeyPath,
			ServerName:         u.Host,
			InsecureSkipVerify: *tlsSkipVerify,
		},
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

func liveTailQueryConn() (*websocket.Conn, error) {
	path := fmt.Sprintf(tailPath,
		url.QueryEscape(*queryStr),      // query
		*delayFor,                       // delay_for
		*limit,                          // limit
		getStart(time.Now()).UnixNano(), // start
	)
	return wsConnect(path)
}

func wsConnect(path string) (*websocket.Conn, error) {
	us := *addr + path

	// Parse the URL to extract the host
	u, err := url.Parse(us)
	if err != nil {
		return nil, err
	}
	tlsConfig, err := config.NewTLSConfig(&config.TLSConfig{
		CAFile:             *tlsCACertPath,
		CertFile:           *tlsClientCertPath,
		KeyFile:            *tlsClientCertKeyPath,
		ServerName:         u.Host,
		InsecureSkipVerify: *tlsSkipVerify,
	})
	if err != nil {
		return nil, err
	}

	if strings.HasPrefix(us, "https") {
		us = strings.Replace(us, "https", "wss", 1)
	} else if strings.HasPrefix(us, "http") {
		us = strings.Replace(us, "http", "ws", 1)
	}
	if !*quiet {
		log.Println(us)
	}

	h := http.Header{"Authorization": {"Basic " + base64.StdEncoding.EncodeToString([]byte(*username+":"+*password))}}

	ws := websocket.Dialer{
		TLSClientConfig: tlsConfig,
	}

	c, resp, err := ws.Dial(us, h)

	if err != nil {
		if resp == nil {
			return nil, err
		}
		buf, _ := ioutil.ReadAll(resp.Body) // nolint
		return nil, fmt.Errorf("Error response from server: %s (%v)", string(buf), err)
	}

	return c, nil
}
