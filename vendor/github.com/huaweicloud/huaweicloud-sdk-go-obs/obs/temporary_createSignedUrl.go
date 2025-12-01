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
	"errors"
	"fmt"
)

// CreateSignedUrl creates signed url with the specified CreateSignedUrlInput, and returns the CreateSignedUrlOutput and error
func (obsClient ObsClient) CreateSignedUrl(input *CreateSignedUrlInput, extensions ...extensionOptions) (output *CreateSignedUrlOutput, err error) {
	if input == nil {
		return nil, errors.New("CreateSignedUrlInput is nil")
	}

	params := make(map[string]string, len(input.QueryParams))
	for key, value := range input.QueryParams {
		params[key] = value
	}

	if input.SubResource != "" {
		params[string(input.SubResource)] = ""
	}

	headers := make(map[string][]string, len(input.Headers))
	for key, value := range input.Headers {
		headers[key] = []string{value}
	}

	for _, extension := range extensions {
		if extensionHeader, ok := extension.(extensionHeaders); ok {
			_err := extensionHeader(headers, obsClient.conf.signature == SignatureObs)
			if _err != nil {
				doLog(LEVEL_INFO, fmt.Sprintf("set header with error: %v", _err))
			}
		} else {
			doLog(LEVEL_INFO, "Unsupported extensionOptions")
		}
	}

	if input.Expires <= 0 {
		input.Expires = 300
	}

	requestURL, err := obsClient.doAuthTemporary(string(input.Method), input.Bucket, input.Key, input.Policy, params, headers, int64(input.Expires))
	if err != nil {
		return nil, err
	}

	output = &CreateSignedUrlOutput{
		SignedUrl:                  requestURL,
		ActualSignedRequestHeaders: headers,
	}
	return
}
