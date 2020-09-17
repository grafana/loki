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

	"github.com/kelseyhightower/envconfig"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/version"

	_ "github.com/grafana/loki/pkg/build"
	"github.com/grafana/loki/pkg/canary/comparator"
	"github.com/grafana/loki/pkg/canary/reader"
	"github.com/grafana/loki/pkg/canary/writer"
)

type canary struct {
	lock sync.Mutex

	writer     *writer.Writer
	reader     *reader.Reader
	comparator *comparator.Comparator
}

type Config struct {
	LabelName   string `envconfig:"label_name"`
	LabelValue  string `envconfig:"label_value"`
	StreamName  string `envconfig:"stream_name"`
	StreamValue string `envconfig:"stream_value"`

	Port         int           `envconfig:"port"`
	Addr         string        `envconfig:"addr"`
	Tls          bool          `envconfig:"tls"`
	User         string        `envconfig:"user"`
	Pass         string        `envconfig:"pass"`
	QueryTimeout time.Duration `envconfig:"query_timeout"`

	Interval      time.Duration `envconfig:"interval"`
	Size          int           `envconfig:"size"`
	Wait          time.Duration `envconfig:"wait"`
	MaxWait       time.Duration `envconfig:"max_wait"`
	PruneInterval time.Duration `envconfig:"prune_interval"`

	Buckets              int           `envconfig:"buckets"`
	MetricTestInterval   time.Duration `envconfig:"metric_test_interval"`
	MetricTestQueryRange time.Duration `envconfig:"metric_test_query_range"`

	SpotCheckInterval  time.Duration `envconfig:"spot_check_interval"`
	SpotCheckMax       time.Duration `envconfig:"spot_check_max"`
	SpotCheckQueryRate time.Duration `envconfig:"spot_check_query_rate"`
	SpotCheckWait      time.Duration `envconfig:"spot_check_wait"`

	PrintVersion bool `ignored:"true"`
}

func main() {

	var cfg Config
	err := envconfig.Process("loki_canary", &cfg)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "Failed to parse config from environment variables: ", err.Error())
		os.Exit(1)
	}

	flag.StringVar(&cfg.LabelName, "labelname", "name", "The label name for this instance of loki-canary to use in the log selector")
	flag.StringVar(&cfg.LabelValue, "labelvalue", "loki-canary", "The unique label value for this instance of loki-canary to use in the log selector")
	flag.StringVar(&cfg.StreamName, "streamname", "stream", "The stream name for this instance of loki-canary to use in the log selector")
	flag.StringVar(&cfg.StreamValue, "streamvalue", "stdout", "The unique stream value for this instance of loki-canary to use in the log selector")
	flag.IntVar(&cfg.Port, "port", 3500, "Port which loki-canary should expose metrics")
	flag.StringVar(&cfg.Addr, "addr", "", "The Loki server URL:Port, e.g. loki:3100")
	flag.BoolVar(&cfg.Tls, "tls", false, "Does the loki connection use TLS?")
	flag.StringVar(&cfg.User, "user", "", "Loki username")
	flag.StringVar(&cfg.Pass, "pass", "", "Loki password")
	flag.DurationVar(&cfg.QueryTimeout, "query-timeout", 10*time.Second, "How long to wait for a query response from Loki")

	flag.DurationVar(&cfg.Interval, "interval", 1000*time.Millisecond, "Duration between log entries")
	flag.IntVar(&cfg.Size, "Size", 100, "Size in bytes of each log line")
	flag.DurationVar(&cfg.Wait, "wait", 60*time.Second, "Duration to wait for log entries on websocket before querying loki for them")
	flag.DurationVar(&cfg.MaxWait, "max-wait", 5*time.Minute, "Duration to keep querying Loki for missing websocket entries before reporting them missing")
	flag.DurationVar(&cfg.PruneInterval, "pruneinterval", 60*time.Second, "Frequency to check sent vs received logs, "+
		"also the frequency which queries for missing logs will be dispatched to loki")
	flag.IntVar(&cfg.Buckets, "buckets", 10, "Number of buckets in the response_latency histogram")

	flag.DurationVar(&cfg.MetricTestInterval, "metric-test-interval", 1*time.Hour, "The interval the metric test query should be run")
	flag.DurationVar(&cfg.MetricTestQueryRange, "metric-test-range", 24*time.Hour, "The range value [24h] used in the metric test instant-query."+
		" Note: this value is truncated to the running time of the canary until this value is reached")

	flag.DurationVar(&cfg.SpotCheckInterval, "spot-check-interval", 15*time.Minute, "Interval that a single result will be kept from sent entries and spot-checked against Loki, "+
		"e.g. 15min default one entry every 15 min will be saved and then queried again every 15min until spot-check-max is reached")
	flag.DurationVar(&cfg.SpotCheckMax, "spot-check-max", 4*time.Hour, "How far back to check a spot check entry before dropping it")
	flag.DurationVar(&cfg.SpotCheckQueryRate, "spot-check-query-rate", 1*time.Minute, "Interval that the canary will query Loki for the current list of all spot check entries")
	flag.DurationVar(&cfg.SpotCheckWait, "spot-check-initial-wait", 10*time.Second, "How long should the spot check query wait before starting to check for entries")

	flag.BoolVar(&cfg.PrintVersion, "version", false, "Print this builds version information")

	flag.Parse()

	if cfg.PrintVersion {
		fmt.Println(version.Print("loki-canary"))
		os.Exit(0)
	}

	if cfg.Addr == "" {
		_, _ = fmt.Fprintf(os.Stderr, "Must specify a Loki address with -addr\n")
		os.Exit(1)
	}

	sentChan := make(chan time.Time)
	receivedChan := make(chan time.Time)

	c := &canary{}
	startCanary := func() {
		c.stop()

		c.lock.Lock()
		defer c.lock.Unlock()

		c.writer = writer.NewWriter(os.Stdout, sentChan, cfg.Interval, cfg.Size)
		c.reader = reader.NewReader(
			os.Stderr,
			receivedChan,
			cfg.Tls,
			cfg.Addr,
			cfg.User,
			cfg.Pass,
			cfg.QueryTimeout,
			cfg.LabelName,
			cfg.LabelValue,
			cfg.StreamName,
			cfg.StreamValue,
			cfg.Interval)
		c.comparator = comparator.NewComparator(
			os.Stderr,
			cfg.Wait,
			cfg.MaxWait,
			cfg.PruneInterval,
			cfg.SpotCheckInterval,
			cfg.SpotCheckMax,
			cfg.SpotCheckQueryRate,
			cfg.SpotCheckWait,
			cfg.MetricTestInterval,
			cfg.MetricTestQueryRange,
			cfg.Interval,
			cfg.Buckets,
			sentChan,
			receivedChan,
			c.reader,
			true)
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
		err := http.ListenAndServe(":"+strconv.Itoa(cfg.Port), nil)
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
