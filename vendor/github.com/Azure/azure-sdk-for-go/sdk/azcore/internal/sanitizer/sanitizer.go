// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package sanitizer

import (
	"net/http"
	"net/url"
	"strings"
)

const redactedValue = "REDACTED"

// Sanitizer redacts header and URL query parameter values not in its allow lists.
// Create instances with New.
type Sanitizer struct {
	// allowedHeaders contains lowercase header names whose values are preserved.
	allowedHeaders map[string]struct{}
	// allowedQP contains lowercase query parameter names whose values are preserved.
	allowedQP map[string]struct{}
}

// Options configures additions to the default sanitization allow lists.
type Options struct {
	// AllowedHeaders contains additional header names whose values are preserved.
	// Names are matched case-insensitively.
	AllowedHeaders []string
	// AllowedQueryParams contains additional query parameter names whose values are preserved.
	// Names are matched case-insensitively.
	AllowedQueryParams []string
}

// New creates a Sanitizer using the default allow lists and any additions in o.
func New(o Options) *Sanitizer {
	// construct default hash set of allowed headers
	allowedHeaders := map[string]struct{}{
		"accept":                        {},
		"azure-deprecating":             {},
		"cache-control":                 {},
		"connection":                    {},
		"content-length":                {},
		"content-type":                  {},
		"date":                          {},
		"etag":                          {},
		"expires":                       {},
		"if-match":                      {},
		"if-modified-since":             {},
		"if-none-match":                 {},
		"if-unmodified-since":           {},
		"last-modified":                 {},
		"ms-cv":                         {},
		"pragma":                        {},
		"request-id":                    {},
		"retry-after":                   {},
		"server":                        {},
		"traceparent":                   {},
		"transfer-encoding":             {},
		"user-agent":                    {},
		"www-authenticate":              {},
		"x-ms-request-id":               {},
		"x-ms-client-request-id":        {},
		"x-ms-return-client-request-id": {},
	}
	// add any caller-specified allowed headers to the set
	for _, ah := range o.AllowedHeaders {
		allowedHeaders[strings.ToLower(ah)] = struct{}{}
	}
	// now do the same thing for query params
	allowedQP := getAllowedQueryParams(o.AllowedQueryParams)
	return &Sanitizer{
		allowedHeaders: allowedHeaders,
		allowedQP:      allowedQP,
	}
}

// GetSanitizedHeader returns the sanitized value for key based on the allow list.
func (s *Sanitizer) GetSanitizedHeader(key string, header http.Header) string {
	if _, ok := s.allowedHeaders[strings.ToLower(key)]; ok {
		if values := header[key]; len(values) > 0 {
			return values[0]
		}
		return header.Get(key)
	}
	return redactedValue
}

// GetSanitizedURL returns a sanitized string for the provided url.URL
func (s *Sanitizer) GetSanitizedURL(u url.URL) string {
	// redact applicable query params
	qp := u.Query()
	for k := range qp {
		if _, ok := s.allowedQP[strings.ToLower(k)]; !ok {
			qp.Set(k, redactedValue)
		}
	}
	u.RawQuery = qp.Encode()
	return u.String()
}

// GetSanitizedURLString returns str with the query params of u redacted.
// If str doesn't contain u, then str is returned unchanged.
func (s *Sanitizer) GetSanitizedURLString(str string, u *url.URL) string {
	if u.RawQuery == "" {
		// the URL doesn't have any query params, so nothing to redact
		return str
	}

	urlIndex := strings.Index(strings.ToLower(str), strings.ToLower(u.String()))
	if urlIndex < 0 {
		// the URL isn't in the string, so nothing to redact
		return str
	}

	// replace the unsanitized URL in the error message with the sanitized version
	sanitizedURL := s.GetSanitizedURL(*u)
	str = str[:urlIndex] + sanitizedURL + str[urlIndex+len(u.String()):]

	return str
}

// getAllowedQueryParams merges the default set of allowed query parameters
// with a custom set (usually comes from client options).
func getAllowedQueryParams(customAllowedQP []string) map[string]struct{} {
	allowedQP := map[string]struct{}{
		"api-version": {},
	}
	for _, qp := range customAllowedQP {
		allowedQP[strings.ToLower(qp)] = struct{}{}
	}
	return allowedQP
}
