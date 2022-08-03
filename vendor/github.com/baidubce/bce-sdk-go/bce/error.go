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

// error.go - define the error types for BCE

package bce

const (
	EACCESS_DENIED            = "AccessDenied"
	EINAPPROPRIATE_JSON       = "InappropriateJSON"
	EINTERNAL_ERROR           = "InternalError"
	EINVALID_ACCESS_KEY_ID    = "InvalidAccessKeyId"
	EINVALID_HTTP_AUTH_HEADER = "InvalidHTTPAuthHeader"
	EINVALID_HTTP_REQUEST     = "InvalidHTTPRequest"
	EINVALID_URI              = "InvalidURI"
	EMALFORMED_JSON           = "MalformedJSON"
	EINVALID_VERSION          = "InvalidVersion"
	EOPT_IN_REQUIRED          = "OptInRequired"
	EPRECONDITION_FAILED      = "PreconditionFailed"
	EREQUEST_EXPIRED          = "RequestExpired"
	ESIGNATURE_DOES_NOT_MATCH = "SignatureDoesNotMatch"
)

// BceError abstracts the error for BCE
type BceError interface {
	error
}

// BceClientError defines the error struct for the client when making request
type BceClientError struct{ Message string }

func (b *BceClientError) Error() string { return b.Message }

func NewBceClientError(msg string) *BceClientError { return &BceClientError{msg} }

// BceServiceError defines the error struct for the BCE service when receiving response
type BceServiceError struct {
	Code       string
	Message    string
	RequestId  string
	StatusCode int
}

func (b *BceServiceError) Error() string {
	ret := "[Code: " + b.Code
	ret += "; Message: " + b.Message
	ret += "; RequestId: " + b.RequestId + "]"
	return ret
}

func NewBceServiceError(code, msg, reqId string, status int) *BceServiceError {
	return &BceServiceError{code, msg, reqId, status}
}
