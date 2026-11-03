package querytee

import (
	"context"
	"io"
	"net/http"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/user"

	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/util/server"
)

// QueryHandler decodes a query request, sends it to the downstream handler and writes the response.
type QueryHandler struct {
	codec         queryrangebase.Codec
	fanOutHandler queryrangebase.Handler
	logger        log.Logger
}

// NewQueryHandler returns a handler that serves query requests with the given handler. It
// requires the org ID of the tenant on every request.
func NewQueryHandler(codec queryrangebase.Codec, fanOutHandler queryrangebase.Handler, logger log.Logger) *QueryHandler {
	return &QueryHandler{codec: codec, fanOutHandler: fanOutHandler, logger: logger}
}

// ServeHTTP implements http.Handler. It answers with its own error when it cannot read the tenant
// or decode the request.
func (h *QueryHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	_, ctx, err := user.ExtractOrgIDFromHTTPRequest(r)
	if err != nil {
		level.Warn(h.logger).Log(
			"msg", "failed to extract tenant ID",
			"err", err,
			"req", r.URL.String(),
		)
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	req, err := h.codec.DecodeRequest(ctx, r, nil)
	if err != nil {
		query := r.Form.Get("query")
		level.Warn(h.logger).Log(
			"msg", "failed to decode request",
			"query", query,
			"req", r.URL.String(),
			"err", err,
		)
		server.WriteError(err, w)
		return
	}

	resp, err := h.fanOutHandler.Do(ctx, req)
	h.writeResponse(ctx, r, w, resp, err)
}

// writeResponse handles encoding and writing the response back to the HTTP response writer.
func (h *QueryHandler) writeResponse(ctx context.Context, r *http.Request, w http.ResponseWriter, resp queryrangebase.Response, err error) {
	if err != nil {
		switch typedResp := resp.(type) {
		case *NonDecodableResponse:
			level.Warn(h.logger).Log("msg", "backend request failed", "status", typedResp.StatusCode, "err", err)
			http.Error(w, string(typedResp.Body), typedResp.StatusCode)
		default:
			level.Warn(h.logger).Log("msg", "handler failed", "err", err)
			server.WriteError(err, w)
		}
		return
	}

	httpResp, err := h.codec.EncodeResponse(ctx, r, resp)
	if err != nil {
		level.Warn(h.logger).Log("msg", "failed to encode response", "err", err)
		server.WriteError(err, w)
		return
	}
	defer httpResp.Body.Close()

	for key, values := range httpResp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	w.WriteHeader(httpResp.StatusCode)

	if _, err := io.Copy(w, httpResp.Body); err != nil {
		level.Warn(h.logger).Log("msg", "unable to write response body", "err", err)
	}
}
