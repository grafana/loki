package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/version"

	"github.com/grafana/loki/v2/pkg/canary/comparator"
	"github.com/grafana/loki/v2/pkg/canary/reader"
	"github.com/grafana/loki/v2/pkg/canary/writer"
	_ "github.com/grafana/loki/v2/pkg/util/build"
)

type canary struct {
	lock sync.Mutex

	writer     *writer.Writer
	reader     *reader.Reader
	comparator *comparator.Comparator
}

func main() {

	lName := flag.String("labelname", "name", "The label name for this instance of loki-canary to use in the log selector")
	lVal := flag.String("labelvalue", "loki-canary", "The unique label value for this instance of loki-canary to use in the log selector")
	sName := flag.String("streamname", "stream", "The stream name for this instance of loki-canary to use in the log selector")
	sValue := flag.String("streamvalue", "stdout", "The unique stream value for this instance of loki-canary to use in the log selector")
	port := flag.Int("port", 3500, "Port which loki-canary should expose metrics")
	addr := flag.String("addr", "", "The Loki server URL:Port, e.g. loki:3100")
	tls := flag.Bool("tls", false, "Does the loki connection use TLS?")
	user := flag.String("user", "", "Loki username. Must not be set with tenant-id flag.")
	pass := flag.String("pass", "", "Loki password. Must not be set with tenant-id flag.")
	tenantID := flag.String("tenant-id", "", "Tenant id to be set in X-Scope-OrgID header. Must not be set with user/pass flags.")
	queryTimeout := flag.Duration("query-timeout", 10*time.Second, "How long to wait for a query response from Loki")

	interval := flag.Duration("interval", 1000*time.Millisecond, "Duration between log entries")
	outOfOrderPercentage := flag.Int("out-of-order-percentage", 0, "Percentage (0-100) of log entries that should be sent out of order.")
	outOfOrderMin := flag.Duration("out-of-order-min", 30*time.Second, "Minimum amount of time to go back for out of order entries (in seconds).")
	outOfOrderMax := flag.Duration("out-of-order-max", 60*time.Second, "Maximum amount of time to go back for out of order entries (in seconds).")

	size := flag.Int("size", 100, "Size in bytes of each log line")
	wait := flag.Duration("wait", 60*time.Second, "Duration to wait for log entries on websocket before querying loki for them")
	maxWait := flag.Duration("max-wait", 5*time.Minute, "Duration to keep querying Loki for missing websocket entries before reporting them missing")
	pruneInterval := flag.Duration("pruneinterval", 60*time.Second, "Frequency to check sent vs received logs, "+
		"also the frequency which queries for missing logs will be dispatched to loki")
	buckets := flag.Int("buckets", 10, "Number of buckets in the response_latency histogram")

	metricTestInterval := flag.Duration("metric-test-interval", 1*time.Hour, "The interval the metric test query should be run")
	metricTestQueryRange := flag.Duration("metric-test-range", 24*time.Hour, "The range value [24h] used in the metric test instant-query."+
		" Note: this value is truncated to the running time of the canary until this value is reached")

	spotCheckInterval := flag.Duration("spot-check-interval", 15*time.Minute, "Interval that a single result will be kept from sent entries and spot-checked against Loki, "+
		"e.g. 15min default one entry every 15 min will be saved and then queried again every 15min until spot-check-max is reached")
	spotCheckMax := flag.Duration("spot-check-max", 4*time.Hour, "How far back to check a spot check entry before dropping it")
	spotCheckQueryRate := flag.Duration("spot-check-query-rate", 1*time.Minute, "Interval that the canary will query Loki for the current list of all spot check entries")
	spotCheckWait := flag.Duration("spot-check-initial-wait", 10*time.Second, "How long should the spot check query wait before starting to check for entries")

	printVersion := flag.Bool("version", false, "Print this builds version information")

	flag.Parse()

	if *printVersion {
		fmt.Println(version.Print("loki-canary"))
		os.Exit(0)
	}

	if *addr == "" {
		_, _ = fmt.Fprintf(os.Stderr, "Must specify a Loki address with -addr\n")
		os.Exit(1)
	}

	if *outOfOrderPercentage < 0 || *outOfOrderPercentage > 100 {
		_, _ = fmt.Fprintf(os.Stderr, "Out of order percentage must be between 0 and 100\n")
		os.Exit(1)
	}

	sentChan := make(chan time.Time)
	receivedChan := make(chan time.Time)

	c := &canary{}
	startCanary := func() {
		c.stop()

		c.lock.Lock()
		defer c.lock.Unlock()

		c.writer = writer.NewWriter(os.Stdout, sentChan, *interval, *outOfOrderMin, *outOfOrderMax, *outOfOrderPercentage, *size)
		c.reader = reader.NewReader(os.Stderr, receivedChan, *tls, *addr, *user, *pass, *tenantID, *queryTimeout, *lName, *lVal, *sName, *sValue, *interval)
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
