package tail

import (
	"fmt"
	"net/http"
	"time"

	"github.com/go-kit/log/level"
	"github.com/gorilla/websocket"
	"github.com/grafana/dskit/httpgrpc"

	"github.com/grafana/dskit/tenant"

	"github.com/grafana/loki/v3/pkg/loghttp"
	loghttp_legacy "github.com/grafana/loki/v3/pkg/loghttp/legacy"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/util/httpreq"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
	"github.com/grafana/loki/v3/pkg/util/marshal"
	marshal_legacy "github.com/grafana/loki/v3/pkg/util/marshal/legacy"
	serverutil "github.com/grafana/loki/v3/pkg/util/server"
)

const (
	wsPingPeriod = 1 * time.Second
)

// TailHandler is a http.HandlerFunc for handling tail queries.
func (q *Querier) TailHandler(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(_ *http.Request) bool { return true },
	}
	logger := util_log.WithContext(r.Context(), util_log.Logger)

	req, err := loghttp.ParseTailQuery(r)
	if err != nil {
		serverutil.WriteError(httpgrpc.Errorf(http.StatusBadRequest, "%s", err.Error()), w)
		return
	}

	// ParseTailQuery only validates the label matchers. Build the pipeline up
	// front as well: a query whose pipeline stages can't be compiled (e.g. an
	// invalid regexp in a line filter) is rejected by the ingesters after the
	// websocket upgrade, and since the ingesters reject it on every reconnect
	// the client hangs on an open connection that only receives pings. Fail
	// with a 400 before upgrading, just like query_range does.
	if err := validateTailQuery(req); err != nil {
		serverutil.WriteError(httpgrpc.Errorf(http.StatusBadRequest, "%s", err.Error()), w)
		return
	}

	tenantID, err := tenant.TenantID(r.Context())
	if err != nil {
		level.Warn(logger).Log("msg", "error getting tenant id", "err", err)
		serverutil.WriteError(httpgrpc.Errorf(http.StatusBadRequest, "%s", err.Error()), w)
		return
	}

	encodingFlags := httpreq.ExtractEncodingFlags(r)
	version := loghttp.GetVersion(r.RequestURI)

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		level.Error(logger).Log("msg", "Error in upgrading websocket", "err", err)
		return
	}

	level.Info(logger).Log("msg", "starting to tail logs", "tenant", tenantID, "selectors", req.Query)

	defer func() {
		level.Info(logger).Log("msg", "ended tailing logs", "tenant", tenantID, "selectors", req.Query)
	}()

	defer func() {
		if err := conn.Close(); err != nil {
			level.Error(logger).Log("msg", "Error closing websocket", "err", err)
		}
	}()

	tailer, err := q.Tail(r.Context(), req, encodingFlags.Has(httpreq.FlagCategorizeLabels))
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

	connWriter := marshal.NewWebsocketJSONWriter(conn)

	var response *loghttp_legacy.TailResponse
	responseChan := tailer.getResponseChan()
	closeErrChan := tailer.getCloseErrorChan()

	doneChan := make(chan struct{})
	go func() {
		defer close(doneChan)
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				if closeErr, ok := err.(*websocket.CloseError); ok {
					if closeErr.Code == websocket.CloseNormalClosure {
						return
					}
					level.Error(logger).Log("msg", "Error from client", "err", err)
					return
				} else if tailer.stopped.Load() {
					return
				}

				level.Error(logger).Log("msg", "Unexpected error from client", "err", err)
				return
			}
		}
	}()

	for {
		select {
		case response = <-responseChan:
			var err error
			if version == loghttp.VersionV1 {
				err = marshal.WriteTailResponseJSON(*response, connWriter, encodingFlags)
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

// validateTailQuery compiles the pipeline stages of a parsed tail query so
// that queries the ingesters will reject are refused before the websocket
// upgrade. ParseTailQuery only validates label matchers; pipeline stages are
// compiled lazily on the ingester side, which is too late to tell the HTTP
// client about it.
func validateTailQuery(req *logproto.TailRequest) error {
	expr, ok := req.Plan.AST.(syntax.LogSelectorExpr)
	if !ok {
		return fmt.Errorf("unsupported query expression: want (LogSelectorExpr), got (%T)", req.Plan.AST)
	}
	_, err := expr.Pipeline()
	return err
}
