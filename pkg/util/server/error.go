package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/promql"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/grafana/loki/pkg/logqlmodel"
	storage_errors "github.com/grafana/loki/pkg/storage/errors"
	"github.com/grafana/loki/pkg/util"
)

// StatusClientClosedRequest is the status code for when a client request cancellation of an http request
const StatusClientClosedRequest = 499

const (
	ErrClientCanceled   = "The request was cancelled by the client."
	ErrDeadlineExceeded = "Request timed out, decrease the duration of the request or add more label matchers (prefer exact match over regex match) to reduce the amount of data processed."
)

// WriteError write a go error with the correct status code.
func WriteError(err error, w http.ResponseWriter) {
	status, cerr := ClientHTTPStatusAndError(err)
	http.Error(w, cerr.Error(), status)
}

// ClientHTTPStatusAndError returns error and http status that is "safe" to return to client without
// exposing any implementation details.
func ClientHTTPStatusAndError(err error) (int, error) {
	var (
		queryErr storage_errors.QueryError
		promErr  promql.ErrStorage
	)

	me, ok := err.(util.MultiError)
	if ok && me.Is(context.Canceled) {
		return StatusClientClosedRequest, errors.New(ErrClientCanceled)
	}
	if ok && me.IsDeadlineExceeded() {
		return http.StatusGatewayTimeout, errors.New(ErrDeadlineExceeded)
	}

	s, isRPC := status.FromError(err)
	switch {
	case errors.Is(err, context.Canceled) ||
		(errors.As(err, &promErr) && errors.Is(promErr.Err, context.Canceled)):
		return StatusClientClosedRequest, errors.New(ErrClientCanceled)
	case errors.Is(err, context.DeadlineExceeded) ||
		(isRPC && s.Code() == codes.DeadlineExceeded):
		return http.StatusGatewayTimeout, errors.New(ErrDeadlineExceeded)
	case errors.As(err, &queryErr):
		return http.StatusBadRequest, err
	case errors.Is(err, logqlmodel.ErrLimit) || errors.Is(err, logqlmodel.ErrParse) || errors.Is(err, logqlmodel.ErrPipeline) || errors.Is(err, logqlmodel.ErrBlocked):
		return http.StatusBadRequest, err
	case errors.Is(err, user.ErrNoOrgID):
		return http.StatusBadRequest, err
	default:
		if grpcErr, ok := httpgrpc.HTTPResponseFromError(err); ok {
			return int(grpcErr.Code), errors.New(string(grpcErr.Body))
		}
		return http.StatusInternalServerError, err
	}
}
