package reader

import (
	"encoding/base64"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	json "github.com/json-iterator/go"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/pkg/build"
	loghttp "github.com/grafana/loki/pkg/loghttp/legacy"
	"github.com/grafana/loki/pkg/logproto"
)

var (
	reconnects = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "loki_canary",
		Name:      "ws_reconnects",
		Help:      "counts every time the websocket connection has to reconnect",
	})
	userAgent = fmt.Sprintf("loki-canary/%s", build.Version)
)

type LokiReader interface {
	Query(start time.Time, end time.Time) ([]time.Time, error)
}

type Reader struct {
	header       http.Header
	tls          bool
	addr         string
	user         string
	pass         string
	sName        string
	sValue       string
	lName        string
	lVal         string
	conn         *websocket.Conn
	w            io.Writer
	recv         chan time.Time
	quit         chan struct{}
	shuttingDown bool
	done         chan struct{}
}

func NewReader(writer io.Writer, receivedChan chan time.Time, tls bool,
	address string, user string, pass string, labelName string, labelVal string, streamName string, streamValue string) *Reader {
	h := http.Header{}
	if user != "" {
		h = http.Header{"Authorization": {"Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))}}
	}

	rd := Reader{
		header:       h,
		tls:          tls,
		addr:         address,
		user:         user,
		pass:         pass,
		sName:        streamName,
		sValue:       streamValue,
		lName:        labelName,
		lVal:         labelVal,
		w:            writer,
		recv:         receivedChan,
		quit:         make(chan struct{}),
		done:         make(chan struct{}),
		shuttingDown: false,
	}

	go rd.run()

	go func() {
		<-rd.quit
		if rd.conn != nil {
			fmt.Fprintf(rd.w, "shutting down reader\n")
			rd.shuttingDown = true
			_ = rd.conn.Close()
		}
	}()

	return &rd
}

func (r *Reader) Stop() {
	if r.quit != nil {
		close(r.quit)
		<-r.done
		r.quit = nil
	}
}

func (r *Reader) Query(start time.Time, end time.Time) ([]time.Time, error) {
	scheme := "http"
	if r.tls {
		scheme = "https"
	}
	u := url.URL{
		Scheme: scheme,
		Host:   r.addr,
		Path:   "/api/prom/query",
		RawQuery: fmt.Sprintf("start=%d&end=%d", start.UnixNano(), end.UnixNano()) +
			"&query=" + url.QueryEscape(fmt.Sprintf("{%v=\"%v\",%v=\"%v\"}", r.sName, r.sValue, r.lName, r.lVal)) +
			"&limit=1000",
	}
	fmt.Fprintf(r.w, "Querying loki for missing values with query: %v\n", u.String())

	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return nil, err
	}

	req.SetBasicAuth(r.user, r.pass)
	req.Header.Set("User-Agent", userAgent)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Println("error closing body", err)
		}
	}()

	if resp.StatusCode/100 != 2 {
		buf, _ := ioutil.ReadAll(resp.Body)
		return nil, fmt.Errorf("error response from server: %s (%v)", string(buf), err)
	}
	var decoded logproto.QueryResponse
	err = json.NewDecoder(resp.Body).Decode(&decoded)
	if err != nil {
		return nil, err
	}

	tss := []time.Time{}

	for _, stream := range decoded.Streams {
		for _, entry := range stream.Entries {
			ts, err := parseResponse(&entry)
			if err != nil {
				fmt.Fprint(r.w, err)
				continue
			}
			tss = append(tss, *ts)
		}

	}

	return tss, nil
}

func (r *Reader) run() {

	r.closeAndReconnect()

	tailResponse := &loghttp.TailResponse{}

	for {
		err := r.conn.ReadJSON(tailResponse)
		if err != nil {
			if r.shuttingDown {
				close(r.done)
				return
			}
			fmt.Fprintf(r.w, "error reading websocket, will retry in 10 seconds: %s\n", err)
			// Even though we sleep between connection retries, we found it's possible to DOS Loki if the connection
			// succeeds but some other error is returned, so also sleep here before retrying.
			<-time.After(10 * time.Second)
			r.closeAndReconnect()
			continue
		}
		for _, stream := range tailResponse.Streams {
			for _, entry := range stream.Entries {
				ts, err := parseResponse(&entry)
				if err != nil {
					fmt.Fprint(r.w, err)
					continue
				}
				r.recv <- *ts
			}
		}
	}
}

func (r *Reader) closeAndReconnect() {
	if r.conn != nil {
		_ = r.conn.Close()
		r.conn = nil
		// By incrementing reconnects here we should only count a failure followed by a successful reconnect.
		// Initial connections and reconnections from failed tries will not be counted.
		reconnects.Inc()
	}
	for r.conn == nil {
		scheme := "ws"
		if r.tls {
			scheme = "wss"
		}
		u := url.URL{
			Scheme:   scheme,
			Host:     r.addr,
			Path:     "/api/prom/tail",
			RawQuery: "query=" + url.QueryEscape(fmt.Sprintf("{%v=\"%v\",%v=\"%v\"}", r.sName, r.sValue, r.lName, r.lVal)),
		}

		fmt.Fprintf(r.w, "Connecting to loki at %v, querying for label '%v' with value '%v'\n", u.String(), r.lName, r.lVal)

		c, _, err := websocket.DefaultDialer.Dial(u.String(), r.header)
		if err != nil {
			fmt.Fprintf(r.w, "failed to connect to %s with err %s\n", u.String(), err)
			<-time.After(10 * time.Second)
			continue
		}
		r.conn = c
	}
}

func parseResponse(entry *logproto.Entry) (*time.Time, error) {
	sp := strings.Split(entry.Line, " ")
	if len(sp) != 2 {
		return nil, errors.Errorf("received invalid entry: %s\n", entry.Line)
	}
	ts, err := strconv.ParseInt(sp[0], 10, 64)
	if err != nil {
		return nil, errors.Errorf("failed to parse timestamp: %s\n", sp[0])
	}
	t := time.Unix(0, ts)
	return &t, nil
}
