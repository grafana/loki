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
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/baidubce/bce-sdk-go/util/log"
)

var (
	defaultMaxIdleConnsPerHost   = 500
	defaultResponseHeaderTimeout = 60 * time.Second
	defaultDialTimeout           = 30 * time.Second
	defaultKeepAlive             = 30 * time.Second
	defaultSmallInterval         = 600 * time.Second
	defaultLargeInterval         = 1200 * time.Second
	defaultTLSHandshakeTimeout   = 10 * time.Second
	defaultIdleConnTimeout       = 90 * time.Second
	defaultHTTPClientTimeout     = 1200 * time.Second
	NilHTTPClient                = fmt.Errorf("custom HTTP Client is nil")
)

// The httpClient is the global variable to send the request and get response
// for reuse and the Client provided by the Go standard library is thread safe.
var (
	httpClient *http.Client
	transport  *http.Transport
)

var defaultHTTPClient = &http.Client{
	Timeout:   defaultHTTPClientTimeout,
	Transport: NewTransportCustom(&DefaultClientConfig),
}

type Dialer struct {
	net.Dialer
	ReadTimeout  *time.Duration
	WriteTimeout *time.Duration
	postRead     []func(n int, err error)
	postWrite    []func(n int, err error)
}

func NewDialer(config *ClientConfig) *Dialer {
	dialer := &Dialer{
		Dialer: net.Dialer{
			Timeout:   *config.DialTimeout,
			KeepAlive: *config.KeepAlive,
		},
		ReadTimeout:  config.ReadTimeout,
		WriteTimeout: config.WriteTimeout,
		postRead:     config.PostRead,
		postWrite:    config.PostWrite,
	}
	return dialer
}

func (d *Dialer) Dial(network, address string) (net.Conn, error) {
	return d.DialContext(context.Background(), network, address)
}

func (d *Dialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	c, err := d.Dialer.DialContext(ctx, network, address)
	if err != nil {
		return c, err
	}
	tc := &timeoutConn{
		conn:          c,
		smallInterval: defaultSmallInterval,
		largeInterval: defaultLargeInterval,
		readTimeout:   d.ReadTimeout,
		writeTimeout:  d.WriteTimeout,
		dialer:        d,
	}
	if tc.readTimeout != nil {
		err = tc.SetReadDeadline(time.Now().Add(*tc.readTimeout))
	} else {
		err = tc.SetReadDeadline(time.Now().Add(defaultLargeInterval))
	}
	if err != nil {
		return nil, err
	}
	if tc.writeTimeout != nil {
		err = tc.SetWriteDeadline(time.Now().Add(*tc.writeTimeout))
	} else {
		err = tc.SetWriteDeadline(time.Now().Add(defaultLargeInterval))
	}
	if err != nil {
		return nil, err
	}
	return tc, err
}

type timeoutConn struct {
	conn          net.Conn
	smallInterval time.Duration
	largeInterval time.Duration
	readTimeout   *time.Duration
	writeTimeout  *time.Duration
	dialer        *Dialer
}

func (c *timeoutConn) Read(b []byte) (n int, err error) {
	if c.readTimeout == nil {
		err = c.SetReadDeadline(time.Now().Add(c.smallInterval))
		if err != nil {
			return n, err
		}
	}
	n, err = c.conn.Read(b)
	if err != nil {
		return n, err
	}
	if c.readTimeout == nil {
		err = c.SetReadDeadline(time.Now().Add(c.largeInterval))
	} else {
		err = c.SetReadDeadline(time.Now().Add(*c.readTimeout))
	}
	if c.dialer != nil {
		for _, fn := range c.dialer.postRead {
			fn(n, err)
		}
	}
	return n, err
}
func (c *timeoutConn) Write(b []byte) (n int, err error) {
	if c.writeTimeout == nil {
		err = c.SetWriteDeadline(time.Now().Add(c.smallInterval))
	}
	if err != nil {
		return n, err
	}
	n, err = c.conn.Write(b)
	if err != nil {
		return n, err
	}
	if c.writeTimeout == nil {
		err = c.SetWriteDeadline(time.Now().Add(c.largeInterval))
	} else {
		err = c.SetWriteDeadline(time.Now().Add(*c.writeTimeout))
	}
	if c.dialer != nil {
		for _, fn := range c.dialer.postWrite {
			fn(n, err)
		}
	}
	return n, err
}
func (c *timeoutConn) Close() error                       { return c.conn.Close() }
func (c *timeoutConn) LocalAddr() net.Addr                { return c.conn.LocalAddr() }
func (c *timeoutConn) RemoteAddr() net.Addr               { return c.conn.RemoteAddr() }
func (c *timeoutConn) SetDeadline(t time.Time) error      { return c.conn.SetDeadline(t) }
func (c *timeoutConn) SetReadDeadline(t time.Time) error  { return c.conn.SetReadDeadline(t) }
func (c *timeoutConn) SetWriteDeadline(t time.Time) error { return c.conn.SetWriteDeadline(t) }

