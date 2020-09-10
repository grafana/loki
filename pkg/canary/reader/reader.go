package reader

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/gorilla/websocket"
	json "github.com/json-iterator/go"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/pkg/build"
	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logql"
)

var (
	reconnects = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "loki_canary",
		Name:      "ws_reconnects_total",
		Help:      "counts every time the websocket connection has to reconnect",
	})
	websocketPings = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "loki_canary",
		Name:      "ws_pings_total",
		Help:      "counts every time the websocket receives a ping message",
	})
	userAgent = fmt.Sprintf("loki-canary/%s", build.Version)
)

type LokiReader interface {
	Query(start time.Time, end time.Time) ([]time.Time, error)
	QueryCountOverTime(queryRange string) (float64, error)
}

type Reader struct {
	header       http.Header
	tls          bool
	addr         string
	user         string
	pass         string
	queryTimeout time.Duration
	sName        string
	sValue       string
	lName        string
	lVal         string
	backoff      *util.Backoff
	nextQuery    time.Time
	backoffMtx   sync.RWMutex
	interval     time.Duration
	conn         *websocket.Conn
	w            io.Writer
	recv         chan time.Time
	quit         chan struct{}
	shuttingDown bool
	done         chan struct{}
}

func NewReader(writer io.Writer,
	receivedChan chan time.Time,
	tls bool,
	address string,
	user string,
	pass string,
	queryTimeout time.Duration,
	labelName string,
	labelVal string,
	streamName string,
	streamValue string,
	interval time.Duration) *Reader {
	h := http.Header{}
	if user != "" {
		h = http.Header{"Authorization": {"Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))}}
	}

	next := time.Now()
	bkcfg := util.BackoffConfig{
		MinBackoff: 1 * time.Second,
		MaxBackoff: 10 * time.Minute,
		MaxRetries: 0,
	}
	bkoff := util.NewBackoff(context.Background(), bkcfg)

	rd := Reader{
		header:       h,
		tls:          tls,
		addr:         address,
		user:         user,
		pass:         pass,
		queryTimeout: queryTimeout,
		sName:        streamName,
		sValue:       streamValue,
		lName:        labelName,
		lVal:         labelVal,
		nextQuery:    next,
		backoff:      bkoff,
		interval:     interval,
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

// QueryCountOverTime will ask Loki for a count of logs over the provided range e.g. 5m
// QueryCountOverTime blocks if a previous query has failed until the appropriate backoff time has been reached.
func (r *Reader) QueryCountOverTime(queryRange string) (float64, error) {
	r.backoffMtx.RLock()
	next := r.nextQuery
	r.backoffMtx.RUnlock()
	for time.Now().Before(next) {
		time.Sleep(50 * time.Millisecond)
		// Update next in case other queries have tried and failed
		r.backoffMtx.RLock()
		next = r.nextQuery
		r.backoffMtx.RUnlock()
	}

	scheme := "http"
	if r.tls {
		scheme = "https"
	}
	u := url.URL{
		Scheme: scheme,
		Host:   r.addr,
		Path:   "/loki/api/v1/query",
		RawQuery: "query=" + url.QueryEscape(fmt.Sprintf("count_over_time({%v=\"%v\",%v=\"%v\"}[%s])", r.sName, r.sValue, r.lName, r.lVal, queryRange)) +
			"&limit=1000",
	}
	fmt.Fprintf(r.w, "Querying loki for metric count with query: %v\n", u.String())

	ctx, cancel := context.WithTimeout(context.Background(), r.queryTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return 0, err
	}

	req.SetBasicAuth(r.user, r.pass)
	req.Header.Set("User-Agent", userAgent)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Println("error closing body", err)
		}
	}()

	if resp.StatusCode/100 != 2 {
		r.backoffMtx.Lock()
		r.nextQuery = nextBackoff(r.w, resp.StatusCode, r.backoff)
		r.backoffMtx.Unlock()
		buf, _ := ioutil.ReadAll(resp.Body)
		return 0, fmt.Errorf("error response from server: %s (%v)", string(buf), err)
	}
	// No Errors, reset backoff
	r.backoffMtx.Lock()
	r.backoff.Reset()
	r.backoffMtx.Unlock()

	var decoded loghttp.QueryResponse
	err = json.NewDecoder(resp.Body).Decode(&decoded)
	if err != nil {
		return 0, err
	}

	value := decoded.Data.Result
	ret := 0.0
	switch value.Type() {
	case loghttp.ResultTypeVector:
		samples := value.(loghttp.Vector)
		if len(samples) > 1 {
			return 0, fmt.Errorf("expected only a single result in the metric test query vector, instead received %v", len(samples))
		}
		if len(samples) == 0 {
			return 0, fmt.Errorf("expected to receive one sample in the result vector, received 0")
		}
		ret = float64(samples[0].Value)
	default:
		return 0, fmt.Errorf("unexpected result type, expected a Vector result instead received %v", value.Type())
	}

	return ret, nil
}

// Query will ask Loki for all canary timestamps in the requested timerange.
// Query blocks if a previous query has failed until the appropriate backoff time has been reached.
func (r *Reader) Query(start time.Time, end time.Time) ([]time.Time, error) {
	r.backoffMtx.RLock()
	next := r.nextQuery
	r.backoffMtx.RUnlock()
	for time.Now().Before(next) {
		time.Sleep(50 * time.Millisecond)
		// Update next in case other queries have tried and failed moving it even farther in the future
		r.backoffMtx.RLock()
		next = r.nextQuery
		r.backoffMtx.RUnlock()
	}

	scheme := "http"
	if r.tls {
		scheme = "https"
	}
	u := url.URL{
		Scheme: scheme,
		Host:   r.addr,
		Path:   "/loki/api/v1/query_range",
		RawQuery: fmt.Sprintf("start=%d&end=%d", start.UnixNano(), end.UnixNano()) +
			"&query=" + url.QueryEscape(fmt.Sprintf("{%v=\"%v\",%v=\"%v\"}", r.sName, r.sValue, r.lName, r.lVal)) +
			"&limit=1000",
	}
	fmt.Fprintf(r.w, "Querying loki for logs with query: %v\n", u.String())

	ctx, cancel := context.WithTimeout(context.Background(), r.queryTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
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
		r.backoffMtx.Lock()
		r.nextQuery = nextBackoff(r.w, resp.StatusCode, r.backoff)
		r.backoffMtx.Unlock()
		buf, _ := ioutil.ReadAll(resp.Body)
		return nil, fmt.Errorf("error response from server: %s (%v)", string(buf), err)
	}
	// No Errors, reset backoff
	r.backoffMtx.Lock()
	r.backoff.Reset()
	r.backoffMtx.Unlock()

	var decoded loghttp.QueryResponse
	err = json.NewDecoder(resp.Body).Decode(&decoded)
	if err != nil {
		return nil, err
	}

	tss := []time.Time{}
	value := decoded.Data.Result
	switch value.Type() {
	case logql.ValueTypeStreams:
		for _, stream := range value.(loghttp.Streams) {
			for _, entry := range stream.Entries {
				ts, err := parseResponse(&entry)
				if err != nil {
					fmt.Fprint(r.w, err)
					continue
				}
				tss = append(tss, *ts)
			}

		}
	default:
		return nil, fmt.Errorf("unexpected result type, expected a log stream result instead received %v", value.Type())
	}

	return tss, nil
}

func (r *Reader) run() {

	r.closeAndReconnect()

	tailResponse := &loghttp.TailResponse{}
	lastMessage := time.Now()

	for {
		if r.shuttingDown {
			if r.conn != nil {
				_ = r.conn.Close()
				r.conn = nil
			}
			close(r.done)
			return
		}

		// Set a read timeout of 10x the interval we expect to see messages
		// Ignore the error as it will get caught when we call ReadJSON
		_ = r.conn.SetReadDeadline(time.Now().Add(10 * r.interval))
		err := r.conn.ReadJSON(tailResponse)
		if err != nil {
			fmt.Fprintf(r.w, "error reading websocket, will retry in 10 seconds: %s\n", err)
			// Even though we sleep between connection retries, we found it's possible to DOS Loki if the connection
			// succeeds but some other error is returned, so also sleep here before retrying.
			<-time.After(10 * time.Second)
			r.closeAndReconnect()
			continue
		}
		// If there were streams update last message timestamp
		if len(tailResponse.Streams) > 0 {
			lastMessage = time.Now()
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
		// Ping messages can reset the read deadline so also make sure we are receiving regular messages.
		// We use the same 10x interval to make sure we are getting recent messages
		if time.Since(lastMessage).Milliseconds() > 10*r.interval.Milliseconds() {
			fmt.Fprintf(r.w, "Have not received a canary message from loki on the websocket in %vms, "+
				"sleeping 10s and reconnecting\n", 10*r.interval.Milliseconds())
			<-time.After(10 * time.Second)
			r.closeAndReconnect()
			continue
		}
	}
}

func (r *Reader) closeAndReconnect() {
	if r.shuttingDown {
		return
	}
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
			Path:     "/loki/api/v1/tail",
			RawQuery: "query=" + url.QueryEscape(fmt.Sprintf("{%v=\"%v\",%v=\"%v\"}", r.sName, r.sValue, r.lName, r.lVal)),
		}

		fmt.Fprintf(r.w, "Connecting to loki at %v, querying for label '%v' with value '%v'\n", u.String(), r.lName, r.lVal)

		c, _, err := websocket.DefaultDialer.Dial(u.String(), r.header)
		if err != nil {
			fmt.Fprintf(r.w, "failed to connect to %s with err %s\n", u.String(), err)
			<-time.After(10 * time.Second)
			continue
		}
		// Use a custom ping handler so we can increment a metric, this is copied from the default ping handler
		c.SetPingHandler(func(message string) error {
			websocketPings.Inc()
			err := c.WriteControl(websocket.PongMessage, []byte(message), time.Now().Add(time.Second))
			if err == websocket.ErrCloseSent {
				return nil
			} else if e, ok := err.(net.Error); ok && e.Temporary() {
				return nil
			}
			return err
		})
		r.conn = c
	}
}

func parseResponse(entry *loghttp.Entry) (*time.Time, error) {
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

func nextBackoff(w io.Writer, statusCode int, backoff *util.Backoff) time.Time {
	// Be way more conservative with an http 429 and wait 5 minutes before trying again.
	var next time.Time
	if statusCode == http.StatusTooManyRequests {
		next = time.Now().Add(5 * time.Minute)
	} else {
		next = time.Now().Add(backoff.NextDelay())
	}
	fmt.Fprintf(w, "Loki returned an error code: %v, waiting %v before next query.", statusCode, next.Sub(time.Now()))
	return next
}
