/*
 * MinIO Go Library for Amazon S3 Compatible Cloud Storage
 * Copyright 2015-2024 MinIO, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package minio

import (
	"net/http"
	"net/url"
)

// PromptObjectOptions provides options to PromptObject call.
// LambdaArn is the ARN of the Prompt Lambda to be invoked.
// PromptArgs is a map of key-value pairs to be passed to the inference action on the Prompt Lambda.
// "prompt" is a reserved key and should not be used as a key in PromptArgs.
type PromptObjectOptions struct {
	LambdaArn  string
	PromptArgs map[string]any
	headers    map[string]string
	reqParams  url.Values
}

// Header returns the http.Header representation of the POST options.
func (o PromptObjectOptions) Header() http.Header {
	headers := make(http.Header, len(o.headers))
	for k, v := range o.headers {
		headers.Set(k, v)
	}
	return headers
}

// AddPromptArg Add a key value pair to the prompt arguments where the key is a string and
// the value is a JSON serializable.
func (o *PromptObjectOptions) AddPromptArg(key string, value any) {
	if o.PromptArgs == nil {
		o.PromptArgs = make(map[string]any)
	}
	o.PromptArgs[key] = value
}

// AddLambdaArnToReqParams adds the lambdaArn to the request query string parameters.
func (o *PromptObjectOptions) AddLambdaArnToReqParams(lambdaArn string) {
	if o.reqParams == nil {
		o.reqParams = make(url.Values)
	}
	o.reqParams.Add("lambdaArn", lambdaArn)
}

// SetHeader adds a key value pair to the options. The
// key-value pair will be part of the HTTP POST request
// headers.
func (o *PromptObjectOptions) SetHeader(key, value string) {
	if o.headers == nil {
		o.headers = make(map[string]string)
	}
	o.headers[http.CanonicalHeaderKey(key)] = value
}

// toQueryValues - Convert the reqParams in Options to query string parameters.
func (o *PromptObjectOptions) toQueryValues() url.Values {
	urlValues := make(url.Values)
	if o.reqParams != nil {
		for key, values := range o.reqParams {
			for _, value := range values {
				urlValues.Add(key, value)
			}
		}
	}

	return urlValues
}
