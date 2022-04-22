package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/prometheus/prometheus/promql"
	"github.com/weaveworks/common/httpgrpc"
	"github.com/weaveworks/common/user"
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
	cerr, status := ClientErrorAndStatus(err)
	http.Error(w, cerr.Error(), status)
}

// ClientErrorAndHTTPStatus returns error and http status that is "safe" to return to client without
// exposing any implementation details.
func ClientErrorAndStatus(err error) (error, int) {
	var (
		queryErr storage_errors.QueryError
		promErr  promql.ErrStorage
	)

	me, ok := err.(util.MultiError)
	if ok && me.Is(context.Canceled) {
		return errors.New(ErrClientCanceled), StatusClientClosedRequest
	}
	if ok && me.IsDeadlineExceeded() {
		return errors.New(ErrDeadlineExceeded), http.StatusGatewayTimeout
	}

	s, isRPC := status.FromError(err)
	switch {
	case errors.Is(err, context.Canceled) ||
		(errors.As(err, &promErr) && errors.Is(promErr.Err, context.Canceled)):
		return errors.New(ErrClientCanceled), StatusClientClosedRequest
	case errors.Is(err, context.DeadlineExceeded) ||
		(isRPC && s.Code() == codes.DeadlineExceeded):
		return errors.New(ErrDeadlineExceeded), http.StatusGatewayTimeout
	case errors.As(err, &queryErr):
		return err, http.StatusBadRequest
	case errors.Is(err, logqlmodel.ErrLimit) || errors.Is(err, logqlmodel.ErrParse) || errors.Is(err, logqlmodel.ErrPipeline):
		return err, http.StatusBadRequest
	case errors.Is(err, user.ErrNoOrgID):
		return err, http.StatusBadRequest
	default:
		if grpcErr, ok := httpgrpc.HTTPResponseFromError(err); ok {
			return errors.New(string(grpcErr.Body)), int(grpcErr.Code)
		}
		return err, http.StatusInternalServerError
	}
}
