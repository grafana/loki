package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/weaveworks/common/httpgrpc"

	"github.com/grafana/loki/pkg/logql"
)

// StatusClientClosedRequest is the status code for when a client request cancellation of an http request
const StatusClientClosedRequest = 499

// WriteError write a go error with the correct status code.
func WriteError(err error, w http.ResponseWriter) {
	var queryErr chunk.QueryError

	switch {
	case errors.Is(err, context.Canceled):
		http.Error(w, err.Error(), StatusClientClosedRequest)
	case errors.Is(err, context.DeadlineExceeded):
		http.Error(w, err.Error(), http.StatusGatewayTimeout)
	case errors.As(err, &queryErr):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case logql.IsParseError(err):
		http.Error(w, err.Error(), http.StatusBadRequest)
	default:
		if grpcErr, ok := httpgrpc.HTTPResponseFromError(err); ok {
			http.Error(w, string(grpcErr.Body), int(grpcErr.Code))
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
