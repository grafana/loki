package request

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/loki/pkg/logproto"
)

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
func parseUnixNano(value string, def time.Time) (time.Time, error) {
	if value == "" {
		return def, nil
	}

	nanos, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		if ts, err := time.Parse(time.RFC3339Nano, value); err == nil {
			return ts, nil
		}
		return time.Time{}, err
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
