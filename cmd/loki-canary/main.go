package main

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/grafana/loki-canary/pkg/comparator"
	"github.com/grafana/loki-canary/pkg/reader"
	"github.com/grafana/loki-canary/pkg/writer"
)

func main() {

	c := comparator.NewComparator(os.Stderr, 1*time.Minute, 1*time.Second)

	w := writer.NewWriter(os.Stdout, c, 10*time.Millisecond, 1024)

	u := url.URL{
		Scheme:   "ws",
		Host:     "loki:3100",
		Path:     "/api/prom/tail",
		RawQuery: "query=" + url.QueryEscape("{name=\"loki-canary\",stream=\"stdout\"}"),
	}

	r := reader.NewReader(os.Stderr, c, u, "", "")

	http.Handle("/metrics", promhttp.Handler())
	go func() {
		err := http.ListenAndServe(":2112", nil)
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
