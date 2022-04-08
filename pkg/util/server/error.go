package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

type ErrorResponseBody struct {
	Code    int    `json:"code"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

func NotFoundHandler(w http.ResponseWriter, r *http.Request) {
	JSONError(w, 404, "not found")
}

func JSONError(w http.ResponseWriter, code int, message string, args ...interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(ErrorResponseBody{
		Code:    code,
		Status:  "error",
		Message: fmt.Sprintf(message, args...),
	})
}

// WriteError write a go error with the correct status code.
func WriteError(err error, w http.ResponseWriter) {
	var (
		queryErr storage_errors.QueryError
		promErr  promql.ErrStorage
	)

	me, ok := err.(util.MultiError)
	if ok && me.Is(context.Canceled) {
		JSONError(w, StatusClientClosedRequest, ErrClientCanceled)
		return
	}
	if ok && me.IsDeadlineExceeded() {
		JSONError(w, http.StatusGatewayTimeout, ErrDeadlineExceeded)
		return
	}

	s, isRPC := status.FromError(err)
	switch {
	case errors.Is(err, context.Canceled) ||
		(errors.As(err, &promErr) && errors.Is(promErr.Err, context.Canceled)):
		JSONError(w, StatusClientClosedRequest, ErrClientCanceled)
	case errors.Is(err, context.DeadlineExceeded) ||
		(isRPC && s.Code() == codes.DeadlineExceeded):
		JSONError(w, http.StatusGatewayTimeout, ErrDeadlineExceeded)
	case errors.As(err, &queryErr),
		errors.Is(err, logqlmodel.ErrLimit) || errors.Is(err, logqlmodel.ErrParse) || errors.Is(err, logqlmodel.ErrPipeline),
		errors.Is(err, user.ErrNoOrgID):
		JSONError(w, http.StatusBadRequest, err.Error())
	default:
		if grpcErr, ok := httpgrpc.HTTPResponseFromError(err); ok {
			JSONError(w, int(grpcErr.Code), string(grpcErr.Body))
			return
		}
		JSONError(w, http.StatusInternalServerError, err.Error())
	}
}
