// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package exthttp

import (
	"net/http"
	"strconv"
	"time"

	"github.com/pkg/errors"
)

const (
	ContentLengthHeader = "Content-Length"
	LastModifiedHeader  = "Last-Modified"
)

// ParseContentLength returns the content length (in bytes) parsed from the Content-Length
// HTTP header in input.
func ParseContentLength(m http.Header) (int64, error) {
	v, ok := m[ContentLengthHeader]
	if !ok {
		return 0, errors.Errorf("%s header not found", ContentLengthHeader)
	}

	if len(v) == 0 {
		return 0, errors.Errorf("%s header has no values", ContentLengthHeader)
	}

	ret, err := strconv.ParseInt(v[0], 10, 64)
	if err != nil {
		return 0, errors.Wrapf(err, "convert %s", ContentLengthHeader)
	}

	return ret, nil
}

// ParseLastModified returns the timestamp parsed from the Last-Modified
// HTTP header in input.
// Passing an second parameter, named f, to specify the time format.
// If f is empty then RFC3339 will be used as default format.
func ParseLastModified(m http.Header, f string) (time.Time, error) {
	const defaultFormat = time.RFC3339

	v, ok := m[LastModifiedHeader]
	if !ok {
		return time.Time{}, errors.Errorf("%s header not found", LastModifiedHeader)
	}

	if len(v) == 0 {
		return time.Time{}, errors.Errorf("%s header has no values", LastModifiedHeader)
	}

	if f == "" {
		f = defaultFormat
	}

	mod, err := time.Parse(f, v[0])
	if err != nil {
		return time.Time{}, errors.Wrapf(err, "parse %s", LastModifiedHeader)
	}

	return mod, nil
}
