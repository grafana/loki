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
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

type extensionOptions interface{}
type extensionHeaders func(headers map[string][]string, isObs bool) error
type extensionProgressListener func() ProgressListener

func WithProgress(progressListener ProgressListener) extensionProgressListener {
	return func() ProgressListener {
		return progressListener
	}
}

func setHeaderPrefix(key string, value string) extensionHeaders {
	return func(headers map[string][]string, isObs bool) error {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("set header %s with empty value", key)
		}
		setHeaders(headers, key, []string{value}, isObs)
		return nil
	}
}

// WithReqPaymentHeader sets header for requester-pays
func WithReqPaymentHeader(requester PayerType) extensionHeaders {
	return setHeaderPrefix(REQUEST_PAYER, string(requester))
}

func WithTrafficLimitHeader(trafficLimit int64) extensionHeaders {
	return setHeaderPrefix(TRAFFIC_LIMIT, strconv.FormatInt(trafficLimit, 10))
}

func WithCallbackHeader(callback string) extensionHeaders {
	return setHeaderPrefix(CALLBACK, string(callback))
}

func WithCustomHeader(key string, value string) extensionHeaders {
	return func(headers map[string][]string, isObs bool) error {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("set header %s with empty value", key)
		}
		headers[key] = []string{value}
		return nil
	}
}

func PreprocessCallbackInputToSHA256(callbackInput *CallbackInput) (output string, err error) {

	if callbackInput == nil {
		return "", fmt.Errorf("the parameter can not be nil")
	}

	if callbackInput.CallbackUrl == "" {
		return "", fmt.Errorf("the parameter [CallbackUrl] can not be empty")
	}

	if callbackInput.CallbackBody == "" {
		return "", fmt.Errorf("the parameter [CallbackBody] can not be empty")
	}

	callbackBuffer := bytes.NewBuffer([]byte{})
	callbackEncoder := json.NewEncoder(callbackBuffer)
	// 避免HTML字符转义
	callbackEncoder.SetEscapeHTML(false)
	err = callbackEncoder.Encode(callbackInput)
	if err != nil {
		return "", err
	}
	callbackVal := base64.StdEncoding.EncodeToString(removeEndNewlineCharacter(callbackBuffer))
	// 计算SHA256哈希
	hash := sha256.Sum256([]byte(callbackVal))

	// 将哈希转换为十六进制字符串
	return hex.EncodeToString(hash[:]), nil

}

// Encode函数会默认在json结尾增加换行符，导致base64转码结果与其他方式不一致，需要去掉末尾的换行符
func removeEndNewlineCharacter(callbackBuffer *bytes.Buffer) []byte {
	return callbackBuffer.Bytes()[:callbackBuffer.Len()-1]
}