type ClientConfig struct {
	RedirectDisabled      bool
	DisableKeepAlives     bool
	NoVerifySSL           bool
	DialTimeout           *time.Duration
	KeepAlive             *time.Duration
	ReadTimeout           *time.Duration
	WriteTimeout          *time.Duration
	TLSHandshakeTimeout   *time.Duration
	IdleConnectionTimeout *time.Duration
	ResponseHeaderTimeout *time.Duration
	HTTPClientTimeout     *time.Duration
	PostRead              []func(n int, err error)
	PostWrite             []func(n int, err error)
}

var DefaultClientConfig = ClientConfig{
	RedirectDisabled:      false,
	DisableKeepAlives:     false,
	DialTimeout:           &defaultDialTimeout,
	KeepAlive:             &defaultKeepAlive,
	TLSHandshakeTimeout:   &defaultTLSHandshakeTimeout,
	IdleConnectionTimeout: &defaultIdleConnTimeout,
	ResponseHeaderTimeout: &defaultResponseHeaderTimeout,
	HTTPClientTimeout:     &defaultHTTPClientTimeout,
}

func (cfg *ClientConfig) Copy(src *ClientConfig) {
	if src == nil {
		return
	}
	cfg.RedirectDisabled = src.RedirectDisabled
	cfg.DisableKeepAlives = src.DisableKeepAlives
	if src.DialTimeout != nil {
		cfg.DialTimeout = src.DialTimeout
	}
	if src.KeepAlive != nil {
		cfg.KeepAlive = src.KeepAlive
	}
	if src.ReadTimeout != nil {
		cfg.ReadTimeout = src.ReadTimeout
	}
	if src.WriteTimeout != nil {
		cfg.WriteTimeout = src.WriteTimeout
	}
	if src.TLSHandshakeTimeout != nil {
		cfg.TLSHandshakeTimeout = src.TLSHandshakeTimeout
	}
	if src.IdleConnectionTimeout != nil {
		cfg.IdleConnectionTimeout = src.IdleConnectionTimeout
	}
	if src.ResponseHeaderTimeout != nil {
		cfg.ResponseHeaderTimeout = src.ResponseHeaderTimeout
	}
	if src.HTTPClientTimeout != nil {
		cfg.HTTPClientTimeout = src.HTTPClientTimeout
	}
	if len(src.PostRead) > 0 {
		cfg.PostRead = append(cfg.PostRead, src.PostRead...)
	}
	if len(src.PostWrite) > 0 {
		cfg.PostWrite = append(cfg.PostWrite, src.PostWrite...)
	}
}

func MergeWithDefaultConfig(cfgs ...*ClientConfig) *ClientConfig {
	dst := &ClientConfig{}
	dst.Copy(&DefaultClientConfig)
	for _, cfg := range cfgs {
		dst.Copy(cfg)
	}
	return dst
}

var customizeInit sync.Once

