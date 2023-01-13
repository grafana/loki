package main

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"sync"
	"syscall"
	"time"

	"crypto/tls"
	"net/http"
	"os/signal"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/backoff"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/config"
	"github.com/prometheus/common/version"
	"gopkg.in/alecthomas/kingpin.v2"

	"github.com/grafana/loki/pkg/canary/comparator"
	"github.com/grafana/loki/pkg/canary/reader"
	"github.com/grafana/loki/pkg/canary/writer"
	_ "github.com/grafana/loki/pkg/util/build"
)

type canary struct {
	lock sync.Mutex

	writer     *writer.Writer
	reader     *reader.Reader
	comparator *comparator.Comparator
}

func main() {

	lName := kingpin.Flag("labelname", "The label name for this instance of loki-canary to use in the log selector").Default("name").String()
	lVal := kingpin.Flag("labelvalue", "The unique label value for this instance of loki-canary to use in the log selector").Default("loki-canary").String()
	sName := kingpin.Flag("streamname", "The stream name for this instance of loki-canary to use in the log selector").Default("stream").String()
	sValue := kingpin.Flag("streamvalue", "The unique stream value for this instance of loki-canary to use in the log selector").Default("stdout").String()
	port := kingpin.Flag("port", "Port which loki-canary should expose metrics").Default("3500").Int()
	addr := kingpin.Flag("addr", "The Loki server URL:Port, e.g. loki:3100").Default("").Envar("LOKI_ADDRESS").String()
	push := kingpin.Flag("push", "Push the logs directly to given Loki address").Default("false").Bool()
	useTLS := kingpin.Flag("tls", "Does the loki connection use TLS?").Default("false").Bool()
	certFile := kingpin.Flag("cert-file", "Client PEM encoded X.509 certificate for optional use with TLS connection to Loki").Default("").String()
	keyFile := kingpin.Flag("key-file", "Client PEM encoded X.509 key for optional use with TLS connection to Loki").Default("").String()
	caFile := kingpin.Flag("ca-file", "Client certificate authority for optional use with TLS connection to Loki").Default("").String()
	insecureSkipVerify := kingpin.Flag("insecure", "Allow insecure TLS connections").Default("false").Bool()
	user := kingpin.Flag("user", "Loki username.").Default("").Envar("LOKI_USERNAME").String()
	pass := kingpin.Flag("pass", "Loki password. This credential should have both read and write permissions to Loki endpoints").Default("").Envar("LOKI_PASSWORD").String()
	tenantID := kingpin.Flag("tenant-id", "Tenant ID to be set in X-Scope-OrgID header.").Default("").String()
	writeTimeout := kingpin.Flag("write-timeout", "How long to wait write response from Loki").Default("10s").Duration()
	writeMinBackoff := kingpin.Flag("write-min-backoff", "Initial backoff time before first retry ").Default("500ms").Duration()
	writeMaxBackoff := kingpin.Flag("write-max-backoff", "Maximum backoff time between retries ").Default("5m").Duration()
	writeMaxRetries := kingpin.Flag("write-max-retries", "Maximum number of retries when push a log entry ").Default("10").Int()
	queryTimeout := kingpin.Flag("query-timeout", "How long to wait for a query response from Loki").Default("10s").Duration()

	interval := kingpin.Flag("interval", "Duration between log entries").Default("1s").Duration()
	outOfOrderPercentage := kingpin.Flag("out-of-order-percentage", "Percentage (0-100) of log entries that should be sent out of order.").Default("0").Int()
	outOfOrderMin := kingpin.Flag("out-of-order-min", "Minimum amount of time to go back for out of order entries (in seconds).").Default("30s").Duration()
	outOfOrderMax := kingpin.Flag("out-of-order-max", "Maximum amount of time to go back for out of order entries (in seconds).").Default("60s").Duration()

	size := kingpin.Flag("size", "Size in bytes of each log line").Default("100").Int()
	wait := kingpin.Flag("wait", "Duration to wait for log entries on websocket before querying loki for them").Default("60s").Duration()
	maxWait := kingpin.Flag("max-wait", "Duration to keep querying Loki for missing websocket entries before reporting them missing").Default("5m").Duration()
	pruneInterval := kingpin.Flag("pruneinterval", "Frequency to check sent vs received logs, "+
		"also the frequency which queries for missing logs will be dispatched to loki").Default("60s").Duration()
	buckets := kingpin.Flag("buckets", "Number of buckets in the response_latency histogram").Default("10").Int()

	metricTestInterval := kingpin.Flag("metric-test-interval", "The interval the metric test query should be run").Default("1h").Duration()
	metricTestQueryRange := kingpin.Flag("metric-test-range", "The range value [24h] used in the metric test instant-query."+
		" Note: this value is truncated to the running time of the canary until this value is reached").Default("24h").Duration()

	spotCheckInterval := kingpin.Flag("spot-check-interval", "Interval that a single result will be kept from sent entries and spot-checked against Loki, "+
		"e.g. 15min default one entry every 15 min will be saved and then queried again every 15min until spot-check-max is reached").Default("15m").Duration()
	spotCheckMax := kingpin.Flag("spot-check-max", "How far back to check a spot check entry before dropping it").Default("4h").Duration()
	spotCheckQueryRate := kingpin.Flag("spot-check-query-rate", "Interval that the canary will query Loki for the current list of all spot check entries").Default("1m").Duration()
	spotCheckWait := kingpin.Flag("spot-check-initial-wait", "How long should the spot check query wait before starting to check for entries").Default("10s").Duration()

	printVersion := kingpin.Flag("version", "Print this builds version information").Default("false").Bool()

	kingpin.Parse()

	if *printVersion {
		fmt.Println(version.Print("loki-canary"))
		os.Exit(0)
	}

	if *addr == "" {
		_, _ = fmt.Fprintf(os.Stderr, "Must specify a Loki address with -addr or set the environemnt variable LOKI_ADDRESS\n")
		os.Exit(1)
	}

	if *outOfOrderPercentage < 0 || *outOfOrderPercentage > 100 {
		_, _ = fmt.Fprintf(os.Stderr, "Out of order percentage must be between 0 and 100\n")
		os.Exit(1)
	}

	var tlsConfig *tls.Config
	tc := config.TLSConfig{}
	if *certFile != "" || *keyFile != "" || *caFile != "" {
		if !*useTLS {
			_, _ = fmt.Fprintf(os.Stderr, "Must set --tls when specifying client certs\n")
			os.Exit(1)
		}
		tc.CAFile = *caFile
		tc.CertFile = *certFile
		tc.KeyFile = *keyFile
		tc.InsecureSkipVerify = *insecureSkipVerify

		var err error
		tlsConfig, err = config.NewTLSConfig(&tc)
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "TLS configuration error: %s\n", err.Error())
			os.Exit(1)
		}
	}

	sentChan := make(chan time.Time)
	receivedChan := make(chan time.Time)

	logger := log.NewLogfmtLogger(log.NewSyncWriter(os.Stderr))
	logger = log.With(logger, "caller", log.Caller(3))

	c := &canary{}
	startCanary := func() {
		c.stop()

		c.lock.Lock()
		defer c.lock.Unlock()

		var (
			err error
			w   io.Writer
		)

		w = os.Stdout

		if *push {
			backoffCfg := backoff.Config{
				MinBackoff: *writeMinBackoff,
				MaxBackoff: *writeMaxBackoff,
				MaxRetries: *writeMaxRetries,
			}
			push, err := writer.NewPush(
				*addr,
				*tenantID,
				*writeTimeout,
				config.DefaultHTTPClientConfig,
				*lName, *lVal,
				*sName, *sValue,
				*useTLS,
				tlsConfig,
				*caFile, *certFile, *keyFile,
				*user, *pass,
				&backoffCfg,
				log.NewLogfmtLogger(os.Stdout),
			)
			if err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "Unable to create writer for Loki, check config: %s", err)
				os.Exit(1)
			}

			w = push
		}

		c.writer = writer.NewWriter(w, sentChan, *interval, *outOfOrderMin, *outOfOrderMax, *outOfOrderPercentage, *size, logger)
		c.reader, err = reader.NewReader(os.Stderr, receivedChan, *useTLS, tlsConfig, *caFile, *certFile, *keyFile, *addr, *user, *pass, *tenantID, *queryTimeout, *lName, *lVal, *sName, *sValue, *interval)
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Unable to create reader for Loki querier, check config: %s", err)
			os.Exit(1)
		}
		c.comparator = comparator.NewComparator(os.Stderr, *wait, *maxWait, *pruneInterval, *spotCheckInterval, *spotCheckMax, *spotCheckQueryRate, *spotCheckWait, *metricTestInterval, *metricTestQueryRange, *interval, *buckets, sentChan, receivedChan, c.reader, true)
	}

	startCanary()

	http.HandleFunc("/resume", func(_ http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(os.Stderr, "restarting\n")
		startCanary()
	})
	http.HandleFunc("/suspend", func(_ http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(os.Stderr, "suspending\n")
		c.stop()
	})
	http.Handle("/metrics", promhttp.Handler())
	go func() {
		err := http.ListenAndServe(":"+strconv.Itoa(*port), nil)
		if err != nil {
			panic(err)
		}
	}()

	terminate := make(chan os.Signal, 1)
	signal.Notify(terminate, syscall.SIGTERM, os.Interrupt)

	for range terminate {
		_, _ = fmt.Fprintf(os.Stderr, "shutting down\n")
		c.stop()
		return
	}
}

func (c *canary) stop() {
	c.lock.Lock()
	defer c.lock.Unlock()

	if c.writer == nil || c.reader == nil || c.comparator == nil {
		return
	}

	c.writer.Stop()
	c.reader.Stop()
	c.comparator.Stop()

	c.writer = nil
	c.reader = nil
	c.comparator = nil
}
