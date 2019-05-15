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

	"github.com/grafana/loki/pkg/logproto"
)

const (
	queryPath       = "/api/prom/query?query=%s&limit=%d&start=%d&end=%d&direction=%s&regexp=%s"
	labelsPath      = "/api/prom/label"
	labelValuesPath = "/api/prom/label/%s/values"
	tailPath        = "/api/prom/tail?query=%s&regexp=%s"
)

func query(from, through time.Time, direction logproto.Direction) (*logproto.QueryResponse, error) {
	path := fmt.Sprintf(queryPath, url.QueryEscape(*queryStr), *limit, from.UnixNano(),
		through.UnixNano(), direction.String(), url.QueryEscape(*regexpStr))

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
	url := *addr + path
	log.Print(url)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(*username, *password)

	resp, err := http.DefaultClient.Do(req)
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
	path := fmt.Sprintf(tailPath, url.QueryEscape(*queryStr), url.QueryEscape(*regexpStr))
	return wsConnect(path)
}

func wsConnect(path string) (*websocket.Conn, error) {
	url := *addr + path
	if strings.HasPrefix(url, "https") {
		url = strings.Replace(url, "https", "wss", 1)
	} else if strings.HasPrefix(url, "http") {
		url = strings.Replace(url, "http", "ws", 1)
	}
	log.Println(url)

	h := http.Header{"Authorization": {"Basic " + base64.StdEncoding.EncodeToString([]byte(*username+":"+*password))}}
	c, resp, err := websocket.DefaultDialer.Dial(url, h)

	if err != nil {
		if resp == nil {
			return nil, err
		}
		buf, _ := ioutil.ReadAll(resp.Body) // nolint
		return nil, fmt.Errorf("Error response from server: %s (%v)", string(buf), err)
	}

	return c, nil
}
