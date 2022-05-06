/*
 * Copyright 2017 Baidu, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file
 * except in compliance with the License. You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software distributed under the
 * License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
 * either express or implied. See the License for the specific language governing permissions
 * and limitations under the License.
 */

// response.go - the custom HTTP response for BCE

package http

import (
	"io"
	"net/http"
	"time"
)

// Response defines the general http response structure for accessing the BCE services.
type Response struct {
	httpResponse *http.Response // the standard libaray http.Response object
	elapsedTime  time.Duration  // elapsed time just from sending request to receiving response
}

func (r *Response) StatusText() string {
	return r.httpResponse.Status
}

func (r *Response) StatusCode() int {
	return r.httpResponse.StatusCode
}

func (r *Response) Protocol() string {
	return r.httpResponse.Proto
}

func (r *Response) HttpResponse() *http.Response {
	return r.httpResponse
}

func (r *Response) SetHttpResponse(response *http.Response) {
	r.httpResponse = response
}

func (r *Response) ElapsedTime() time.Duration {
	return r.elapsedTime
}

func (r *Response) GetHeader(name string) string {
	return r.httpResponse.Header.Get(name)
}

func (r *Response) GetHeaders() map[string]string {
	header := r.httpResponse.Header
	ret := make(map[string]string, len(header))
	for k, v := range header {
		ret[k] = v[0]
	}
	return ret
}

func (r *Response) ContentLength() int64 {
	return r.httpResponse.ContentLength
}

func (r *Response) Body() io.ReadCloser {
	return r.httpResponse.Body
}
