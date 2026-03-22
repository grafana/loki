package cloudflare

import (
	"fmt"
	"net/http"
	"strings"
)

// Error messages
const (
	errEmptyCredentials          = "invalid credentials: key & email must not be empty"
	errEmptyAPIToken             = "invalid credentials: API Token must not be empty"
	errMakeRequestError          = "error from makeRequest"
	errUnmarshalError            = "error unmarshalling the JSON response"
	errUnmarshalErrorBody        = "error unmarshalling the JSON response error body"
	errRequestNotSuccessful      = "error reported by API"
	errMissingAccountID          = "account ID is empty and must be provided"
	errOperationStillRunning     = "bulk operation did not finish before timeout"
	errOperationUnexpectedStatus = "bulk operation returned an unexpected status"
	errResultInfo                = "incorrect pagination info (result_info) in responses"
	errManualPagination          = "unexpected pagination options passed to functions that handle pagination automatically"
)

// APIRequestError is a type of error raised by API calls made by this library.
type APIRequestError struct {
	StatusCode int
	Errors     []ResponseInfo
}

func (e APIRequestError) Error() string {
	errString := ""
	errString += fmt.Sprintf("HTTP status %d", e.StatusCode)

	if len(e.Errors) > 0 {
		errString += ": "
	}

	errMessages := []string{}
	for _, err := range e.Errors {
		m := ""
		if err.Message != "" {
			m += err.Message
		}

		if err.Code != 0 {
			m += fmt.Sprintf(" (%d)", err.Code)
		}

		errMessages = append(errMessages, m)
	}

	return errString + strings.Join(errMessages, ", ")
}

// HTTPStatusCode exposes the HTTP status from the error response encountered.
func (e APIRequestError) HTTPStatusCode() int {
	return e.StatusCode
}

// ErrorMessages exposes the error messages as a slice of strings from the error
// response encountered.
func (e *APIRequestError) ErrorMessages() []string {
	messages := []string{}

	for _, e := range e.Errors {
		messages = append(messages, e.Message)
	}

	return messages
}

// InternalErrorCodes exposes the internal error codes as a slice of int from
// the error response encountered.
func (e *APIRequestError) InternalErrorCodes() []int {
	ec := []int{}

	for _, e := range e.Errors {
		ec = append(ec, e.Code)
	}

	return ec
}

// ServiceError returns a boolean whether or not the raised error was caused by
// an internal service.
func (e *APIRequestError) ServiceError() bool {
	return e.StatusCode >= http.StatusInternalServerError &&
		e.StatusCode < 600
}

// ClientError returns a boolean whether or not the raised error was caused by
// something client side.
func (e *APIRequestError) ClientError() bool {
	return e.StatusCode >= http.StatusBadRequest &&
		e.StatusCode < http.StatusInternalServerError
}

// ClientRateLimited returns a boolean whether or not the raised error was
// caused by too many requests from the client.
func (e *APIRequestError) ClientRateLimited() bool {
	return e.StatusCode == http.StatusTooManyRequests
}

// InternalErrorCodeIs returns a boolean whether or not the desired internal
// error code is present in `e.InternalErrorCodes`.
func (e *APIRequestError) InternalErrorCodeIs(code int) bool {
	for _, errCode := range e.InternalErrorCodes() {
		if errCode == code {
			return true
		}
	}

	return false
}

// ErrorMessageContains returns a boolean whether or not a substring exists in
// any of the `e.ErrorMessages` slice entries.
func (e *APIRequestError) ErrorMessageContains(s string) bool {
	for _, errMsg := range e.ErrorMessages() {
		if strings.Contains(errMsg, s) {
			return true
		}
	}
	return false
}
