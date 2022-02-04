// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package azblob

import "net/http"

type ResponseError interface {
	Error() string
	Unwrap() error
	RawResponse() *http.Response
	NonRetriable()
}
