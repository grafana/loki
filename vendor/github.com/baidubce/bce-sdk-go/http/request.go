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

// request.go - the custom HTTP request for BCE

package http

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/baidubce/bce-sdk-go/util"
)

// Reauest stands for the general http request structure to make request to the BCE services.
type Request struct {
	protocol string
	host     string
	port     int
	method   string
	uri      string
	proxyUrl string
	timeout  int
	headers  map[string]string
	params   map[string]string

	// Optional body and length fields to set the body stream and content length
	body   io.ReadCloser
	length int64
}

func (r *Request) Protocol() string {
	return r.protocol
}

func (r *Request) SetProtocol(protocol string) {
	r.protocol = protocol
}

func (r *Request) Endpoint() string {
	if r.host == "" {
		return ""
	}
	return r.protocol + "://" + r.host
}

func (r *Request) SetEndpoint(endpoint string) {
	pos := strings.Index(endpoint, "://")
	rest := endpoint
	if pos != -1 {
		r.protocol = endpoint[0:pos]
		rest = endpoint[pos+3:]
	} else {
		r.protocol = "http"
	}

	r.SetHost(rest)
}

func (r *Request) Host() string {
	return r.host
}

func (r *Request) SetHost(host string) {
	r.host = host
	pos := strings.Index(host, ":")
	if pos != -1 {
		p, e := strconv.Atoi(host[pos+1:])
		if e == nil {
			r.port = p
		}
	}

	if r.port == 0 {
		if r.protocol == "http" {
			r.port = 80
		} else if r.protocol == "https" {
			r.port = 443
		}
	}
}

func (r *Request) Port() int {
	return r.port
}

func (r *Request) SetPort(port int) {
	// Port can be set by the endpoint or host, this method is rarely used.
	r.port = port
}

func (r *Request) Headers() map[string]string {
	return r.headers
}

func (r *Request) SetHeaders(headers map[string]string) {
	r.headers = headers
}

func (r *Request) Header(key string) string {
	if v, ok := r.headers[key]; ok {
		return v
	}
	return ""
}

func (r *Request) SetHeader(key, value string) {
	if r.headers == nil {
		r.headers = make(map[string]string)
	}
	r.headers[key] = value
}

func (r *Request) Params() map[string]string {
	return r.params
}

func (r *Request) SetParams(params map[string]string) {
	r.params = params
}

func (r *Request) Param(key string) string {
	if v, ok := r.params[key]; ok {
		return v
	}
	return ""
}

func (r *Request) SetParam(key, value string) {
	if r.params == nil {
		r.params = make(map[string]string)
	}
	r.params[key] = value
}

func (r *Request) QueryString() string {
	buf := make([]string, 0, len(r.params))
	for k, v := range r.params {
		if len(v) == 0 {
			buf = append(buf, util.UriEncode(k, true))
		} else {
			buf = append(buf, util.UriEncode(k, true)+"="+util.UriEncode(v, true))
		}
	}
	return strings.Join(buf, "&")
}

func (r *Request) Method() string {
	return r.method
}

func (r *Request) SetMethod(method string) {
	r.method = method
}

func (r *Request) Uri() string {
	return r.uri
}

func (r *Request) SetUri(uri string) {
	r.uri = uri
}

func (r *Request) ProxyUrl() string {
	return r.proxyUrl
}

func (r *Request) SetProxyUrl(url string) {
	r.proxyUrl = url
}

func (r *Request) Timeout() int {
	return r.timeout
}

func (r *Request) SetTimeout(timeout int) {
	r.timeout = timeout
}

func (r *Request) Body() io.ReadCloser {
	return r.body
}

func (r *Request) SetBody(stream io.ReadCloser) {
	r.body = stream
}

func (r *Request) Length() int64 {
	return r.length
}

func (r *Request) SetLength(l int64) {
	r.length = l
}

func (r *Request) GenerateUrl(addPort bool) string {
	if addPort {
		return fmt.Sprintf("%s://%s:%d%s?%s",
			r.protocol, r.host, r.port, r.uri, r.QueryString())
	} else {
		return fmt.Sprintf("%s://%s%s?%s", r.protocol, r.host, r.uri, r.QueryString())
	}
}

func (r *Request) String() string {
	header := make([]string, 0, len(r.headers))
	for k, v := range r.headers {
		header = append(header, "\t"+k+"="+v)
	}
	return fmt.Sprintf("\t%s %s\n%v",
		r.method, r.GenerateUrl(false), strings.Join(header, "\n"))
}