func InitClient(config ClientConfig) {
	customizeInit.Do(func() {
		httpClient = &http.Client{}
		maxIdleConnsPerHost := defaultMaxIdleConnsPerHost
		if config.DisableKeepAlives {
			maxIdleConnsPerHost = -1
		}
		transport = &http.Transport{
			MaxIdleConnsPerHost:   maxIdleConnsPerHost,
			ResponseHeaderTimeout: defaultResponseHeaderTimeout,
			DisableKeepAlives:     config.DisableKeepAlives,
			Dial: func(network, address string) (net.Conn, error) {
				conn, err := net.DialTimeout(network, address, defaultDialTimeout)
				if err != nil {
					return nil, err
				}
				tc := &timeoutConn{
					conn:          conn,
					smallInterval: defaultSmallInterval,
					largeInterval: defaultLargeInterval,
				}
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

func NewTransportCustom(config *ClientConfig) *http.Transport {
	maxIdleConnsPerHost := defaultMaxIdleConnsPerHost
	if config.DisableKeepAlives {
		maxIdleConnsPerHost = -1
	}
	transport := &http.Transport{
		DialContext:           NewDialer(config).DialContext,
		MaxIdleConnsPerHost:   maxIdleConnsPerHost,
		DisableKeepAlives:     config.DisableKeepAlives,
		TLSHandshakeTimeout:   *config.TLSHandshakeTimeout,
		ResponseHeaderTimeout: *config.ResponseHeaderTimeout,
		IdleConnTimeout:       *config.IdleConnectionTimeout,
	}
	if config.NoVerifySSL {
		transport.TLSClientConfig = &tls.Config{
			InsecureSkipVerify: true,
		}
	}
	return transport
}

func InitWithSpecifiedClient(customHTTPClient *http.Client) error {
	if customHTTPClient == nil {
		log.Warnf("customized HTTP Client is nil")
		return NilHTTPClient
	}
	if customHTTPClient.Transport == nil {
		log.Infof("transport is nil")
		return nil
	}
	if customTransport, ok := customHTTPClient.Transport.(*http.Transport); ok {
		transport = customTransport
	}
	return nil
}

func InitExclusiveHTTPClient(config *ClientConfig) *http.Client {
	config = MergeWithDefaultConfig(config)
	transport = NewTransportCustom(config)
	myHTTPClient := &http.Client{
		Timeout:   *config.HTTPClientTimeout,
		Transport: transport,
	}
	if config.RedirectDisabled {
		myHTTPClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}
	return myHTTPClient
}

func InitClientWithTimeout(config *ClientConfig) {
	customizeInit.Do(func() {
		config = MergeWithDefaultConfig(config)
		transport = NewTransportCustom(config)
		httpClient = &http.Client{
			Timeout:   *config.HTTPClientTimeout,
			Transport: transport,
		}
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
//   - request: the http request instance to be sent
//
// RETURNS:
//   - response: the http response returned from the server
//   - error: nil if ok otherwise the specific error
func Execute(request *Request) (*Response, error) {
	// Build the request object for the current requesting
	httpRequest := &http.Request{
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
	}

	// get http client
	curHTTPClient := httpClient
	if request.HTTPClient() != nil {
		curHTTPClient = request.HTTPClient()
	}
	if curHTTPClient == nil {
		log.Infof("use default http client to execute request")
		curHTTPClient = defaultHTTPClient
	}

	if request.Context() != nil {
		httpRequest = httpRequest.WithContext(request.Context())
	} else {
		// Set the connection timeout for current request
		curHTTPClient.Timeout = time.Duration(request.Timeout()) * time.Second
	}

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
	if len(request.ProxyUrl()) != 0 && transport != nil {
		transport.Proxy = func(_ *http.Request) (*url.URL, error) {
			return url.Parse(request.ProxyUrl())
		}
	}

	// Perform the http request and get response
	// It needs to explicitly close the keep-alive connections when error occurs for the request
	// that may continue sending request's data subsequently.
	start := time.Now()

	httpResponse, err := curHTTPClient.Do(httpRequest)
	end := time.Now()
	if err != nil {
		if transport != nil {
			transport.CloseIdleConnections()
		}
		return nil, err
	}
	if transport != nil && httpResponse.StatusCode >= 400 &&
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
			tc := &timeoutConn{
				conn:          conn,
				smallInterval: defaultSmallInterval,
				largeInterval: defaultLargeInterval,
			}
			err = tc.SetReadDeadline(time.Now().Add(defaultLargeInterval))
			if err != nil {
				return nil, err
			}
			return tc, nil
		},
	}
	httpClient.Transport = transport
}
