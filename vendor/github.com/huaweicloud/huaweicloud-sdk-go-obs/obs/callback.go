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
	"io"
)

type ICallbackReadCloser interface {
	setCallbackReadCloser(body io.ReadCloser)
}

func (output *PutObjectOutput) setCallbackReadCloser(body io.ReadCloser) {
	output.CallbackBody.data = body
}

func (output *CompleteMultipartUploadOutput) setCallbackReadCloser(body io.ReadCloser) {
	output.CallbackBody.data = body
}

// define CallbackBody
type CallbackBody struct {
	data io.ReadCloser
}

func (output CallbackBody) ReadCallbackBody(p []byte) (int, error) {
	if output.data == nil {
		return 0, errors.New("have no callback data")
	}
	return output.data.Read(p)
}

func (output CallbackBody) CloseCallbackBody() error {
	if output.data == nil {
		return errors.New("have no callback data")
	}
	return output.data.Close()
}
