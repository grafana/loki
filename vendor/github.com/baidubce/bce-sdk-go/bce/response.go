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

// response.go - defines the common BCE services response

package bce

import (
	"bytes"
	"encoding/json"
	"io"
	"io/ioutil"
	"strings"
	"time"

	"github.com/baidubce/bce-sdk-go/http"
)

// BceResponse defines the response structure for receiving BCE services response.
type BceResponse struct {
	statusCode   int
	statusText   string
	requestId    string
	debugId      string
	response     *http.Response
	serviceError *BceServiceError
}

func (r *BceResponse) IsFail() bool {
	return r.response.StatusCode() >= 400
}

func (r *BceResponse) StatusCode() int {
	return r.statusCode
}

func (r *BceResponse) StatusText() string {
	return r.statusText
}

func (r *BceResponse) RequestId() string {
	return r.requestId
}

func (r *BceResponse) DebugId() string {
	return r.debugId
}

func (r *BceResponse) Header(key string) string {
	return r.response.GetHeader(key)
}

func (r *BceResponse) Headers() map[string]string {
	return r.response.GetHeaders()
}

func (r *BceResponse) Body() io.ReadCloser {
	return r.response.Body()
}

func (r *BceResponse) SetHttpResponse(response *http.Response) {
	r.response = response
}

func (r *BceResponse) ElapsedTime() time.Duration {
	return r.response.ElapsedTime()
}

func (r *BceResponse) ServiceError() *BceServiceError {
	return r.serviceError
}

func (r *BceResponse) ParseResponse() {
	r.statusCode = r.response.StatusCode()
	r.statusText = r.response.StatusText()
	r.requestId = r.response.GetHeader(http.BCE_REQUEST_ID)
	r.debugId = r.response.GetHeader(http.BCE_DEBUG_ID)
	if r.IsFail() {
		r.serviceError = NewBceServiceError("", r.statusText, r.requestId, r.statusCode)

		// First try to read the error `Code' and `Message' from body
		rawBody, _ := ioutil.ReadAll(r.Body())
		defer r.Body().Close()
		if len(rawBody) != 0 {
			jsonDecoder := json.NewDecoder(bytes.NewBuffer(rawBody))
			if err := jsonDecoder.Decode(r.serviceError); err != nil {
				r.serviceError = NewBceServiceError(
					EMALFORMED_JSON,
					"Service json error message decode failed",
					r.requestId,
					r.statusCode)
			}
			return
		}

		// Then guess the `Message' from by the return status code
		switch r.statusCode {
		case 400:
			r.serviceError.Code = EINVALID_HTTP_REQUEST
		case 403:
			r.serviceError.Code = EACCESS_DENIED
		case 412:
			r.serviceError.Code = EPRECONDITION_FAILED
		case 500:
			r.serviceError.Code = EINTERNAL_ERROR
		default:
			words := strings.Split(r.statusText, " ")
			r.serviceError.Code = strings.Join(words[1:], "")
		}
	}
}

func (r *BceResponse) ParseJsonBody(result interface{}) error {
	defer r.Body().Close()
	jsonDecoder := json.NewDecoder(r.Body())
	return jsonDecoder.Decode(result)
}
