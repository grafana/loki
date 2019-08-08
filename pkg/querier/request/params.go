package request

import (
	"net/http"
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
	return parseUnixNano(r.URL.Query().Get("time"), time.Now())
}

func direction(r *http.Request) (logproto.Direction, error) {
	return parseDirection(r.URL.Query().Get("direction"), logproto.BACKWARD)
}

func bounds(r *http.Request) (time.Time, time.Time, error) {
	now := time.Now()
	start, err := parseUnixNano(r.URL.Query().Get("start"), now.Add(-defaultSince))
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	end, err := parseUnixNano(r.URL.Query().Get("end"), now)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	return start, end, nil
}

func step(r *http.Request) (time.Duration, error) {
	s, err := parseInt(r.URL.Query().Get("step"), defaultStep)
	if err != nil {
		return 0, err
	}
	return time.Duration(s) * time.Second, nil
}

func tailDelay(r *http.Request) (uint32, error) {
	l, err := parseInt(r.URL.Query().Get("delay_for"), 0)
	if err != nil {
		return 0, err
	}
	return uint32(l), nil
}
