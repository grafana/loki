// Copyright 2019 Huawei Technologies Co.,Ltd.
// Licensed under the Apache License, Version 2.0 (the "License"); you may not use
// this file except in compliance with the License.  You may obtain a copy of the
// License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software distributed
// under the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR
// CONDITIONS OF ANY KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations under the License.

package obs

import (
	"io"
)

type IRepeatable interface {
	Reset() error
}

// IReadCloser defines interface with function: setReadCloser
type IReadCloser interface {
	setReadCloser(body io.ReadCloser)
}

func setHeaders(headers map[string][]string, header string, headerValue []string, isObs bool) {
	if isObs {
		header = HEADER_PREFIX_OBS + header
		headers[header] = headerValue
	} else {
		header = HEADER_PREFIX + header
		headers[header] = headerValue
	}
}

func setHeadersNext(headers map[string][]string, header string, headerNext string, headerValue []string, isObs bool) {
	if isObs {
		headers[header] = headerValue
	} else {
		headers[headerNext] = headerValue
	}
}

// IBaseModel defines interface for base response model
type IBaseModel interface {
	setStatusCode(statusCode int)

	setRequestID(requestID string)

	setResponseHeaders(responseHeaders map[string][]string)
}

// ISerializable defines interface with function: trans
type ISerializable interface {
	trans(isObs bool) (map[string]string, map[string][]string, interface{}, error)
}

// DefaultSerializable defines default serializable struct
type DefaultSerializable struct {
	params  map[string]string
	headers map[string][]string
	data    interface{}
}

func (input DefaultSerializable) trans(isObs bool) (map[string]string, map[string][]string, interface{}, error) {
	return input.params, input.headers, input.data, nil
}

var defaultSerializable = &DefaultSerializable{}

func newSubResourceSerialV2(subResource SubResourceType, value string) *DefaultSerializable {
	return &DefaultSerializable{map[string]string{string(subResource): value}, nil, nil}
}

func newSubResourceSerial(subResource SubResourceType) *DefaultSerializable {
	return &DefaultSerializable{map[string]string{string(subResource): ""}, nil, nil}
}

func trans(subResource SubResourceType, input interface{}) (params map[string]string, headers map[string][]string, data interface{}, err error) {
	params = map[string]string{string(subResource): ""}
	data, err = ConvertRequestToIoReader(input)
	return
}

func (baseModel *BaseModel) setStatusCode(statusCode int) {
	baseModel.StatusCode = statusCode
}

func (baseModel *BaseModel) setRequestID(requestID string) {
	baseModel.RequestId = requestID
}

func (baseModel *BaseModel) setResponseHeaders(responseHeaders map[string][]string) {
	baseModel.ResponseHeaders = responseHeaders
}

// GetEncryption gets the Encryption field value from SseKmsHeader
func (header SseKmsHeader) GetEncryption() string {
	if header.Encryption != "" {
		return header.Encryption
	}
	if !header.isObs {
		return DEFAULT_SSE_KMS_ENCRYPTION
	}
	return DEFAULT_SSE_KMS_ENCRYPTION_OBS
}

// GetKey gets the Key field value from SseKmsHeader
func (header SseKmsHeader) GetKey() string {
	return header.Key
}

// GetEncryption gets the Encryption field value from SseCHeader
func (header SseCHeader) GetEncryption() string {
	if header.Encryption != "" {
		return header.Encryption
	}
	return DEFAULT_SSE_C_ENCRYPTION
}

// GetKey gets the Key field value from SseCHeader
func (header SseCHeader) GetKey() string {
	return header.Key
}

// GetKeyMD5 gets the KeyMD5 field value from SseCHeader
func (header SseCHeader) GetKeyMD5() string {
	if header.KeyMD5 != "" {
		return header.KeyMD5
	}

	if ret, err := Base64Decode(header.GetKey()); err == nil {
		return Base64Md5(ret)
	}
	return ""
}

func setSseHeader(headers map[string][]string, sseHeader ISseHeader, sseCOnly bool, isObs bool) {
	if sseHeader != nil {
		if sseCHeader, ok := sseHeader.(SseCHeader); ok {
			setHeaders(headers, HEADER_SSEC_ENCRYPTION, []string{sseCHeader.GetEncryption()}, isObs)
			setHeaders(headers, HEADER_SSEC_KEY, []string{sseCHeader.GetKey()}, isObs)
			setHeaders(headers, HEADER_SSEC_KEY_MD5, []string{sseCHeader.GetKeyMD5()}, isObs)
		} else if sseKmsHeader, ok := sseHeader.(SseKmsHeader); !sseCOnly && ok {
			sseKmsHeader.isObs = isObs
			setHeaders(headers, HEADER_SSEKMS_ENCRYPTION, []string{sseKmsHeader.GetEncryption()}, isObs)
			if sseKmsHeader.GetKey() != "" {
				setHeadersNext(headers, HEADER_SSEKMS_KEY_OBS, HEADER_SSEKMS_KEY_AMZ, []string{sseKmsHeader.GetKey()}, isObs)
			}
		}
	}
}
