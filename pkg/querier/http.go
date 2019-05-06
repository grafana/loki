package querier

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/weaveworks/common/httpgrpc/server"

	"github.com/weaveworks/common/httpgrpc"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log/level"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/grafana/loki/pkg/logproto"
)

const (
	defaultQueryLimit         = 100
	defaulSince               = 1 * time.Hour
	pingPeriod                = 1 * time.Second
	bufferSizeForTailResponse = 10
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

func httpRequestToTailRequest(httpRequest *http.Request) (*logproto.TailRequest, error) {
	params := httpRequest.URL.Query()
	tailRequest := logproto.TailRequest{
		Regex: params.Get("regexp"),
		Query: params.Get("query"),
	}

	// delay_for is used to allow server to let slow loggers catch up.
	// Entries would be accumulated in a heap until they become older than now()-<delay_for>
	delayFor, err := intParam(params, "delay_for", 0)
	if err != nil {
		return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
	}

	tailRequest.DelayFor = uint32(delayFor)

	return &tailRequest, nil
}

// QueryHandler is a http.HandlerFunc for queries.
func (q *Querier) QueryHandler(w http.ResponseWriter, r *http.Request) {
	request, err := httpRequestToQueryRequest(r)
	if err != nil {
		server.WriteError(w, err)
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

type droppedEntry struct {
	Timestamp time.Time
	Labels    string
}

type tailResponse struct {
	Stream         logproto.Stream
	DroppedEntries []droppedEntry
}

// TailHandler is a http.HandlerFunc for handling tail queries.
func (q *Querier) TailHandler(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	tailRequestPtr, err := httpRequestToTailRequest(r)
	if err != nil {
		server.WriteError(w, err)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		level.Error(util.Logger).Log("Error in upgrading websocket", fmt.Sprintf("%v", err))
		return
	}

	defer func() {
		if err := conn.Close(); err != nil {
			level.Error(util.Logger).Log("Error closing websocket", fmt.Sprintf("%v", err))
		}
	}()

	// response from httpRequestToQueryRequest is a ptr, if we keep passing pointer down the call then it would stay on
	// heap until connection to websocket stays open
	tailRequest := *tailRequestPtr
	responseChan := make(chan tailResponse, bufferSizeForTailResponse)
	closeErrChan := make(chan error)

	tailer, err := q.Tail(r.Context(), &tailRequest, responseChan, closeErrChan)
	if err != nil {
		if err := conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseInternalServerErr, err.Error())); err != nil {
			level.Error(util.Logger).Log("Error connecting to ingesters for tailing", fmt.Sprintf("%v", err))
		}
		return
	}
	defer func() {
		if err := tailer.close(); err != nil {
			level.Error(util.Logger).Log("Error closing Tailer", fmt.Sprintf("%v", err))
		}
	}()

	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()

	var response tailResponse

	for {
		select {
		case response = <-responseChan:
			err := conn.WriteJSON(response)
			if err != nil {
				level.Error(util.Logger).Log("Error writing to websocket", fmt.Sprintf("%v", err))
				if err := conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseInternalServerErr, err.Error())); err != nil {
					level.Error(util.Logger).Log("Error writing close message to websocket", fmt.Sprintf("%v", err))
				}
				if err := tailer.close(); err != nil {
					level.Error(util.Logger).Log("Error closing Tailer", fmt.Sprintf("%v", err))
				}
				return
			}
		case err := <-closeErrChan:
			level.Error(util.Logger).Log("Error from iterator", fmt.Sprintf("%v", err))
			if err := conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseInternalServerErr, err.Error())); err != nil {
				level.Error(util.Logger).Log("Error writing close message to websocket", fmt.Sprintf("%v", err))
			}
			return
		case <-ticker.C:
			// This is to periodically check whether connection is active, useful to clean up dead connections when there are no entries to send
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				level.Error(util.Logger).Log("Error writing ping message to websocket", fmt.Sprintf("%v", err))
				if err := conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseInternalServerErr, err.Error())); err != nil {
					level.Error(util.Logger).Log("Error writing close message to websocket", fmt.Sprintf("%v", err))
				}
				return
			}
		}
	}
}
