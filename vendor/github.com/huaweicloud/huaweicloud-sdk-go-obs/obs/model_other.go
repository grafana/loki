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
	"net/http"
)

// CreateSignedUrlInput is the input parameter of CreateSignedUrl function
type CreateSignedUrlInput struct {
	Method      HttpMethodType
	Bucket      string
	Key         string
	Policy      string
	SubResource SubResourceType
	Expires     int
	Headers     map[string]string
	QueryParams map[string]string
}

// CreateSignedUrlOutput is the result of CreateSignedUrl function
type CreateSignedUrlOutput struct {
	SignedUrl                  string
	ActualSignedRequestHeaders http.Header
}

// ConditionRange the specifying ranges in the conditions
type ConditionRange struct {
	RangeName string
	Lower     int64
	Upper     int64
}

// CreateBrowserBasedSignatureInput is the input parameter of CreateBrowserBasedSignature function.
type CreateBrowserBasedSignatureInput struct {
	Bucket      string
	Key         string
	Expires     int
	FormParams  map[string]string
	RangeParams []ConditionRange
}

// CreateBrowserBasedSignatureOutput is the result of CreateBrowserBasedSignature function.
type CreateBrowserBasedSignatureOutput struct {
	OriginPolicy string
	Policy       string
	Algorithm    string
	Credential   string
	Date         string
	Signature    string
}

// SetBucketRequestPaymentInput is the input parameter of SetBucketRequestPayment function
type SetBucketRequestPaymentInput struct {
	Bucket string `xml:"-"`
	BucketPayer
}

// GetBucketRequestPaymentOutput is the result of GetBucketRequestPayment function
type GetBucketRequestPaymentOutput struct {
	BaseModel
	BucketPayer
}
