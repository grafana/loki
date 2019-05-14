package main

import (
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/grafana/loki-canary/pkg/comparator"
	"github.com/grafana/loki-canary/pkg/reader"
	"github.com/grafana/loki-canary/pkg/writer"
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
	flag.Parse()

	if *addr == "" {
		_, _ = fmt.Fprintf(os.Stderr, "Must specify a Loki address with -addr\n")
		os.Exit(1)
	}

	var ui *url.Userinfo
	if *user != "" {
		ui = url.UserPassword(*user, *pass)
	}

	scheme := "ws"
	if *tls {
		scheme = "wss"
	}

	u := url.URL{
		Scheme:   scheme,
		Host:     *addr,
		User:     ui,
		Path:     "/api/prom/tail",
		RawQuery: "query=" + url.QueryEscape(fmt.Sprintf("{stream=\"stdout\",%v=\"%v\"}", *lName, *lVal)),
	}

	_, _ = fmt.Fprintf(os.Stderr, "Connecting to loki at %v, querying for label '%v' with value '%v'\n", u.String(), *lName, *lVal)

	c := comparator.NewComparator(os.Stderr, *wait, 1*time.Second)
	w := writer.NewWriter(os.Stdout, c, *interval, *size)
	r := reader.NewReader(os.Stderr, c, u, "", "")

	http.Handle("/metrics", promhttp.Handler())
	go func() {
		err := http.ListenAndServe(":"+strconv.Itoa(*port), nil)
		if err != nil {
			panic(err)
		}
	}()

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	for {
		select {
		case <-interrupt:
			_, _ = fmt.Fprintf(os.Stderr, "shutting down\n")
			w.Stop()
			r.Stop()
			c.Stop()
			return
		}
	}

}
