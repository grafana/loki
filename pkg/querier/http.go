package querier

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/loki/pkg/helpers"
	"github.com/weaveworks/common/httpgrpc"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log/level"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
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

func httpRequestToQueryRequest(httpRequest *http.Request) (*logproto.QueryRequest, error) {
	params := httpRequest.URL.Query()
	now := time.Now()
	queryRequest := logproto.QueryRequest{
		Regex: params.Get("regexp"),
		Query: params.Get("query"),
	}

	limit, err := intParam(params, "limit", defaultQueryLimit)
	if err != nil {
		return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
	}
	queryRequest.Limit = uint32(limit)

	queryRequest.Start, err = unixNanoTimeParam(params, "start", now.Add(-defaulSince))
	if err != nil {
		return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
	}

	queryRequest.End, err = unixNanoTimeParam(params, "end", now)
	if err != nil {
		return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
	}

	queryRequest.Direction, err = directionParam(params, "direction", logproto.BACKWARD)
	if err != nil {
		return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
	}

	return &queryRequest, nil
}

func writeHTTPErrorResponse(err error, defaultStatusCode int, w http.ResponseWriter) {
	statusCode := defaultStatusCode
	var errMessage string

	if httpResponse, isHTTPResponse := httpgrpc.HTTPResponseFromError(err); isHTTPResponse {
		errMessage = string(httpResponse.Body)
		statusCode = int(httpResponse.Code)
	} else {
		errMessage = err.Error()
	}

	http.Error(w, errMessage, statusCode)
}

// QueryHandler is a http.HandlerFunc for queries.
func (q *Querier) QueryHandler(w http.ResponseWriter, r *http.Request) {
	request, err := httpRequestToQueryRequest(r)
	if err != nil {
		writeHTTPErrorResponse(err, http.StatusBadRequest, w)
		return
	}

	level.Debug(util.Logger).Log("request", fmt.Sprintf("%+v", request))
	result, err := q.Query(r.Context(), request)
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

// TailHandler is a http.HandlerFunc for handling tail queries.
func (q *Querier) TailHandler(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	queryRequestPtr, err := httpRequestToQueryRequest(r)
	if err != nil {
		writeHTTPErrorResponse(err, http.StatusBadRequest, w)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		level.Error(util.Logger).Log("Error in upgrading websocket", fmt.Sprintf("%v", err))
		return
	}

	defer func() {
		err := conn.Close()
		level.Error(util.Logger).Log("Error closing websocket", fmt.Sprintf("%v", err))
	}()

	// response from httpRequestToQueryRequest is a ptr, if we keep passing pointer down the call then it would stay on
	// heap until connection to websocket stays open
	queryRequest := *queryRequestPtr
	itr := q.tailQuery(r.Context(), &queryRequest)
	stream := logproto.Stream{}

	for itr.Next() {
		stream.Entries = []logproto.Entry{itr.Entry()}
		stream.Labels = itr.Labels()

		err := conn.WriteJSON(stream)
		if err != nil {
			level.Error(util.Logger).Log("Error writing to websocket", fmt.Sprintf("%v", err))
			helpers.LogError("writing close message to websocket", func() error {
				return conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseInternalServerErr, err.Error()))
			})
			break
		}
	}

	if err := itr.Error(); err != nil {
		level.Error(util.Logger).Log("Error from iterator", fmt.Sprintf("%v", err))
		helpers.LogError("writing close message to websocket", func() error {
			return conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseInternalServerErr, err.Error()))
		})
	}
}
