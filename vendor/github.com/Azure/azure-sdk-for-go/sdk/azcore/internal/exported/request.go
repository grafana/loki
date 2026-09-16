// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package exported

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"reflect"
	"strconv"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/internal/shared"
)

// Base64Encoding is usesd to specify which base-64 encoder/decoder to use when
// encoding/decoding a slice of bytes to/from a string.
// Exported as runtime.Base64Encoding
type Base64Encoding int

const (
	// Base64StdFormat uses base64.StdEncoding for encoding and decoding payloads.
	Base64StdFormat Base64Encoding = 0

	// Base64URLFormat uses base64.RawURLEncoding for encoding and decoding payloads.
	Base64URLFormat Base64Encoding = 1
)

// EncodeByteArray will base-64 encode the byte slice v.
// Exported as runtime.EncodeByteArray()
func EncodeByteArray(v []byte, format Base64Encoding) string {
	if format == Base64URLFormat {
		return base64.RawURLEncoding.EncodeToString(v)
	}
	return base64.StdEncoding.EncodeToString(v)
}

// Request is an abstraction over the creation of an HTTP request as it passes through the pipeline.
// Don't use this type directly, use NewRequest() instead.
// Exported as policy.Request.
type Request struct {
	req      *http.Request
	body     io.ReadSeekCloser
	policies []Policy
	values   opValues
}

type opValues map[reflect.Type]any

// Set adds/changes a value
func (ov opValues) set(value any) {
	ov[reflect.TypeOf(value)] = value
}

// Get looks for a value set by SetValue first
func (ov opValues) get(value any) bool {
	v, ok := ov[reflect.ValueOf(value).Elem().Type()]
	if ok {
		reflect.ValueOf(value).Elem().Set(reflect.ValueOf(v))
	}
	return ok
}

// NewRequestFromRequest creates a new policy.Request with an existing *http.Request
// Exported as runtime.NewRequestFromRequest().
func NewRequestFromRequest(req *http.Request) (*Request, error) {
	// populate values so that the same instance is propagated across policies
	policyReq := &Request{req: req, values: opValues{}}

	if req.Body != nil {
		// we can avoid a body copy here if the underlying stream is already a
		// ReadSeekCloser.
		readSeekCloser, isReadSeekCloser := req.Body.(io.ReadSeekCloser)

		if !isReadSeekCloser {
			// since this is an already populated http.Request we want to copy
			// over its body, if it has one.
			bodyBytes, err := io.ReadAll(req.Body)

			if err != nil {
				return nil, err
			}

			if err := req.Body.Close(); err != nil {
				return nil, err
			}

			readSeekCloser = NopCloser(bytes.NewReader(bodyBytes))
		}

		// SetBody also takes care of updating the http.Request's body
		// as well, so they should stay in-sync from this point.
		if err := policyReq.SetBody(readSeekCloser, req.Header.Get("Content-Type")); err != nil {
			return nil, err
		}
	}

	return policyReq, nil
}

// NewRequest creates a new Request with the specified input.
// Exported as runtime.NewRequest().
func NewRequest(ctx context.Context, httpMethod string, endpoint string) (*Request, error) {
	req, err := http.NewRequestWithContext(ctx, httpMethod, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if req.URL.Host == "" {
		return nil, errors.New("no Host in request URL")
	}
	if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
		return nil, fmt.Errorf("unsupported protocol scheme %s", req.URL.Scheme)
	}
	// populate values so that the same instance is propagated across policies
	return &Request{req: req, values: opValues{}}, nil
}

// NewRequestForNextLink creates a new policy.Request with the specified input.
// This helper is for use in paged operations that use the "next link" pattern.
//   - endpoint is the service endpoint (e.g. https://contoso.com)
//   - nextLink the absolute or relative URL to the next page of items
//
// When nextLink is an absolute URL, its value is used to create the request to the next page.
// If nextLink is a relative URL, it will be joined with the specified endpoint.
//
// Exported as runtime.NewRequestForNextLink().
func NewRequestForNextLink(ctx context.Context, httpMethod, endpoint, nextLink string) (*Request, error) {
	nextLink, err := EncodeQueryParams(nextLink, true)
	if err != nil {
		return nil, err
	}
	if !hasScheme(nextLink) {
		nextLink = JoinPaths(endpoint, nextLink)
	}
	return NewRequest(ctx, httpMethod, nextLink)
}

