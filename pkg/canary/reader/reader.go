package reader

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/grafana/dskit/backoff"
	json "github.com/json-iterator/go"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/config"

	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/util/build"
	"github.com/grafana/loki/v3/pkg/util/unmarshal"
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
	QueryCountOverTime(queryRange string, now time.Time, cache bool) (float64, error)
}

type Reader struct {
	header          http.Header
	useTLS          bool
	clientTLSConfig *tls.Config
	caFile          string
	addr            string
	user            string
	pass            string
	tenantID        string
	httpClient      *http.Client
	queryTimeout    time.Duration
	sName           string
	sValue          string
	lName           string
	lVal            string
	backoff         *backoff.Backoff
	nextQuery       time.Time
	backoffMtx      sync.RWMutex
	interval        time.Duration
	conn            *websocket.Conn
	w               io.Writer
	recv            chan time.Time
	quit            chan struct{}
	shuttingDown    bool
	done            chan struct{}
	queryAppend     string
}

func NewReader(writer io.Writer,
	receivedChan chan time.Time,
	useTLS bool,
	tlsConfig *tls.Config,
	caFile, certFile, keyFile string,
	address string,
	user string,
	pass string,
	tenantID string,
	queryTimeout time.Duration,
	labelName string,
	labelVal string,
	streamName string,
	streamValue string,
	interval time.Duration,
	queryAppend string,
) (*Reader, error) {
	h := http.Header{}

	// http.DefaultClient will be used in the case that the connection to Loki is http or TLS without client certs.
	httpClient := http.DefaultClient
	if tlsConfig != nil && (certFile != "" || keyFile != "" || caFile != "") {
		// For the mTLS case, use a http.Client configured with the client side certificates.
		tlsSettings := config.TLSRoundTripperSettings{
			CA:   config.NewFileSecret(caFile),
			Cert: config.NewFileSecret(certFile),
			Key:  config.NewFileSecret(keyFile),
		}
		rt, err := config.NewTLSRoundTripper(tlsConfig, tlsSettings, func(tls *tls.Config) (http.RoundTripper, error) {
			return &http.Transport{TLSClientConfig: tls}, nil
		})
		if err != nil {
			return nil, errors.Wrapf(err, "Failed to create HTTPS transport with mTLS config")
		}
		httpClient = &http.Client{Transport: rt}
	} else if tlsConfig != nil {
		// non-mutual TLS
		httpClient = &http.Client{Transport: &http.Transport{TLSClientConfig: tlsConfig}}
	}
	if user != "" {
		h.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(user+":"+pass)))
	}
	if tenantID != "" {
		h.Set("X-Scope-OrgID", tenantID)
	}

	next := time.Now()
	bkcfg := backoff.Config{
		MinBackoff: 1 * time.Second,
		MaxBackoff: 10 * time.Minute,
		MaxRetries: 0,
	}
	bkoff := backoff.New(context.Background(), bkcfg)

	rd := Reader{
		header:          h,
		useTLS:          useTLS,
		clientTLSConfig: tlsConfig,
		caFile:          caFile,
		addr:            address,
		user:            user,
		pass:            pass,
		tenantID:        tenantID,
		queryTimeout:    queryTimeout,
		httpClient:      httpClient,
		sName:           streamName,
		sValue:          streamValue,
		lName:           labelName,
		lVal:            labelVal,
		nextQuery:       next,
		backoff:         bkoff,
		interval:        interval,
		w:               writer,
		recv:            receivedChan,
		quit:            make(chan struct{}),
		done:            make(chan struct{}),
		shuttingDown:    false,
		queryAppend:     queryAppend,
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

	return &rd, nil
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
func (r *Reader) QueryCountOverTime(queryRange string, now time.Time, cache bool) (float64, error) {
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
	if r.useTLS {
		scheme = "https"
	}
	u := url.URL{
		Scheme: scheme,
		Host:   r.addr,
		Path:   "/loki/api/v1/query",
		RawQuery: "query=" + url.QueryEscape(fmt.Sprintf("count_over_time({%v=\"%v\",%v=\"%v\"}[%s])", r.sName, r.sValue, r.lName, r.lVal, queryRange)) +
			fmt.Sprintf("&time=%d", now.UnixNano()) +
			"&limit=1000",
	}
	fmt.Fprintf(r.w, "Querying loki for metric count with query: %v, cache: %v\n", u.String(), cache)

	ctx, cancel := context.WithTimeout(context.Background(), r.queryTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return 0, err
	}

	if r.user != "" {
		req.SetBasicAuth(r.user, r.pass)
	}
	if r.tenantID != "" {
		req.Header.Set("X-Scope-OrgID", r.tenantID)
	}
	req.Header.Set("User-Agent", userAgent)
	if !cache {
		req.Header.Set("Cache-Control", "no-cache")
	}

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return 0, errors.Wrap(err, "query request failed")
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
		buf, _ := io.ReadAll(resp.Body)
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
		series := value.(loghttp.Vector)
		if len(series) > 1 {
			return 0, fmt.Errorf("expected only a single series result in the metric test query vector, instead received %v", len(series))
		}
		if len(series) == 0 {
			return 0, fmt.Errorf("expected to receive one sample in the result vector, received 0")
		}
		ret = float64(series[0].Value)
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
	if r.useTLS {
		scheme = "https"
	}
	u := url.URL{
		Scheme: scheme,
		Host:   r.addr,
		Path:   "/loki/api/v1/query_range",
		RawQuery: fmt.Sprintf("start=%d&end=%d", start.UnixNano(), end.UnixNano()) +
			"&query=" + url.QueryEscape(fmt.Sprintf("{%v=\"%v\",%v=\"%v\"} %v", r.sName, r.sValue, r.lName, r.lVal, r.queryAppend)) +
			"&limit=1000",
	}
	fmt.Fprintf(r.w, "Querying loki for logs with query: %v\n", u.String())

	ctx, cancel := context.WithTimeout(context.Background(), r.queryTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, err
	}

	if r.user != "" {
		req.SetBasicAuth(r.user, r.pass)
	}
	if r.tenantID != "" {
		req.Header.Set("X-Scope-OrgID", r.tenantID)
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "query_range request failed")
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
		buf, _ := io.ReadAll(resp.Body)
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
	case logqlmodel.ValueTypeStreams:
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

// run uses the established websocket connection to tail logs from Loki
func (r *Reader) run() {
	r.closeAndReconnect()

	tailResponse := &loghttp.TailResponse{}
	lastMessageTs := time.Now()

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
		timeoutInterval := 10 * r.interval
		_ = r.conn.SetReadDeadline(time.Now().Add(timeoutInterval))
		// I assume this is a blocking call that either reads from the websocket connection
		// or times out based on the above SetReadDeadline call.
		err := unmarshal.ReadTailResponseJSON(tailResponse, r.conn)
		if err != nil {
			var e *websocket.CloseError
			if errors.As(err, &e) && e.Text == "reached tail max duration limit" {
				fmt.Fprintf(r.w, "tail max duration limit exceeded, will retry immediately: %s\n", err)

				r.closeAndReconnect()
				continue
			}

			reason := "error reading websocket"
			if e, ok := err.(net.Error); ok && e.Timeout() {
				reason = fmt.Sprintf("timeout tailing new logs (timeout period: %.2fs)", timeoutInterval.Seconds())
			}

			fmt.Fprintf(r.w, "%s, will retry in 10 seconds: %s\n", reason, err)

			// Even though we sleep between connection retries, we found it's possible to DOS Loki if the connection
			// succeeds but some other error is returned, so also sleep here before retrying.
			<-time.After(10 * time.Second)
			r.closeAndReconnect()
			continue
		}
		// If there were streams update last message timestamp
		if len(tailResponse.Streams) > 0 {
			lastMessageTs = time.Now()
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
		if time.Since(lastMessageTs).Milliseconds() > 10*r.interval.Milliseconds() {
			fmt.Fprintf(r.w, "Have not received a canary message from loki on the websocket in %vms, "+
				"sleeping 10s and reconnecting\n", 10*r.interval.Milliseconds())
			<-time.After(10 * time.Second)
			r.closeAndReconnect()
			continue
		}
	}
}

// closeAndReconnect establishes a web socket connection to Loki and sets up a tail query
// with the stream and label selectors which can be used check if all logs generated by the
// canary have been received by Loki.
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
		if r.useTLS {
			scheme = "wss"
		}
		u := url.URL{
			Scheme:   scheme,
			Host:     r.addr,
			Path:     "/loki/api/v1/tail",
			RawQuery: "query=" + url.QueryEscape(fmt.Sprintf("{%v=\"%v\",%v=\"%v\"} %v", r.sName, r.sValue, r.lName, r.lVal, r.queryAppend)),
		}

		fmt.Fprintf(r.w, "Connecting to loki at %v, querying for label '%v' with value '%v'\n", u.String(), r.lName, r.lVal)

		dialer := r.webSocketDialer()
		c, _, err := dialer.Dial(u.String(), r.header)
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
			} else if _, ok := err.(net.Error); ok {
				return nil
			}
			return err
		})
		r.conn = c
	}
}

