package loghttp

import (
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/loki/pkg/logproto"
)

const (
	defaultQueryLimit = 100
	defaultSince      = 1 * time.Hour
	defaultStep       = 1 // 1 seconds
)

func limit(r *http.Request) (uint32, error) {
	l, err := parseInt(r.URL.Query().Get("limit"), defaultQueryLimit)
	if err != nil {
		return 0, err
	}
	return uint32(l), nil
}

func query(r *http.Request) string {
	return r.URL.Query().Get("query")
}

func ts(r *http.Request) (time.Time, error) {
	return parseTimestamp(r.URL.Query().Get("time"), time.Now())
}

func direction(r *http.Request) (logproto.Direction, error) {
	return parseDirection(r.URL.Query().Get("direction"), logproto.BACKWARD)
}

func bounds(r *http.Request) (time.Time, time.Time, error) {
	now := time.Now()
	start, err := parseTimestamp(r.URL.Query().Get("start"), now.Add(-defaultSince))
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	end, err := parseTimestamp(r.URL.Query().Get("end"), now)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	return start, end, nil
}

func step(r *http.Request, start, end time.Time) (time.Duration, error) {
	s, err := parseInt(r.URL.Query().Get("step"), defaultQueryRangeStep(start, end))
	if err != nil {
		return 0, err
	}
	return time.Duration(s) * time.Second, nil
}

// defaultQueryRangeStep returns the default step used in the query range API,
// which is dinamically calculated based on the time range
func defaultQueryRangeStep(start time.Time, end time.Time) int {
	return int(math.Max(math.Floor(end.Sub(start).Seconds()/250), 1))
}

func tailDelay(r *http.Request) (uint32, error) {
	l, err := parseInt(r.URL.Query().Get("delay_for"), 0)
	if err != nil {
		return 0, err
	}
	return uint32(l), nil
}

// parseInt parses an int from a string
// if the value is empty it returns a default value passed as second parameter
func parseInt(value string, def int) (int, error) {
	if value == "" {
		return def, nil
	}
	return strconv.Atoi(value)
}

// parseUnixNano parses a ns unix timestamp from a string
// if the value is empty it returns a default value passed as second parameter
func parseTimestamp(value string, def time.Time) (time.Time, error) {
	if value == "" {
		return def, nil
	}

	if strings.Contains(value, ".") {
		if t, err := strconv.ParseFloat(value, 64); err == nil {
			s, ns := math.Modf(t)
			ns = math.Round(ns*1000) / 1000
			return time.Unix(int64(s), int64(ns*float64(time.Second))), nil
		}
	}
	nanos, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		if ts, err := time.Parse(time.RFC3339Nano, value); err == nil {
			return ts, nil
		}
		return time.Time{}, err
	}
	if len(value) <= 10 {
		return time.Unix(nanos, 0), nil
	}
	return time.Unix(0, nanos), nil
}

// parseDirection parses a logproto.Direction from a string
// if the value is empty it returns a default value passed as second parameter
func parseDirection(value string, def logproto.Direction) (logproto.Direction, error) {
	if value == "" {
		return def, nil
	}

	d, ok := logproto.Direction_value[strings.ToUpper(value)]
	if !ok {
		return logproto.FORWARD, fmt.Errorf("invalid direction '%s'", value)
	}
	return logproto.Direction(d), nil
}