// Body returns the original body specified when the Request was created.
func (req *Request) Body() io.ReadSeekCloser {
	return req.body
}

// Raw returns the underlying HTTP request.
func (req *Request) Raw() *http.Request {
	return req.req
}

// Next calls the next policy in the pipeline.
// If there are no more policies, nil and an error are returned.
// This method is intended to be called from pipeline policies.
// To send a request through a pipeline call Pipeline.Do().
func (req *Request) Next() (*http.Response, error) {
	if len(req.policies) == 0 {
		return nil, errors.New("no more policies")
	}
	nextPolicy := req.policies[0]
	nextReq := *req
	nextReq.policies = nextReq.policies[1:]
	return nextPolicy.Do(&nextReq)
}

// SetOperationValue adds/changes a mutable key/value associated with a single operation.
func (req *Request) SetOperationValue(value any) {
	if req.values == nil {
		req.values = opValues{}
	}
	req.values.set(value)
}

// OperationValue looks for a value set by SetOperationValue().
func (req *Request) OperationValue(value any) bool {
	if req.values == nil {
		return false
	}
	return req.values.get(value)
}

// SetBody sets the specified ReadSeekCloser as the HTTP request body, and sets Content-Type and Content-Length
// accordingly. If the ReadSeekCloser is nil or empty, Content-Length won't be set. If contentType is "",
// Content-Type won't be set, and if it was set, will be deleted.
// Use streaming.NopCloser to turn an io.ReadSeeker into an io.ReadSeekCloser.
func (req *Request) SetBody(body io.ReadSeekCloser, contentType string) error {
	// clobber the existing Content-Type to preserve behavior
	return SetBody(req, body, contentType, true)
}

// RewindBody seeks the request's Body stream back to the beginning so it can be resent when retrying an operation.
func (req *Request) RewindBody() error {
	if req.body != nil {
		// Reset the stream back to the beginning and restore the body
		_, err := req.body.Seek(0, io.SeekStart)
		req.req.Body = req.body
		return err
	}
	return nil
}

// Close closes the request body.
func (req *Request) Close() error {
	if req.body == nil {
		return nil
	}
	return req.body.Close()
}

// Clone returns a deep copy of the request with its context changed to ctx.
func (req *Request) Clone(ctx context.Context) *Request {
	r2 := *req
	r2.req = req.req.Clone(ctx)
	return &r2
}

// WithContext returns a shallow copy of the request with its context changed to ctx.
func (req *Request) WithContext(ctx context.Context) *Request {
	r2 := new(Request)
	*r2 = *req
	r2.req = r2.req.WithContext(ctx)
	return r2
}

// not exported but dependent on Request

// PolicyFunc is a type that implements the Policy interface.
// Use this type when implementing a stateless policy as a first-class function.
type PolicyFunc func(*Request) (*http.Response, error)

// Do implements the Policy interface on policyFunc.
func (pf PolicyFunc) Do(req *Request) (*http.Response, error) {
	return pf(req)
}

