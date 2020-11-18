package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/weaveworks/common/httpgrpc"
	"github.com/weaveworks/common/user"

	"github.com/grafana/loki/pkg/logql"
)

// StatusClientClosedRequest is the status code for when a client request cancellation of an http request
const StatusClientClosedRequest = 499

const (
	ErrClientCanceled   = "The request was cancelled by the client."
	ErrDeadlineExceeded = "Request timed out, decrease the duration of the request or add more label matchers (prefer exact match over regex match) to reduce the amount of data processed."
)

// WriteError write a go error with the correct status code.
func WriteError(err error, w http.ResponseWriter) {
	var queryErr chunk.QueryError

	switch {
	case errors.Is(err, context.Canceled):
		http.Error(w, ErrClientCanceled, StatusClientClosedRequest)
	case errors.Is(err, context.DeadlineExceeded):
		http.Error(w, ErrDeadlineExceeded, http.StatusGatewayTimeout)
	case errors.As(err, &queryErr):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, logql.ErrLimit) || errors.Is(err, logql.ErrParse) || errors.Is(err, logql.ErrPipeline):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, user.ErrNoOrgID):
		http.Error(w, err.Error(), http.StatusBadRequest)
	default:
		if grpcErr, ok := httpgrpc.HTTPResponseFromError(err); ok {
			http.Error(w, string(grpcErr.Body), int(grpcErr.Code))
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
