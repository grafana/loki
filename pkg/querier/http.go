package querier

import (
	"context"
	"net/http"
	"time"

	"github.com/grafana/loki/pkg/loghttp"
	loghttp_legacy "github.com/grafana/loki/pkg/loghttp/legacy"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logql/marshal"
	marshal_legacy "github.com/grafana/loki/pkg/logql/marshal/legacy"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log/level"
	"github.com/gorilla/websocket"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/weaveworks/common/httpgrpc"
)

const (
	wsPingPeriod = 1 * time.Second
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

	request, err := loghttp.ParseRangeQuery(r)
	if err != nil {
		http.Error(w, httpgrpc.Errorf(http.StatusBadRequest, err.Error()).Error(), http.StatusBadRequest)
		return
	}
	query := q.engine.NewRangeQuery(q, request.Query, request.Start, request.End, request.Step, request.Direction, request.Limit)
	result, err := query.Exec(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := marshal.WriteQueryResponseJSON(result, w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

// InstantQueryHandler is a http.HandlerFunc for instant queries.
func (q *Querier) InstantQueryHandler(w http.ResponseWriter, r *http.Request) {
	// Enforce the query timeout while querying backends
	ctx, cancel := context.WithDeadline(r.Context(), time.Now().Add(q.cfg.QueryTimeout))
	defer cancel()

	request, err := loghttp.ParseInstantQuery(r)
	if err != nil {
		http.Error(w, httpgrpc.Errorf(http.StatusBadRequest, err.Error()).Error(), http.StatusBadRequest)
		return
	}
	query := q.engine.NewInstantQuery(q, request.Query, request.Ts, request.Direction, request.Limit)
	result, err := query.Exec(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := marshal.WriteQueryResponseJSON(result, w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

// LogQueryHandler is a http.HandlerFunc for log only queries.
func (q *Querier) LogQueryHandler(w http.ResponseWriter, r *http.Request) {
	// Enforce the query timeout while querying backends
	ctx, cancel := context.WithDeadline(r.Context(), time.Now().Add(q.cfg.QueryTimeout))
	defer cancel()

	request, err := loghttp.ParseRangeQuery(r)
	if err != nil {
		http.Error(w, httpgrpc.Errorf(http.StatusBadRequest, err.Error()).Error(), http.StatusBadRequest)
		return
	}
	request.Query, err = parseRegexQuery(r)
	if err != nil {
		http.Error(w, httpgrpc.Errorf(http.StatusBadRequest, err.Error()).Error(), http.StatusBadRequest)
		return
	}

	query := q.engine.NewRangeQuery(q, request.Query, request.Start, request.End, request.Step, request.Direction, request.Limit)
	result, err := query.Exec(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := marshal_legacy.WriteQueryResponseJSON(result, w); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

// LabelHandler is a http.HandlerFunc for handling label queries.
func (q *Querier) LabelHandler(w http.ResponseWriter, r *http.Request) {
	req, err := loghttp.ParseLabelQuery(r)
	if err != nil {
		http.Error(w, httpgrpc.Errorf(http.StatusBadRequest, err.Error()).Error(), http.StatusBadRequest)
		return
	}

	resp, err := q.Label(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if loghttp.GetVersion(r.RequestURI) == loghttp.VersionV1 {
		err = marshal.WriteLabelResponseJSON(*resp, w)
	} else {
		err = marshal_legacy.WriteLabelResponseJSON(*resp, w)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

// TailHandler is a http.HandlerFunc for handling tail queries.
func (q *Querier) TailHandler(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	req, err := loghttp.ParseTailQuery(r)
	if err != nil {
		http.Error(w, httpgrpc.Errorf(http.StatusBadRequest, err.Error()).Error(), http.StatusBadRequest)
		return
	}

	req.Query, err = parseRegexQuery(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		level.Error(util.Logger).Log("msg", "Error in upgrading websocket", "err", err)
		return
	}

	defer func() {
		if err := conn.Close(); err != nil {
			level.Error(util.Logger).Log("msg", "Error closing websocket", "err", err)
		}
	}()

	tailer, err := q.Tail(r.Context(), req)
	if err != nil {
		if err := conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseInternalServerErr, err.Error())); err != nil {
			level.Error(util.Logger).Log("msg", "Error connecting to ingesters for tailing", "err", err)
		}
		return
	}
	defer func() {
		if err := tailer.close(); err != nil {
			level.Error(util.Logger).Log("msg", "Error closing Tailer", "err", err)
		}
	}()

	ticker := time.NewTicker(wsPingPeriod)
	defer ticker.Stop()

	var response *loghttp_legacy.TailResponse
	responseChan := tailer.getResponseChan()
	closeErrChan := tailer.getCloseErrorChan()

	for {
		select {
		case response = <-responseChan:
			var err error
			if loghttp.GetVersion(r.RequestURI) == loghttp.VersionV1 {
				err = marshal.WriteTailResponseJSON(*response, conn)
			} else {
				err = marshal_legacy.WriteTailResponseJSON(*response, conn)
			}
			if err != nil {
				level.Error(util.Logger).Log("msg", "Error writing to websocket", "err", err)
				if err := conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseInternalServerErr, err.Error())); err != nil {
					level.Error(util.Logger).Log("msg", "Error writing close message to websocket", "err", err)
				}
				return
			}

		case err := <-closeErrChan:
			level.Error(util.Logger).Log("msg", "Error from iterator", "err", err)
			if err := conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseInternalServerErr, err.Error())); err != nil {
				level.Error(util.Logger).Log("msg", "Error writing close message to websocket", "err", err)
			}
			return
		case <-ticker.C:
			// This is to periodically check whether connection is active, useful to clean up dead connections when there are no entries to send
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				level.Error(util.Logger).Log("msg", "Error writing ping message to websocket", "err", err)
				if err := conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseInternalServerErr, err.Error())); err != nil {
					level.Error(util.Logger).Log("msg", "Error writing close message to websocket", "err", err)
				}
				return
			}
		}
	}
}

// parseRegexQuery parses regex and query querystring from httpRequest and returns the combined LogQL query.
// This is used only to keep regexp query string support until it gets fully deprecated.
func parseRegexQuery(httpRequest *http.Request) (string, error) {
	params := httpRequest.URL.Query()
	query := params.Get("query")
	regexp := params.Get("regexp")
	if regexp != "" {
		expr, err := logql.ParseLogSelector(query)
		if err != nil {
			return "", err
		}
		query = logql.NewFilterExpr(expr, labels.MatchRegexp, regexp).String()
	}
	return query, nil
}
