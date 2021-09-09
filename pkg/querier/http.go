package querier

import (
	"context"
	"net/http"
	"time"

	"github.com/go-kit/kit/log/level"
	"github.com/gorilla/websocket"
	"github.com/grafana/dskit/dslog"
	"github.com/grafana/dskit/tenant"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/weaveworks/common/httpgrpc"

	"github.com/grafana/loki/pkg/loghttp"
	loghttp_legacy "github.com/grafana/loki/pkg/loghttp/legacy"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logqlmodel"
	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/grafana/loki/pkg/util/marshal"
	marshal_legacy "github.com/grafana/loki/pkg/util/marshal/legacy"
	serverutil "github.com/grafana/loki/pkg/util/server"
)

const (
	wsPingPeriod = 1 * time.Second
)

type QueryResponse struct {
	ResultType parser.ValueType `json:"resultType"`
	Result     parser.Value     `json:"result"`
}

// RangeQueryHandler is a http.HandlerFunc for range queries.
func (q *Querier) RangeQueryHandler(w http.ResponseWriter, r *http.Request) {
	// Enforce the query timeout while querying backends
	ctx, cancel := context.WithDeadline(r.Context(), time.Now().Add(q.cfg.QueryTimeout))
	defer cancel()

	request, err := loghttp.ParseRangeQuery(r)
	if err != nil {
		serverutil.WriteError(httpgrpc.Errorf(http.StatusBadRequest, err.Error()), w)
		return
	}

	if err := q.validateEntriesLimits(ctx, request.Query, request.Limit); err != nil {
		serverutil.WriteError(err, w)
		return
	}

	params := logql.NewLiteralParams(
		request.Query,
		request.Start,
		request.End,
		request.Step,
		request.Interval,
		request.Direction,
		request.Limit,
		request.Shards,
	)
	query := q.engine.Query(params)
	result, err := query.Exec(ctx)
	if err != nil {
		serverutil.WriteError(err, w)
		return
	}

	if err := marshal.WriteQueryResponseJSON(result, w); err != nil {
		serverutil.WriteError(err, w)
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
		serverutil.WriteError(httpgrpc.Errorf(http.StatusBadRequest, err.Error()), w)
		return
	}

	if err := q.validateEntriesLimits(ctx, request.Query, request.Limit); err != nil {
		serverutil.WriteError(err, w)
		return
	}

	params := logql.NewLiteralParams(
		request.Query,
		request.Ts,
		request.Ts,
		0,
		0,
		request.Direction,
		request.Limit,
		request.Shards,
	)
	query := q.engine.Query(params)
	result, err := query.Exec(ctx)
	if err != nil {
		serverutil.WriteError(err, w)
		return
	}

	if err := marshal.WriteQueryResponseJSON(result, w); err != nil {
		serverutil.WriteError(err, w)
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
		serverutil.WriteError(httpgrpc.Errorf(http.StatusBadRequest, err.Error()), w)
		return
	}
	request.Query, err = parseRegexQuery(r)
	if err != nil {
		serverutil.WriteError(httpgrpc.Errorf(http.StatusBadRequest, err.Error()), w)
		return
	}

	expr, err := logql.ParseExpr(request.Query)
	if err != nil {
		serverutil.WriteError(err, w)
		return
	}

	// short circuit metric queries
	if _, ok := expr.(logql.SampleExpr); ok {
		serverutil.WriteError(httpgrpc.Errorf(http.StatusBadRequest, "legacy endpoints only support %s result type", logqlmodel.ValueTypeStreams), w)
		return
	}

	if err := q.validateEntriesLimits(ctx, request.Query, request.Limit); err != nil {
		serverutil.WriteError(err, w)
		return
	}

	params := logql.NewLiteralParams(
		request.Query,
		request.Start,
		request.End,
		request.Step,
		request.Interval,
		request.Direction,
		request.Limit,
		request.Shards,
	)
	query := q.engine.Query(params)

	result, err := query.Exec(ctx)
	if err != nil {
		serverutil.WriteError(err, w)
		return
	}

	if err := marshal_legacy.WriteQueryResponseJSON(result, w); err != nil {
		serverutil.WriteError(err, w)
		return
	}
}

// LabelHandler is a http.HandlerFunc for handling label queries.
func (q *Querier) LabelHandler(w http.ResponseWriter, r *http.Request) {
	req, err := loghttp.ParseLabelQuery(r)
	if err != nil {
		serverutil.WriteError(httpgrpc.Errorf(http.StatusBadRequest, err.Error()), w)
		return
	}

	resp, err := q.Label(r.Context(), req)
	if err != nil {
		serverutil.WriteError(err, w)
		return
	}

	if loghttp.GetVersion(r.RequestURI) == loghttp.VersionV1 {
		err = marshal.WriteLabelResponseJSON(*resp, w)
	} else {
		err = marshal_legacy.WriteLabelResponseJSON(*resp, w)
	}
	if err != nil {
		serverutil.WriteError(err, w)
		return
	}
}

