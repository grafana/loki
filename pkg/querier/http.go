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
	defaultQueryLimit    = 100
	defaultSince         = 1 * time.Hour
	wsPingPeriod         = 1 * time.Second
	maxDelayForInTailing = 5
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
		if ts, err := time.Parse(time.RFC3339Nano, value); err == nil {
			return ts, nil
		}
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
	queryRequest := logproto.QueryRequest{
		Regex: params.Get("regexp"),
		Query: params.Get("query"),
	}

	var err error
	queryRequest.Limit, queryRequest.Start, queryRequest.End, err = httpRequestToLookback(httpRequest)
	if err != nil {
		return nil, err
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
	var err error
	tailRequest.Limit, tailRequest.Start, _, err = httpRequestToLookback(httpRequest)
	if err != nil {
		return nil, err
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

func httpRequestToLookback(httpRequest *http.Request) (limit uint32, start, end time.Time, err error) {
	params := httpRequest.URL.Query()
	now := time.Now()

	lim, err := intParam(params, "limit", defaultQueryLimit)
	if err != nil {
		return 0, time.Now(), time.Now(), httpgrpc.Errorf(http.StatusBadRequest, err.Error())
	}
	limit = uint32(lim)

	start, err = unixNanoTimeParam(params, "start", now.Add(-defaultSince))
	if err != nil {
		return 0, time.Now(), time.Now(), httpgrpc.Errorf(http.StatusBadRequest, err.Error())
	}

	end, err = unixNanoTimeParam(params, "end", now)
	if err != nil {
		return 0, time.Now(), time.Now(), httpgrpc.Errorf(http.StatusBadRequest, err.Error())
	}
	return
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
	params := r.URL.Query()
	now := time.Now()
	req := &logproto.LabelRequest{
		Values: ok,
		Name:   name,
	}

	end, err := unixNanoTimeParam(params, "end", now)
	if err != nil {
		http.Error(w, httpgrpc.Errorf(http.StatusBadRequest, err.Error()).Error(), http.StatusBadRequest)
		return
	}
	req.End = &end

	start, err := unixNanoTimeParam(params, "start", end.Add(-6*time.Hour))
	if err != nil {
		http.Error(w, httpgrpc.Errorf(http.StatusBadRequest, err.Error()).Error(), http.StatusBadRequest)
		return
	}
	req.Start = &start

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

	tailRequestPtr, err := httpRequestToTailRequest(r)
	if err != nil {
		server.WriteError(w, err)
		return
	}

	if tailRequestPtr.DelayFor > maxDelayForInTailing {
		server.WriteError(w, fmt.Errorf("delay_for can't be greater than %d", maxDelayForInTailing))
		level.Error(util.Logger).Log("Error in upgrading websocket", fmt.Sprintf("%v", err))
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

	tailer, err := q.Tail(r.Context(), &tailRequest)
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

	ticker := time.NewTicker(wsPingPeriod)
	defer ticker.Stop()

	var response *TailResponse
	responseChan := tailer.getResponseChan()
	closeErrChan := tailer.getCloseErrorChan()

	for {
		select {
		case response = <-responseChan:
			err := conn.WriteJSON(*response)
			if err != nil {
				level.Error(util.Logger).Log("Error writing to websocket", fmt.Sprintf("%v", err))
				if err := conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseInternalServerErr, err.Error())); err != nil {
					level.Error(util.Logger).Log("Error writing close message to websocket", fmt.Sprintf("%v", err))
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
