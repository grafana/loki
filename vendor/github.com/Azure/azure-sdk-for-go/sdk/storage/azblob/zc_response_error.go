//go:build go1.18
// +build go1.18

// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package azblob

import "net/http"

// ResponseError is a wrapper of error passed from service
type ResponseError interface {
	Error() string
	Unwrap() error
	RawResponse() *http.Response
	NonRetriable()
}
