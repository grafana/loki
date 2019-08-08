package querier

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log/level"
	"github.com/gorilla/websocket"
	"github.com/prometheus/prometheus/promql"
	"github.com/weaveworks/common/httpgrpc"
	"github.com/weaveworks/common/httpgrpc/server"

	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/querier/request"
)

const (
	wsPingPeriod         = 1 * time.Second
	maxDelayForInTailing = 5
)

type QueryResponse struct {
	ResultType promql.ValueType `json:"resultType"`
	Result     promql.Value     `json:"result"`
}

// RangeQueryHandler is a http.HandlerFunc for range queries.
func (q *Querier) RangeQueryHandler(w http.ResponseWriter, r *http.Request) {
	// Enforce the query timeout while querying backends
	ctx, cancel := context.WithDeadline(r.Context(), time.Now().Add(q.cfg.QueryTimeout))
	defer cancel()

	request, err := request.ParseRangeQuery(r)
	if err != nil {
		server.WriteError(w, httpgrpc.Errorf(http.StatusBadRequest, err.Error()))
		return
	}
	query := q.engine.NewRangeQuery(q, request.Query, request.Start, request.End, request.Step, request.Direction, request.Limit)
	result, err := query.Exec(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	response := &QueryResponse{
		ResultType: result.Type(),
		Result:     result,
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

// InstantQueryHandler is a http.HandlerFunc for instant queries.
func (q *Querier) InstantQueryHandler(w http.ResponseWriter, r *http.Request) {
	// Enforce the query timeout while querying backends
	ctx, cancel := context.WithDeadline(r.Context(), time.Now().Add(q.cfg.QueryTimeout))
	defer cancel()

	request, err := request.ParseInstantQuery(r)
	if err != nil {
		server.WriteError(w, httpgrpc.Errorf(http.StatusBadRequest, err.Error()))
		return
	}
	query := q.engine.NewInstantQuery(q, request.Query, request.Ts, request.Direction, request.Limit)
	result, err := query.Exec(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	response := &QueryResponse{
		ResultType: result.Type(),
		Result:     result,
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

// LogQueryHandler is a http.HandlerFunc for log only queries.
func (q *Querier) LogQueryHandler(w http.ResponseWriter, r *http.Request) {
	// Enforce the query timeout while querying backends
	ctx, cancel := context.WithDeadline(r.Context(), time.Now().Add(q.cfg.QueryTimeout))
	defer cancel()

	request, err := request.ParseRangeQuery(r)
	if err != nil {
		server.WriteError(w, httpgrpc.Errorf(http.StatusBadRequest, err.Error()))
		return
	}
	query := q.engine.NewRangeQuery(q, request.Query, request.Start, request.End, request.Step, request.Direction, request.Limit)
	result, err := query.Exec(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if result.Type() != logql.ValueTypeStreams {
		http.Error(w, fmt.Sprintf("log query only support %s result type, current type is %s", logql.ValueTypeStreams, result.Type()), http.StatusBadRequest)
		return
	}

	if err := json.NewEncoder(w).Encode(
		struct {
			Streams promql.Value `json:"streams"`
		}{
			Streams: result,
		},
	); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

// LabelHandler is a http.HandlerFunc for handling label queries.
func (q *Querier) LabelHandler(w http.ResponseWriter, r *http.Request) {
	req, err := request.ParseLabelQuery(r)
	if err != nil {
		server.WriteError(w, httpgrpc.Errorf(http.StatusBadRequest, err.Error()))
		return
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

	tailRequest, err := request.ParseTailQuery(r)
	if err != nil {
		server.WriteError(w, httpgrpc.Errorf(http.StatusBadRequest, err.Error()))
		return
	}

	if tailRequest.DelayFor > maxDelayForInTailing {
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

	tailer, err := q.Tail(r.Context(), tailRequest)
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
