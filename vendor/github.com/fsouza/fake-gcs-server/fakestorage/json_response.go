package fakestorage

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"syscall"

	"github.com/fsouza/fake-gcs-server/internal/backend"
)

type jsonResponse struct {
	status       int
	header       http.Header
	data         any
	errorMessage string
}

type jsonHandler = func(r *http.Request) jsonResponse

func jsonToHTTPHandler(h jsonHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := h(r)
		w.Header().Set("Content-Type", "application/json")
		for name, values := range resp.header {
			for _, value := range values {
				w.Header().Add(name, value)
			}
		}

		status := resp.getStatus()
		var data any
		if status > 399 {
			data = newErrorResponse(status, resp.getErrorMessage(status), resp.getErrorList(status))
		} else {
			data = resp.data
		}

		w.WriteHeader(status)
		json.NewEncoder(w).Encode(data)
	}
}

func (r *jsonResponse) getStatus() int {
	if r.status > 0 {
		return r.status
	}
	if r.errorMessage != "" {
		return http.StatusInternalServerError
	}
	return http.StatusOK
}

func (r *jsonResponse) getErrorMessage(status int) string {
	if r.errorMessage != "" {
		return r.errorMessage
	}
	return http.StatusText(status)
}

func (r *jsonResponse) getErrorList(status int) []apiError {
	if status == http.StatusOK {
		return nil
	} else {
		return []apiError{{
			Domain:  "global",
			Reason:  gcsErrorReason(status),
			Message: r.getErrorMessage(status),
		}}
	}
}

// gcsErrorReason maps an HTTP status code to the reason string used by the GCS
// JSON API error model. These reasons are domain-specific enums distinct from
// the HTTP status text, e.g. 412 is "conditionNotMet" rather than "Precondition
// Failed" and 404 is "notFound" rather than "Not Found".
// See https://cloud.google.com/storage/docs/json_api/v1/status-codes
func gcsErrorReason(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "invalid"
	case http.StatusForbidden:
		return "forbidden"
	case http.StatusNotFound:
		return "notFound"
	case http.StatusConflict:
		return "conflict"
	case http.StatusPreconditionFailed:
		return "conditionNotMet"
	case http.StatusTooManyRequests:
		return "rateLimitExceeded"
	case http.StatusInternalServerError:
		return "internalError"
	case http.StatusServiceUnavailable:
		return "backendError"
	default:
		return http.StatusText(status)
	}
}

func errToJsonResponse(err error) jsonResponse {
	status := 0
	var pathError *os.PathError
	if errors.As(err, &pathError) && pathError.Err == syscall.ENAMETOOLONG {
		status = http.StatusBadRequest
	}
	if err == backend.PreConditionFailed {
		status = http.StatusPreconditionFailed
	}
	return jsonResponse{errorMessage: err.Error(), status: status}
}