// webSocketDialer creates a dialer for the web socket connection to Loki
// websocket.DefaultDialer will be returned in the case that the connection to Loki is http or TLS without client certs.
// For the mTLS case, return a websocket.Dialer configured to use client side certificates.
func (r *Reader) webSocketDialer() *websocket.Dialer {
	return &websocket.Dialer{
		Proxy:            http.ProxyFromEnvironment,
		TLSClientConfig:  r.clientTLSConfig,
		HandshakeTimeout: 45 * time.Second,
	}
}

func parseResponse(entry *loghttp.Entry) (*time.Time, error) {
	sp := strings.Split(entry.Line, " ")
	if len(sp) != 2 {
		return nil, errors.Errorf("received invalid entry: %s", entry.Line)
	}
	ts, err := strconv.ParseInt(sp[0], 10, 64)
	if err != nil {
		return nil, errors.Errorf("failed to parse timestamp: %s", sp[0])
	}
	t := time.Unix(0, ts)
	return &t, nil
}

func nextBackoff(w io.Writer, statusCode int, backoff *backoff.Backoff) time.Time {
	// Be way more conservative with an http 429 and wait 5 minutes before trying again.
	var next time.Time
	if statusCode == http.StatusTooManyRequests {
		next = time.Now().Add(5 * time.Minute)
	} else {
		next = time.Now().Add(backoff.NextDelay())
	}
	fmt.Fprintf(w, "Loki returned an error code: %v, waiting %v before next query.", statusCode, time.Until(next))
	return next
}