// TailHandler is a http.HandlerFunc for handling tail queries.
func (q *Querier) TailHandler(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
	logger := dslog.WithContext(r.Context(), util_log.Logger)

	req, err := loghttp.ParseTailQuery(r)
	if err != nil {
		serverutil.WriteError(httpgrpc.Errorf(http.StatusBadRequest, err.Error()), w)
		return
	}

	req.Query, err = parseRegexQuery(r)
	if err != nil {
		serverutil.WriteError(httpgrpc.Errorf(http.StatusBadRequest, err.Error()), w)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		level.Error(logger).Log("msg", "Error in upgrading websocket", "err", err)
		return
	}

	defer func() {
		if err := conn.Close(); err != nil {
			level.Error(logger).Log("msg", "Error closing websocket", "err", err)
		}
	}()

	tailer, err := q.Tail(r.Context(), req)
	if err != nil {
		if err := conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseInternalServerErr, err.Error())); err != nil {
			level.Error(logger).Log("msg", "Error connecting to ingesters for tailing", "err", err)
		}
		return
	}
	defer func() {
		if err := tailer.close(); err != nil {
			level.Error(logger).Log("msg", "Error closing Tailer", "err", err)
		}
	}()

	ticker := time.NewTicker(wsPingPeriod)
	defer ticker.Stop()

	var response *loghttp_legacy.TailResponse
	responseChan := tailer.getResponseChan()
	closeErrChan := tailer.getCloseErrorChan()

	doneChan := make(chan struct{})
	go func() {
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				if closeErr, ok := err.(*websocket.CloseError); ok {
					if closeErr.Code == websocket.CloseNormalClosure {
						break
					}
					level.Error(logger).Log("msg", "Error from client", "err", err)
					break
				} else if tailer.stopped {
					return
				} else {
					level.Error(logger).Log("msg", "Unexpected error from client", "err", err)
					break
				}
			}
		}
		doneChan <- struct{}{}
	}()

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
				level.Error(logger).Log("msg", "Error writing to websocket", "err", err)
				if err := conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseInternalServerErr, err.Error())); err != nil {
					level.Error(logger).Log("msg", "Error writing close message to websocket", "err", err)
				}
				return
			}

		case err := <-closeErrChan:
			level.Error(logger).Log("msg", "Error from iterator", "err", err)
			if err := conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseInternalServerErr, err.Error())); err != nil {
				level.Error(logger).Log("msg", "Error writing close message to websocket", "err", err)
			}
			return
		case <-ticker.C:
			// This is to periodically check whether connection is active, useful to clean up dead connections when there are no entries to send
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				level.Error(logger).Log("msg", "Error writing ping message to websocket", "err", err)
				if err := conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseInternalServerErr, err.Error())); err != nil {
					level.Error(logger).Log("msg", "Error writing close message to websocket", "err", err)
				}
				return
			}
		case <-doneChan:
			return
		}
	}
}

// SeriesHandler returns the list of time series that match a certain label set.
// See https://prometheus.io/docs/prometheus/latest/querying/api/#finding-series-by-label-matchers
func (q *Querier) SeriesHandler(w http.ResponseWriter, r *http.Request) {
	req, err := logql.ParseAndValidateSeriesQuery(r)
	if err != nil {
		serverutil.WriteError(httpgrpc.Errorf(http.StatusBadRequest, err.Error()), w)
		return
	}

	resp, err := q.Series(r.Context(), req)
	if err != nil {
		serverutil.WriteError(err, w)
		return
	}

	err = marshal.WriteSeriesResponseJSON(*resp, w)
	if err != nil {
		serverutil.WriteError(err, w)
		return
	}
}

// parseRegexQuery parses regex and query querystring from httpRequest and returns the combined LogQL query.
// This is used only to keep regexp query string support until it gets fully deprecated.
func parseRegexQuery(httpRequest *http.Request) (string, error) {
	query := httpRequest.Form.Get("query")
	regexp := httpRequest.Form.Get("regexp")
	if regexp != "" {
		expr, err := logql.ParseLogSelector(query, true)
		if err != nil {
			return "", err
		}
		newExpr, err := logql.AddFilterExpr(expr, labels.MatchRegexp, "", regexp)
		if err != nil {
			return "", err
		}
		query = newExpr.String()
	}
	return query, nil
}

func (q *Querier) validateEntriesLimits(ctx context.Context, query string, limit uint32) error {
	userID, err := tenant.ID(ctx)
	if err != nil {
		return httpgrpc.Errorf(http.StatusBadRequest, err.Error())
	}

	expr, err := logql.ParseExpr(query)
	if err != nil {
		return err
	}

	// entry limit does not apply to metric queries.
	if _, ok := expr.(logql.SampleExpr); ok {
		return nil
	}

	maxEntriesLimit := q.limits.MaxEntriesLimitPerQuery(userID)
	if int(limit) > maxEntriesLimit && maxEntriesLimit != 0 {
		return httpgrpc.Errorf(http.StatusBadRequest,
			"max entries limit per query exceeded, limit > max_entries_limit (%d > %d)", limit, maxEntriesLimit)
	}
	return nil
}
