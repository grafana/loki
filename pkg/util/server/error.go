package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/promql"
	"google.golang.org/grpc/codes"

	"github.com/gogo/googleapis/google/rpc"
	"github.com/gogo/status"

	"github.com/grafana/loki/v3/pkg/logqlmodel"
	storage_errors "github.com/grafana/loki/v3/pkg/storage/errors"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/validation"
)

// Failure categories returned by ClassifyFailure: a bounded set, safe to use as
// a metric label.
const (
	FailureTimeout   = "timeout"
	FailureCanceled  = "canceled"
	FailureThrottled = "throttled"
	FailureLimit     = "limit"
	FailureSyntax    = "syntax"
	FailureBlocked   = "blocked"
	FailureUserError = "user_error"
	FailureInternal  = "internal"
	FailureUnknown   = "unknown"
)

// ClassifyFailure inspects a query error and returns a coarse category from the
// set above, plus a finer-grained reason. Sentinels are matched first; anything
// else is bucketed by the status ClientHTTPStatusAndError maps it to, so the
// classification and the status served to the client cannot drift.
func ClassifyFailure(err error) (category, reason string) {
	if err == nil {
		return "", ""
	}

	switch {
	case errors.Is(err, context.Canceled):
		return FailureCanceled, "client_canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return FailureTimeout, "query_timeout"
	case errors.Is(err, logqlmodel.ErrMaxQueryBytesRead):
		return FailureLimit, "max_query_bytes_read"
	case errors.Is(err, logqlmodel.ErrQuerierTooManyBytes):
		return FailureLimit, "querier_too_large"
	case errors.Is(err, logqlmodel.ErrIntervalLimit):
		return FailureLimit, "interval_limit"
	case errors.Is(err, logqlmodel.ErrLimit):
		return FailureLimit, "series_limit"
	case errors.Is(err, logqlmodel.ErrBlocked):
		return FailureBlocked, "blocked_by_policy"
	case errors.Is(err, logqlmodel.ErrParse), errors.Is(err, logqlmodel.ErrParseMatchers):
		return FailureSyntax, "parse"
	case errors.Is(err, logqlmodel.ErrPipeline):
		return FailureSyntax, "pipeline"
	case errors.Is(err, logqlmodel.ErrUnsupportedSyntaxForInstantQuery):
		return FailureSyntax, "unsupported_instant_query"
	case errors.Is(err, logqlmodel.ErrVariantsDisabled):
		return FailureSyntax, "variants_disabled"
	case errors.Is(err, logqlmodel.ErrMaxQueryParallelism):
		return FailureThrottled, "max_query_parallelism"
	// The two limits below are also matched by message in reasonForBadRequest,
	// which must return the same category and reason.
	case errors.Is(err, logqlmodel.ErrMaxQueryLength):
		return FailureLimit, "max_query_length"
	case errors.Is(err, logqlmodel.ErrMaxEntriesLimit):
		return FailureLimit, "max_entries"
	case errors.Is(err, user.ErrNoOrgID):
		return FailureUserError, "no_org_id"
	}

	status, _ := ClientHTTPStatusAndError(err)
	switch status {
	case StatusClientClosedRequest:
		return FailureCanceled, "client_canceled"
	case http.StatusGatewayTimeout:
		return FailureTimeout, "query_timeout"
	case http.StatusTooManyRequests:
		return FailureThrottled, "too_many_requests"
	case http.StatusRequestEntityTooLarge:
		return FailureLimit, "query_too_large"
	case http.StatusBadRequest:
		return reasonForBadRequest(err)
	default:
		if status/100 == 5 {
			return FailureInternal, "downstream_error"
		}
		if status/100 == 4 {
			return FailureUserError, "unknown"
		}
		return FailureUnknown, "unknown"
	}
}

// reasonForBadRequest refines a generic 400 into a category and reason by
// matching the stable (pre-format) prefix of a known limit message template. It
// is a fallback for the same limits raised on the queriers or in the v2 engine,
// where only the (status, body) pair survives the httpgrpc hop and the sentinel
// does not, so it must return what the sentinel cases above return.
func reasonForBadRequest(err error) (category, reason string) {
	msg := err.Error()
	switch {
	case matchesTemplate(msg, validation.ErrMaxEntriesLimit):
		return FailureLimit, "max_entries"
	case matchesTemplate(msg, validation.ErrQueryTooLong):
		return FailureLimit, "max_query_length"
	default:
		return FailureUserError, "bad_request"
	}
}

// matchesTemplate reports whether msg looks like an instance of the printf-style
// template tmpl, by looking for the stable text preceding its first format verb.
func matchesTemplate(msg, tmpl string) bool {
	prefix := prefixBeforeFormat(tmpl)
	return prefix != "" && strings.Contains(msg, prefix)
}

// prefixBeforeFormat returns the portion of a printf-style template before its
// first format verb. A template starting with a verb yields an empty string,
// which callers must not match on: it matches every message.
func prefixBeforeFormat(tmpl string) string {
	if i := strings.IndexByte(tmpl, '%'); i >= 0 {
		return strings.TrimRight(tmpl[:i], " ")
	}
	return tmpl
}

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
