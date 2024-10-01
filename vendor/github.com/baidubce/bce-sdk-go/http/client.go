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

// client.go - define the execute function to send http request and get response

// Package http defines the structure of request and response which used to access the BCE services
// as well as the http constant headers and and methods. And finally implement the `Execute` funct-
// ion to do the work.
package http

import (
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"
)

const (
	defaultMaxIdleConnsPerHost   = 500
	defaultResponseHeaderTimeout = 60 * time.Second
	defaultDialTimeout           = 30 * time.Second
	defaultSmallInterval         = 600 * time.Second
	defaultLargeInterval         = 1200 * time.Second
)

// The httpClient is the global variable to send the request and get response
// for reuse and the Client provided by the Go standard library is thread safe.
var (
	httpClient *http.Client
	transport  *http.Transport
)

type timeoutConn struct {
	conn          net.Conn
	smallInterval time.Duration
	largeInterval time.Duration
}

func (c *timeoutConn) Read(b []byte) (n int, err error) {
	c.SetReadDeadline(time.Now().Add(c.smallInterval))
	n, err = c.conn.Read(b)
	c.SetReadDeadline(time.Now().Add(c.largeInterval))
	return n, err
}
func (c *timeoutConn) Write(b []byte) (n int, err error) {
	c.SetWriteDeadline(time.Now().Add(c.smallInterval))
	n, err = c.conn.Write(b)
	c.SetWriteDeadline(time.Now().Add(c.largeInterval))
	return n, err
}
func (c *timeoutConn) Close() error                       { return c.conn.Close() }
func (c *timeoutConn) LocalAddr() net.Addr                { return c.conn.LocalAddr() }
func (c *timeoutConn) RemoteAddr() net.Addr               { return c.conn.RemoteAddr() }
func (c *timeoutConn) SetDeadline(t time.Time) error      { return c.conn.SetDeadline(t) }
func (c *timeoutConn) SetReadDeadline(t time.Time) error  { return c.conn.SetReadDeadline(t) }
func (c *timeoutConn) SetWriteDeadline(t time.Time) error { return c.conn.SetWriteDeadline(t) }

type ClientConfig struct {
	RedirectDisabled bool
}

var customizeInit sync.Once

func InitClient(config ClientConfig) {
	customizeInit.Do(func() {
		httpClient = &http.Client{}
		transport = &http.Transport{
			MaxIdleConnsPerHost:   defaultMaxIdleConnsPerHost,
			ResponseHeaderTimeout: defaultResponseHeaderTimeout,
			Dial: func(network, address string) (net.Conn, error) {
				conn, err := net.DialTimeout(network, address, defaultDialTimeout)
				if err != nil {
					return nil, err
				}
				tc := &timeoutConn{conn, defaultSmallInterval, defaultLargeInterval}
				tc.SetReadDeadline(time.Now().Add(defaultLargeInterval))
				return tc, nil
			},
		}
		httpClient.Transport = transport
		if config.RedirectDisabled {
			httpClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			}
		}
	})
}

// Execute - do the http requset and get the response
//
// PARAMS:
//     - request: the http request instance to be sent
// RETURNS:
//     - response: the http response returned from the server
//     - error: nil if ok otherwise the specific error
func Execute(request *Request) (*Response, error) {
	// Build the request object for the current requesting
	httpRequest := &http.Request{
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
	}

	// Set the connection timeout for current request
	httpClient.Timeout = time.Duration(request.Timeout()) * time.Second

	// Set the request method
	httpRequest.Method = request.Method()

	// Set the request url
	internalUrl := &url.URL{
		Scheme:   request.Protocol(),
		Host:     request.Host(),
		Path:     request.Uri(),
		RawQuery: request.QueryString()}
	httpRequest.URL = internalUrl

	// Set the request headers
	internalHeader := make(http.Header)
	for k, v := range request.Headers() {
		val := make([]string, 0, 1)
		val = append(val, v)
		internalHeader[k] = val
	}
	httpRequest.Header = internalHeader

	if request.Body() != nil {
		if request.Length() > 0 {
			httpRequest.ContentLength = request.Length()
			httpRequest.Body = request.Body()
		} else if request.Length() < 0 {
			// if set body and ContentLength <= 0, will be chunked
			httpRequest.Body = request.Body()
		} // else {} body == nil and ContentLength == 0
	}

	// Set the proxy setting if needed
	if len(request.ProxyUrl()) != 0 {
		transport.Proxy = func(_ *http.Request) (*url.URL, error) {
			return url.Parse(request.ProxyUrl())
		}
	}

	// Perform the http request and get response
	// It needs to explicitly close the keep-alive connections when error occurs for the request
	// that may continue sending request's data subsequently.
	start := time.Now()

	httpResponse, err := httpClient.Do(httpRequest)

	end := time.Now()
	if err != nil {
		transport.CloseIdleConnections()
		return nil, err
	}
	if httpResponse.StatusCode >= 400 &&
		(httpRequest.Method == PUT || httpRequest.Method == POST) {
		transport.CloseIdleConnections()
	}
	response := &Response{httpResponse, end.Sub(start)}
	return response, nil
}
func SetResponseHeaderTimeout(t int) {
	transport = &http.Transport{
		MaxIdleConnsPerHost:   defaultMaxIdleConnsPerHost,
		ResponseHeaderTimeout: time.Duration(t) * time.Second,
		Dial: func(network, address string) (net.Conn, error) {
			conn, err := net.DialTimeout(network, address, defaultDialTimeout)
			if err != nil {
				return nil, err
			}
			tc := &timeoutConn{conn, defaultSmallInterval, defaultLargeInterval}
			err = tc.SetReadDeadline(time.Now().Add(defaultLargeInterval))
			if err != nil {
				return nil, err
			}
			return tc, nil
		},
	}
	httpClient.Transport = transport
}
