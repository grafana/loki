package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/version"

	"github.com/grafana/loki/pkg/canary/comparator"
	"github.com/grafana/loki/pkg/canary/reader"
	"github.com/grafana/loki/pkg/canary/writer"
)

func main() {

	lName := flag.String("labelname", "name", "The label name for this instance of loki-canary to use in the log selector")
	lVal := flag.String("labelvalue", "loki-canary", "The unique label value for this instance of loki-canary to use in the log selector")
	port := flag.Int("port", 3500, "Port which loki-canary should expose metrics")
	addr := flag.String("addr", "", "The Loki server URL:Port, e.g. loki:3100")
	tls := flag.Bool("tls", false, "Does the loki connection use TLS?")
	user := flag.String("user", "", "Loki username")
	pass := flag.String("pass", "", "Loki password")

	interval := flag.Duration("interval", 1000*time.Millisecond, "Duration between log entries")
	size := flag.Int("size", 100, "Size in bytes of each log line")
	wait := flag.Duration("wait", 60*time.Second, "Duration to wait for log entries before reporting them lost")
	pruneInterval := flag.Duration("pruneinterval", 60*time.Second, "Frequency to check sent vs received logs, also the frequency which queries for missing logs will be dispatched to loki")
	buckets := flag.Int("buckets", 10, "Number of buckets in the response_latency histogram")

	printVersion := flag.Bool("version", false, "Print this builds version information")

	flag.Parse()

	if *printVersion {
		fmt.Print(version.Print("loki-canary"))
		os.Exit(0)
	}

	if *addr == "" {
		_, _ = fmt.Fprintf(os.Stderr, "Must specify a Loki address with -addr\n")
		os.Exit(1)
	}

	sentChan := make(chan time.Time)
	receivedChan := make(chan time.Time)

	w := writer.NewWriter(os.Stdout, sentChan, *interval, *size)
	r := reader.NewReader(os.Stderr, receivedChan, *tls, *addr, *user, *pass, *lName, *lVal)
	c := comparator.NewComparator(os.Stderr, *wait, *pruneInterval, *buckets, sentChan, receivedChan, r, true)

	http.Handle("/metrics", promhttp.Handler())
	go func() {
		err := http.ListenAndServe(":"+strconv.Itoa(*port), nil)
		if err != nil {
			panic(err)
		}
	}()

	interrupt := make(chan os.Signal, 1)
	terminate := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)
	signal.Notify(terminate, syscall.SIGTERM)

	for {
		select {
		case <-interrupt:
			_, _ = fmt.Fprintf(os.Stderr, "suspending indefinitely\n")
			w.Stop()
			r.Stop()
			c.Stop()
		case <-terminate:
			_, _ = fmt.Fprintf(os.Stderr, "shutting down\n")
			w.Stop()
			r.Stop()
			c.Stop()
			return
		}
	}

}
