package querier

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log/level"
	"github.com/gorilla/mux"
	"github.com/grafana/loki/pkg/logproto"
)

const (
	defaultQueryLimit = 100
	defaulSince       = 1 * time.Hour
)

// nolint
func intParam(values url.Values, name string, def int) (int, error) {
	value := values.Get(name)
	if value == "" {
		return def, nil
	}

	return strconv.Atoi(value)
}

func unixNanoTimeParam(values url.Values, name string, def time.Time) (time.Time, error) {
	value := values.Get(name)
	if value == "" {
		return def, nil
	}

	nanos, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return time.Time{}, err
	}

	return time.Unix(0, nanos), nil
}

// nolint
func directionParam(values url.Values, name string, def logproto.Direction) (logproto.Direction, error) {
	value := values.Get(name)
	if value == "" {
		return def, nil
	}

	d, ok := logproto.Direction_value[strings.ToUpper(value)]
	if !ok {
		return logproto.FORWARD, fmt.Errorf("invalid direction '%s'", value)
	}
	return logproto.Direction(d), nil
}

// QueryHandler is a http.HandlerFunc for queries.
func (q *Querier) QueryHandler(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	query := params.Get("query")
	limit, err := intParam(params, "limit", defaultQueryLimit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	now := time.Now()
	start, err := unixNanoTimeParam(params, "start", now.Add(-defaulSince))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	end, err := unixNanoTimeParam(params, "end", now)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	direction, err := directionParam(params, "direction", logproto.BACKWARD)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	request := logproto.QueryRequest{
		Query:     query,
		Limit:     uint32(limit),
		Start:     start,
		End:       end,
		Direction: direction,
		Regex:     params.Get("regexp"),
	}

	level.Debug(util.Logger).Log("request", fmt.Sprintf("%+v", request))
	result, err := q.Query(r.Context(), &request)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := json.NewEncoder(w).Encode(result); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

// LabelHandler is a http.HandlerFunc for handling label queries.
func (q *Querier) LabelHandler(w http.ResponseWriter, r *http.Request) {
	name, ok := mux.Vars(r)["name"]
	req := &logproto.LabelRequest{
		Values: ok,
		Name:   name,
	}
	resp, err := q.Label(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}
