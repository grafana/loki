package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/promql"
	"google.golang.org/grpc/codes"

	"github.com/gogo/googleapis/google/rpc"
	"github.com/gogo/status"

	"github.com/grafana/loki/v3/pkg/logqlmodel"
	storage_errors "github.com/grafana/loki/v3/pkg/storage/errors"
	"github.com/grafana/loki/v3/pkg/util"
)

// StatusClientClosedRequest is the status code for when a client request cancellation of an http request
const StatusClientClosedRequest = 499

const (
	ErrClientCanceled   = "the request was cancelled by the client"
	ErrDeadlineExceeded = "request timed out, decrease the duration of the request or add more label matchers (prefer exact match over regex match) to reduce the amount of data processed"
)

type UserError string

func (e UserError) Error() string {
	return string(e)
}

func ClientGrpcStatusAndError(err error) error {
	if err == nil {
		return nil
	}

	status, newErr := ClientHTTPStatusAndError(err)
	return httpgrpc.Errorf(status, "%s", newErr.Error())
}

// WriteError write a go error with the correct status code.
func WriteError(err error, w http.ResponseWriter) {
	status, cerr := ClientHTTPStatusAndError(err)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	fmt.Fprint(w, cerr.Error())
}

// ClientHTTPStatusAndError returns error and http status that is "safe" to return to client without
// exposing any implementation details.
func ClientHTTPStatusAndError(err error) (int, error) {
	if err == nil {
		return http.StatusOK, nil
	}

	var (
		queryErr storage_errors.QueryError
		promErr  promql.ErrStorage
		userErr  UserError
	)

	me, ok := err.(util.MultiError)
	if ok && me.Is(context.Canceled) {
		return StatusClientClosedRequest, errors.New(ErrClientCanceled)
	}
	if ok && me.IsDeadlineExceeded() {
		return http.StatusGatewayTimeout, errors.New(ErrDeadlineExceeded)
	}

	// Return 400 if any of the errors in the MultiError are client errors (4xx)
	if ok {
		for _, e := range me {
			if isClientError(e, &queryErr, &userErr) {
				return http.StatusBadRequest, err
			}
		}
	}

	if isClientError(err, &queryErr, &userErr) {
		return http.StatusBadRequest, err
	}

	if s, isRPC := status.FromError(err); isRPC {
		if s.Code() == codes.DeadlineExceeded {
			return http.StatusGatewayTimeout, errors.New(ErrDeadlineExceeded)
		} else if int(s.Code())/100 == 4 || int(s.Code())/100 == 5 {
			return int(s.Code()), errors.New(s.Message())
		}
		return http.StatusInternalServerError, err
	}

	switch {
	case errors.Is(err, context.Canceled) ||
		(errors.As(err, &promErr) && errors.Is(promErr.Err, context.Canceled)):
		return StatusClientClosedRequest, errors.New(ErrClientCanceled)
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusGatewayTimeout, errors.New(ErrDeadlineExceeded)
	case errors.Is(err, logqlmodel.ErrIntervalLimit):
		return http.StatusBadRequest, err
	default:
		if grpcErr, ok := httpgrpc.HTTPResponseFromError(err); ok {
			return int(grpcErr.Code), errors.New(string(grpcErr.Body))
		}
		return http.StatusInternalServerError, err
	}
}

// isClientError reports whether err is (or wraps) a Loki query error that is
// the client's fault and is  not retryable.
func isClientError(err error, queryErr *storage_errors.QueryError, userErr *UserError) bool {
	return errors.As(err, queryErr) ||
		errors.As(err, userErr) ||
		errors.Is(err, logqlmodel.ErrLimit) ||
		errors.Is(err, logqlmodel.ErrParse) ||
		errors.Is(err, logqlmodel.ErrPipeline) ||
		errors.Is(err, logqlmodel.ErrBlocked) ||
		errors.Is(err, logqlmodel.ErrParseMatchers) ||
		errors.Is(err, logqlmodel.ErrUnsupportedSyntaxForInstantQuery) ||
		errors.Is(err, logqlmodel.ErrVariantsDisabled) ||
		errors.Is(err, user.ErrNoOrgID)
}

// WrapError wraps an error in a protobuf status.
func WrapError(err error) *rpc.Status {
	var (
		queryErr storage_errors.QueryError
		userErr  UserError
	)

	if !isClientError(err, &queryErr, &userErr) {
		if s, ok := status.FromError(err); ok {
			return s.Proto()
		}
	}

	code, err := ClientHTTPStatusAndError(err)
	return status.New(codes.Code(code), err.Error()).Proto()
}

func UnwrapError(s *rpc.Status) error {
	return status.ErrorProto(s)
}