// SetBody sets the specified ReadSeekCloser as the HTTP request body, and sets Content-Type and Content-Length accordingly.
//   - req is the request to modify
//   - body is the request body; if nil or empty, Content-Length won't be set
//   - contentType is the value for the Content-Type header; if empty, Content-Type will be deleted
//   - clobberContentType when true, will overwrite the existing value of Content-Type with contentType
func SetBody(req *Request, body io.ReadSeekCloser, contentType string, clobberContentType bool) error {
	var err error
	var size int64
	if body != nil {
		size, err = body.Seek(0, io.SeekEnd) // Seek to the end to get the stream's size
		if err != nil {
			return err
		}
	}
	if size == 0 {
		// treat an empty stream the same as a nil one: assign req a nil body
		body = nil
		// RFC 9110 specifies a client shouldn't set Content-Length on a request containing no content
		// (Del is a no-op when the header has no value)
		req.req.Header.Del(shared.HeaderContentLength)
	} else {
		_, err = body.Seek(0, io.SeekStart)
		if err != nil {
			return err
		}
		req.req.Header.Set(shared.HeaderContentLength, strconv.FormatInt(size, 10))
		req.Raw().GetBody = func() (io.ReadCloser, error) {
			_, err := body.Seek(0, io.SeekStart) // Seek back to the beginning of the stream
			return body, err
		}
	}
	// keep a copy of the body argument.  this is to handle cases
	// where req.Body is replaced, e.g. httputil.DumpRequest and friends.
	req.body = body
	req.req.Body = body
	req.req.ContentLength = size
	if contentType == "" {
		// Del is a no-op when the header has no value
		req.req.Header.Del(shared.HeaderContentType)
	} else if req.req.Header.Get(shared.HeaderContentType) == "" || clobberContentType {
		req.req.Header.Set(shared.HeaderContentType, contentType)
	}
	return nil
}

// JoinPaths concatenates multiple URL path segments into one path,
// inserting path separation characters as required. JoinPaths will preserve
// query parameters in the root path.
// Exported as runtime.JoinPaths
func JoinPaths(root string, paths ...string) string {
	if len(paths) == 0 {
		return root
	}

	qps := ""
	if strings.Contains(root, "?") {
		splitPath := strings.Split(root, "?")
		root, qps = splitPath[0], splitPath[1]
	}

	p := path.Join(paths...)
	// path.Join will remove any trailing slashes.
	// if one was provided, preserve it.
	if strings.HasSuffix(paths[len(paths)-1], "/") && !strings.HasSuffix(p, "/") {
		p += "/"
	}

	if qps != "" {
		if strings.Contains(p, "?") {
			p = p + "&" + qps
		} else {
			p = p + "?" + qps
		}
	}

	if strings.HasSuffix(root, "/") && strings.HasPrefix(p, "/") {
		root = root[:len(root)-1]
	} else if !strings.HasSuffix(root, "/") && !strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "?") {
		p = "/" + p
	}
	return root + p
}

// EncodeQueryParams will parse and encode any query parameters in the specified URL.
// Any semicolons will automatically be escaped.
// When encodeSpaces is true all spaces that were form-encoded to + are replaced with %20.
// Exported as runtime.EncodeQueryParams
func EncodeQueryParams(u string, encodeSpaces bool) (string, error) {
	before, after, found := strings.Cut(u, "?")
	if !found {
		return u, nil
	}
	// starting in Go 1.17, url.ParseQuery will reject semicolons in query params.
	// so, we must escape them first. note that this assumes that semicolons aren't
	// being used as query param separators which is per the current RFC.
	// for more info:
	// https://github.com/golang/go/issues/25192
	// https://github.com/golang/go/issues/50034
	qp, err := url.ParseQuery(strings.ReplaceAll(after, ";", "%3B"))
	if err != nil {
		return "", err
	}
	encoded := qp.Encode()
	if encodeSpaces {
		encoded = strings.ReplaceAll(encoded, "+", "%20")
	}
	return before + "?" + encoded, nil
}

// hasScheme reports whether s begins with a URI scheme, i.e. is an absolute URI.
// Per RFC 3986: scheme = ALPHA *( ALPHA / DIGIT / "+" / "-" / "." ) ":".
// This avoids the allocations of url.Parse when we only need to know if the URI is absolute.
func hasScheme(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
			// always valid
		case c == ':':
			return i > 0
		case i == 0:
			return false
		case c >= '0' && c <= '9', c == '+', c == '-', c == '.':
			// valid after the first character
		default:
			return false
		}
	}
	return false
}
