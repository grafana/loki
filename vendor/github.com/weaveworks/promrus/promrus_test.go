package promrus_test

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"

	"github.com/weaveworks/promrus"
)

const (
	addr     string = ":8080"
	endpoint string = "/metrics"
)

func TestRegisteringHookMultipleTimesShouldBeSafe(t *testing.T) {
	log.AddHook(promrus.MustNewPrometheusHook())
	log.AddHook(promrus.MustNewPrometheusHook()) // Initialising twice in a row should NOT raise any error.

	server := httpServePrometheusMetrics(t)

	lines := httpGetMetrics(t)
	assert.Equal(t, 0, countFor(t, log.InfoLevel, lines))

	log.Info("this is at info level!")

	lines = httpGetMetrics(t)
	assert.Equal(t, 1, countFor(t, log.InfoLevel, lines))

	log.AddHook(promrus.MustNewPrometheusHook()) // Initialising again here should NOT raise any error, but will reset counters.

	lines = httpGetMetrics(t)
	assert.Equal(t, 0, countFor(t, log.InfoLevel, lines))

	server.Close()
}

func TestExposeAndQueryLogrusCounters(t *testing.T) {
	// Create Prometheus hook and configure logrus to use it:
	hook := promrus.MustNewPrometheusHook()
	log.AddHook(hook)
	log.SetLevel(log.DebugLevel)

	server := httpServePrometheusMetrics(t)

	lines := httpGetMetrics(t)
	assert.Equal(t, 0, countFor(t, log.DebugLevel, lines))
	assert.Equal(t, 0, countFor(t, log.InfoLevel, lines))
	assert.Equal(t, 0, countFor(t, log.WarnLevel, lines))
	assert.Equal(t, 0, countFor(t, log.ErrorLevel, lines))

	log.Debug("this is at debug level!")
	lines = httpGetMetrics(t)
	assert.Equal(t, 1, countFor(t, log.DebugLevel, lines))
	assert.Equal(t, 0, countFor(t, log.InfoLevel, lines))
	assert.Equal(t, 0, countFor(t, log.WarnLevel, lines))
	assert.Equal(t, 0, countFor(t, log.ErrorLevel, lines))

	log.Info("this is at info level!")
	lines = httpGetMetrics(t)
	assert.Equal(t, 1, countFor(t, log.DebugLevel, lines))
	assert.Equal(t, 1, countFor(t, log.InfoLevel, lines))
	assert.Equal(t, 0, countFor(t, log.WarnLevel, lines))
	assert.Equal(t, 0, countFor(t, log.ErrorLevel, lines))

	log.Warn("this is at warning level!")
	lines = httpGetMetrics(t)
	assert.Equal(t, 1, countFor(t, log.DebugLevel, lines))
	assert.Equal(t, 1, countFor(t, log.InfoLevel, lines))
	assert.Equal(t, 1, countFor(t, log.WarnLevel, lines))
	assert.Equal(t, 0, countFor(t, log.ErrorLevel, lines))

	log.Error("this is at error level!")
	lines = httpGetMetrics(t)
	assert.Equal(t, 1, countFor(t, log.DebugLevel, lines))
	assert.Equal(t, 1, countFor(t, log.InfoLevel, lines))
	assert.Equal(t, 1, countFor(t, log.WarnLevel, lines))
	assert.Equal(t, 1, countFor(t, log.ErrorLevel, lines))

	server.Close()
}

// httpServePrometheusMetrics exposes the Prometheus metrics over HTTP, in a different go routine.
func httpServePrometheusMetrics(t *testing.T) *http.Server {
	server := &http.Server{
		Addr:    addr,
		Handler: promhttp.Handler(),
	}
	go server.ListenAndServe()
	return server
}

// httpGetMetrics queries the local HTTP server for the exposed metrics and parses the response.
func httpGetMetrics(t *testing.T) []string {
	resp, err := http.Get(fmt.Sprintf("http://localhost%v%v", addr, endpoint))
	assert.Nil(t, err)
	body, err := ioutil.ReadAll(resp.Body)
	assert.Nil(t, err)
	lines := strings.Split(string(body), "\n")
	assert.True(t, len(lines) > 0)
	return lines
}

// countFor is a helper function to get the counter's value for the provided level.
func countFor(t *testing.T, level log.Level, lines []string) int {
	// Metrics are exposed as per the below example:
	//   # HELP test_debug Number of log statements at debug level.
	//   # TYPE test_debug counter
	//   test_debug 0
	metric := fmt.Sprintf("log_messages{level=\"%v\"}", level)
	for _, line := range lines {
		items := strings.Split(line, " ")
		if len(items) != 2 { // e.g. {"test_debug", "0"}
			continue
		}
		if items[0] == metric {
			count, err := strconv.ParseInt(items[1], 10, 32)
			assert.Nil(t, err)
			return int(count)
		}
	}
	panic(fmt.Sprintf("Could not find %v in %v", metric, lines))
}
