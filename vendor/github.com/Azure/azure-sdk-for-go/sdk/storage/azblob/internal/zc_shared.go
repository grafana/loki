//go:build go1.16
// +build go1.16

// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package internal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/ioutil"
	"net/http"
	"strconv"
	"time"
)

// CtxWithHTTPHeaderKey is used as a context key for adding/retrieving http.Header.
type CtxWithHTTPHeaderKey struct{}

// CtxWithRetryOptionsKey is used as a context key for adding/retrieving RetryOptions.
type CtxWithRetryOptionsKey struct{}

type nopCloser struct {
	io.ReadSeeker
}

func (n nopCloser) Close() error {
	return nil
}

// NopCloser returns a ReadSeekCloser with a no-op close method wrapping the provided io.ReadSeeker.
func NopCloser(rs io.ReadSeeker) io.ReadSeekCloser {
	return nopCloser{rs}
}

// BodyDownloadPolicyOpValues is the struct containing the per-operation values
type BodyDownloadPolicyOpValues struct {
	Skip bool
}

func NewResponseError(inner error, resp *http.Response) error {
	return &ResponseError{inner: inner, resp: resp}
}

type ResponseError struct {
	inner error
	resp  *http.Response
}

// Error implements the error interface for type ResponseError.
func (e *ResponseError) Error() string {
	return e.inner.Error()
}

// Unwrap returns the inner error.
func (e *ResponseError) Unwrap() error {
	return e.inner
}

// RawResponse returns the HTTP response associated with this error.
func (e *ResponseError) RawResponse() *http.Response {
	return e.resp
}

// NonRetriable indicates this error is non-transient.
func (e *ResponseError) NonRetriable() {
	// marker method
}

// Delay waits for the duration to elapse or the context to be cancelled.
func Delay(ctx context.Context, delay time.Duration) error {
	select {
	case <-time.After(delay):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ErrNoBody is returned if the response didn't contain a body.
var ErrNoBody = errors.New("the response did not contain a body")

// GetJSON reads the response body into a raw JSON object.
// It returns ErrNoBody if there was no content.
func GetJSON(resp *http.Response) (map[string]interface{}, error) {
	body, err := ioutil.ReadAll(resp.Body)
	defer resp.Body.Close()
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return nil, ErrNoBody
	}
	// put the body back so it's available to others
	resp.Body = ioutil.NopCloser(bytes.NewReader(body))
	// unmarshall the body to get the value
	var jsonBody map[string]interface{}
	if err = json.Unmarshal(body, &jsonBody); err != nil {
		return nil, err
	}
	return jsonBody, nil
}

const HeaderRetryAfter = "Retry-After"

// RetryAfter returns non-zero if the response contains a Retry-After header value.
func RetryAfter(resp *http.Response) time.Duration {
	if resp == nil {
		return 0
	}
	ra := resp.Header.Get(HeaderRetryAfter)
	if ra == "" {
		return 0
	}
	// retry-after values are expressed in either number of
	// seconds or an HTTP-date indicating when to try again
	if retryAfter, _ := strconv.Atoi(ra); retryAfter > 0 {
		return time.Duration(retryAfter) * time.Second
	} else if t, err := time.Parse(time.RFC1123, ra); err == nil {
		return time.Until(t)
	}
	return 0
}

// HasStatusCode returns true if the Response's status code is one of the specified values.
func HasStatusCode(resp *http.Response, statusCodes ...int) bool {
	if resp == nil {
		return false
	}
	for _, sc := range statusCodes {
		if resp.StatusCode == sc {
			return true
		}
	}
	return false
}

const defaultScope = "/.default"

// EndpointToScope converts the provided URL endpoint to its default scope.
func EndpointToScope(endpoint string) string {
	if endpoint[len(endpoint)-1] != '/' {
		endpoint += "/"
	}
	return endpoint + defaultScope
}
