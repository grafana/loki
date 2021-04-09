package ingester

import (
	"fmt"
	"net/http"

	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/weaveworks/common/httpgrpc"
)

type validationError struct {
	err       error // underlying error
	errorType string
	code      int
	noReport  bool // if true, error will be counted but not reported to caller
	labels    labels.Labels
}

func makeLimitError(errorType string, err error) error {
	return &validationError{
		errorType: errorType,
		err:       err,
		code:      http.StatusBadRequest,
	}
}

func makeNoReportError(errorType string) error {
	return &validationError{
		errorType: errorType,
		noReport:  true,
	}
}

func makeMetricValidationError(errorType string, labels labels.Labels, err error) error {
	return &validationError{
		errorType: errorType,
		err:       err,
		code:      http.StatusBadRequest,
		labels:    labels,
	}
}

func makeMetricLimitError(errorType string, labels labels.Labels, err error) error {
	return &validationError{
		errorType: errorType,
		err:       err,
		code:      http.StatusBadRequest,
		labels:    labels,
	}
}

func (e *validationError) Error() string {
	if e.err == nil {
		return e.errorType
	}
	if e.labels == nil {
		return e.err.Error()
	}
	return fmt.Sprintf("%s for series %s", e.err.Error(), e.labels.String())
}

// returns a HTTP gRPC error than is correctly forwarded over gRPC, with no reference to `e` retained.
func grpcForwardableError(userID string, code int, e error) error {
	return httpgrpc.ErrorFromHTTPResponse(&httpgrpc.HTTPResponse{
		Code: int32(code),
		Body: []byte(wrapWithUser(e, userID).Error()),
	})
}

// wrapWithUser prepends the user to the error. It does not retain a reference to err.
func wrapWithUser(err error, userID string) error {
	return fmt.Errorf("user=%s: %s", userID, err)
}
