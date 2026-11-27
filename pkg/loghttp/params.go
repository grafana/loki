package loghttp

import (
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/c2h5oh/datasize"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

const (
	defaultQueryLimit = 100
	defaultLimit      = 1000
	defaultSince      = 1 * time.Hour
	defaultDirection  = logproto.BACKWARD
)

func limit(r *http.Request) (uint32, error) {
	l, err := parseInt(r.Form.Get("limit"), defaultQueryLimit)
	if err != nil {
		return 0, err
	}
	if l <= 0 {
		return 0, errors.New("limit must be a positive value")
	}
	return uint32(l), nil
}

func lineLimit(r *http.Request) (uint32, error) {
	l, err := parseInt(r.Form.Get("line_limit"), defaultQueryLimit)
	if err != nil {
		return 0, err
	}
	if l <= 0 {
		return 0, errors.New("limit must be a positive value")
	}
	return uint32(l), nil
}

func detectedFieldsLimit(r *http.Request) (uint32, error) {
	limit := r.Form.Get("limit")
	if limit == "" {
		// for backwards compatibility
		limit = r.Form.Get("field_limit")
	}

	l, err := parseInt(limit, defaultLimit)
	if err != nil {
		return 0, err
	}

	if l <= 0 {
		return 0, errors.New("limit must be a positive value")
	}
	return uint32(l), nil
}

func query(r *http.Request) string {
	return r.Form.Get("query")
}

func ts(r *http.Request) (time.Time, error) {
	t, err := parseTimestamp(r.Form.Get("time"), time.Now())
	if err != nil {
		return time.Time{}, errors.Wrap(err, "could not parse 'time' parameter")
	}
	return t, nil
}

func direction(r *http.Request) (logproto.Direction, error) {
	return parseDirection(r.Form.Get("direction"), defaultDirection)
}

func shards(r *http.Request) []string {
	return r.Form["shards"]
}

func bounds(r *http.Request) (time.Time, time.Time, error) {
	now := time.Now()
	start := r.Form.Get("start")
	end := r.Form.Get("end")
	since := r.Form.Get("since")
	return determineBounds(now, start, end, since)
}

func determineBounds(now time.Time, startString, endString, sinceString string) (time.Time, time.Time, error) {
	since := defaultSince
	if sinceString != "" {
		d, err := model.ParseDuration(sinceString)
		if err != nil {
			return time.Time{}, time.Time{}, errors.Wrap(err, "could not parse 'since' parameter")
		}
		since = time.Duration(d)
	}

	end, err := parseTimestamp(endString, now)
	if err != nil {
		return time.Time{}, time.Time{}, errors.Wrap(err, "could not parse 'end' parameter")
	}

	// endOrNow is used to apply a default for the start time or an offset if 'since' is provided.
	// we want to use the 'end' time so long as it's not in the future as this should provide
	// a more intuitive experience when end time is in the future.
	endOrNow := end
	if end.After(now) {
		endOrNow = now
	}

	start, err := parseTimestamp(startString, endOrNow.Add(-since))
	if err != nil {
		return time.Time{}, time.Time{}, errors.Wrap(err, "could not parse 'start' parameter")
	}
	if start.Before(minTimestamp) {
		return time.Time{}, time.Time{}, errors.Errorf("the start time that 'end' and 'since' give is before %s, set 'start' or a smaller 'since'", minTimestamp.Format(time.RFC3339))
	}

	return start, end, nil
}

func step(r *http.Request, start, end time.Time) (time.Duration, error) {
	value := r.Form.Get("step")
	if value == "" {
		return time.Duration(defaultQueryRangeStep(start, end)) * time.Second, nil
	}
	return parseSecondsOrDuration(value)
}

func interval(r *http.Request) (time.Duration, error) {
	value := r.Form.Get("interval")
	if value == "" {
		return 0, nil
	}
	return parseSecondsOrDuration(value)
}

// defaultQueryRangeStep returns the default step used in the query range API,
// which is dynamically calculated based on the time range
func defaultQueryRangeStep(start time.Time, end time.Time) int {
	return int(math.Max(math.Floor(end.Sub(start).Seconds()/250), 1))
}

func tailDelay(r *http.Request) (uint32, error) {
	l, err := parseInt(r.Form.Get("delay_for"), 0)
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

// Loki computes with times as int64 nanoseconds since the Unix epoch, which
// holds times up to 2262-04-11. After that limit, UnixNano overflows without
// an error and gives a different time, so a query reads the wrong time range.
//
// maxTimestamp leaves a margin of about 100 days below the limit, because
// query splitting and step alignment move the end time a little later.
//
// The range starts at the Unix epoch, because a log query has no use for an
// earlier time.
var (
	minTimestamp = time.Unix(0, 0).UTC()
	maxTimestamp = time.Date(2262, time.January, 1, 0, 0, 0, 0, time.UTC)
)

// parseTimestamp parses a ns unix timestamp from a string
// if the value is empty it returns a default value passed as second parameter
//
// It returns an error if the time is outside [minTimestamp, maxTimestamp].
func parseTimestamp(value string, def time.Time) (time.Time, error) {
	if value == "" {
		return def, nil
	}

	t, err := parseTimestampValue(value)
	if err != nil {
		return time.Time{}, err
	}
	if t.Before(minTimestamp) || t.After(maxTimestamp) {
		return time.Time{}, timestampOutOfRangeError(value)
	}
	return t, nil
}

func parseTimestampValue(value string) (time.Time, error) {
	if strings.Contains(value, ".") {
		if t, err := strconv.ParseFloat(value, 64); err == nil {
			// Go does not define the result of a float to int64 conversion
			// that overflows, so reject such values before the conversion.
			if t < float64(minTimestamp.Unix()) || t >= float64(maxTimestamp.Unix()+1) {
				return time.Time{}, timestampOutOfRangeError(value)
			}
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

func timestampOutOfRangeError(value string) error {
	return fmt.Errorf("timestamp %q is out of range [%s, %s]", value, minTimestamp.Format(time.RFC3339), maxTimestamp.Format(time.RFC3339))
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

func parseSecondsOrDuration(value string) (time.Duration, error) {
	if d, err := strconv.ParseFloat(value, 64); err == nil {
		ts := d * float64(time.Second)
		if ts > float64(math.MaxInt64) || ts < float64(math.MinInt64) {
			return 0, errors.Errorf("cannot parse %q to a valid duration. It overflows int64", value)
		}
		return time.Duration(ts), nil
	}
	if d, err := model.ParseDuration(value); err == nil {
		return time.Duration(d), nil
	}
	return 0, errors.Errorf("cannot parse %q to a valid duration", value)
}

// parseRegexQuery parses regex and query querystring from httpRequest and returns the combined LogQL query.
// This is used only to keep regexp query string support until it gets fully deprecated.
func parseRegexQuery(httpRequest *http.Request) (string, error) {
	query := httpRequest.Form.Get("query")
	regexp := httpRequest.Form.Get("regexp")
	if regexp != "" {
		expr, err := syntax.ParseLogSelector(query, true)
		if err != nil {
			return "", err
		}
		newExpr, err := syntax.AddFilterExpr(expr, log.LineMatchRegexp, "", regexp)
		if err != nil {
			return "", err
		}
		query = newExpr.String()
	}
	return query, nil
}

func parseBytes(r *http.Request, field string, optional bool) (val datasize.ByteSize, err error) {
	s := r.Form.Get(field)

	if s == "" {
		if !optional {
			return 0, fmt.Errorf("missing %s", field)
		}
		return val, nil
	}

	if err := val.UnmarshalText([]byte(s)); err != nil {
		return 0, errors.Wrapf(err, "invalid %s: %s", field, s)
	}

	return val, nil

}
