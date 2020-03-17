package util

import (
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/weaveworks/common/httpgrpc"
)

func TimeMilliseconds(t time.Time) int64 {
	return t.UnixNano() / int64(time.Millisecond/time.Nanosecond)
}

// ParseTime parses the string into an int64, milliseconds since epoch.
func ParseTime(s string) (int64, error) {
	if t, err := strconv.ParseFloat(s, 64); err == nil {
		s, ns := math.Modf(t)
		tm := time.Unix(int64(s), int64(ns*float64(time.Second)))
		return TimeMilliseconds(tm), nil
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return TimeMilliseconds(t), nil
	}
	return 0, httpgrpc.Errorf(http.StatusBadRequest, "cannot parse %q to a valid timestamp", s)
}
