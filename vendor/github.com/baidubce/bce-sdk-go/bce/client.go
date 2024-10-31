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

// client.go - definiton the BceClientConfiguration and BceClient structure

// Package bce implements the infrastructure to access BCE services.
//
// - BceClient:
//     It is the general client of BCE to access all services. It builds http request to access the
//     services based on the given client configuration.
//
// - BceClientConfiguration:
//     The client configuration data structure which contains endpoint, region, credentials, retry
//     policy, sign options and so on. It supports most of the default value and user can also
//     access or change the default with its public fields' name.
//
// - Error types:
//     The error types when making request or receiving response to the BCE services contains two
//     types: the BceClientError when making request to BCE services and the BceServiceError when
//     recieving response from them.
//
// - BceRequest:
//     The request instance stands for an request to access the BCE services.
//
// - BceResponse:
//     The response instance stands for an response from the BCE services.
package bce

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"time"

	"github.com/baidubce/bce-sdk-go/auth"
	"github.com/baidubce/bce-sdk-go/http"
	"github.com/baidubce/bce-sdk-go/util"
	"github.com/baidubce/bce-sdk-go/util/log"
)

// Client is the general interface which can perform sending request. Different service
// will define its own client in case of specific extension.
type Client interface {
	SendRequest(*BceRequest, *BceResponse) error
	SendRequestFromBytes(*BceRequest, *BceResponse, []byte) error
	GetBceClientConfig() *BceClientConfiguration
}

// BceClient defines the general client to access the BCE services.
type BceClient struct {
	Config *BceClientConfiguration
	Signer auth.Signer // the sign algorithm
}

// BuildHttpRequest - the helper method for the client to build http request
//
// PARAMS:
//     - request: the input request object to be built
func (c *BceClient) buildHttpRequest(request *BceRequest) {
	// Construct the http request instance for the special fields
	request.BuildHttpRequest()

	// Set the client specific configurations
	if request.Endpoint() == "" {
		request.SetEndpoint(c.Config.Endpoint)
	}
	if request.Protocol() == "" {
		request.SetProtocol(DEFAULT_PROTOCOL)
	}
	if len(c.Config.ProxyUrl) != 0 {
		request.SetProxyUrl(c.Config.ProxyUrl)
	}
	request.SetTimeout(c.Config.ConnectionTimeoutInMillis / 1000)

	// Set the BCE request headers
	request.SetHeader(http.HOST, request.Host())
	request.SetHeader(http.USER_AGENT, c.Config.UserAgent)
	request.SetHeader(http.BCE_DATE, util.FormatISO8601Date(util.NowUTCSeconds()))

	//set default content-type if null
	if request.Header(http.CONTENT_TYPE) == "" {
		request.SetHeader(http.CONTENT_TYPE, DEFAULT_CONTENT_TYPE)
	}

	// Generate the auth string if needed
	if c.Config.Credentials != nil {
		c.Signer.Sign(&request.Request, c.Config.Credentials, c.Config.SignOption)
	}
}

// SendRequest - the client performs sending the http request with retry policy and receive the
// response from the BCE services.
//
// PARAMS:
//     - req: the request object to be sent to the BCE service
//     - resp: the response object to receive the content from BCE service
// RETURNS:
//     - error: nil if ok otherwise the specific error
func (c *BceClient) SendRequest(req *BceRequest, resp *BceResponse) error {
	// Return client error if it is not nil
	if req.ClientError() != nil {
		return req.ClientError()
	}

	// Build the http request and prepare to send
	c.buildHttpRequest(req)
	log.Infof("send http request: %v", req)

	// Send request with the given retry policy
	retries := 0
	if req.Body() != nil {
		defer req.Body().Close() // Manually close the ReadCloser body for retry
	}
	for {
		// The request body should be temporarily saved if retry to send the http request
		var retryBuf bytes.Buffer
		var teeReader io.Reader
		if c.Config.Retry.ShouldRetry(nil, 0) && req.Body() != nil {
			teeReader = io.TeeReader(req.Body(), &retryBuf)
			req.Request.SetBody(ioutil.NopCloser(teeReader))
		}
		httpResp, err := http.Execute(&req.Request)

		if err != nil {
			if c.Config.Retry.ShouldRetry(err, retries) {
				delay_in_mills := c.Config.Retry.GetDelayBeforeNextRetryInMillis(err, retries)
				time.Sleep(delay_in_mills)
			} else {
				return &BceClientError{
					fmt.Sprintf("execute http request failed! Retried %d times, error: %v",
						retries, err)}
			}
			retries++
			log.Warnf("send request failed: %v, retry for %d time(s)", err, retries)
			if req.Body() != nil {
				ioutil.ReadAll(teeReader)
				req.Request.SetBody(ioutil.NopCloser(&retryBuf))
			}
			continue
		}
		resp.SetHttpResponse(httpResp)
		resp.ParseResponse()

		log.Infof("receive http response: status: %s, debugId: %s, requestId: %s, elapsed: %v",
			resp.StatusText(), resp.DebugId(), resp.RequestId(), resp.ElapsedTime())

		if resp.ElapsedTime().Milliseconds() > DEFAULT_WARN_LOG_TIMEOUT_IN_MILLS {
			log.Warnf("request time more than 5 second, debugId: %s, requestId: %s, elapsed: %v",
			resp.DebugId(), resp.RequestId(), resp.ElapsedTime())
		}
		for k, v := range resp.Headers() {
			log.Debugf("%s=%s", k, v)
		}
		if resp.IsFail() {
			err := resp.ServiceError()
			if c.Config.Retry.ShouldRetry(err, retries) {
				delay_in_mills := c.Config.Retry.GetDelayBeforeNextRetryInMillis(err, retries)
				time.Sleep(delay_in_mills)
			} else {
				return err
			}
			retries++
			log.Warnf("send request failed, retry for %d time(s)", retries)
			if req.Body() != nil {
				ioutil.ReadAll(teeReader)
				req.Request.SetBody(ioutil.NopCloser(&retryBuf))
			}
			continue
		}
		return nil
	}
}

// SendRequestFromBytes - the client performs sending the http request with retry policy and receive the
// response from the BCE services.
//
// PARAMS:
//     - req: the request object to be sent to the BCE service
//     - resp: the response object to receive the content from BCE service
//     - content: the content of body
// RETURNS:
//     - error: nil if ok otherwise the specific error
func (c *BceClient) SendRequestFromBytes(req *BceRequest, resp *BceResponse, content []byte) error {
	// Return client error if it is not nil
	if req.ClientError() != nil {
		return req.ClientError()
	}
	// Build the http request and prepare to send
	c.buildHttpRequest(req)
	log.Infof("send http request: %v", req)
	// Send request with the given retry policy
	retries := 0
	for {
		// The request body should be temporarily saved if retry to send the http request
		buf := bytes.NewBuffer(content)
		req.Request.SetBody(ioutil.NopCloser(buf))
		defer req.Request.Body().Close() // Manually close the ReadCloser body for retry
		httpResp, err := http.Execute(&req.Request)
		if err != nil {
			if c.Config.Retry.ShouldRetry(err, retries) {
				delay_in_mills := c.Config.Retry.GetDelayBeforeNextRetryInMillis(err, retries)
				time.Sleep(delay_in_mills)
			} else {
				return &BceClientError{
					fmt.Sprintf("execute http request failed! Retried %d times, error: %v",
						retries, err)}
			}
			retries++
			log.Warnf("send request failed: %v, retry for %d time(s)", err, retries)
			continue
		}
		resp.SetHttpResponse(httpResp)
		resp.ParseResponse()
		log.Infof("receive http response: status: %s, debugId: %s, requestId: %s, elapsed: %v",
			resp.StatusText(), resp.DebugId(), resp.RequestId(), resp.ElapsedTime())
		for k, v := range resp.Headers() {
			log.Debugf("%s=%s", k, v)
		}
		if resp.IsFail() {
			err := resp.ServiceError()
			if c.Config.Retry.ShouldRetry(err, retries) {
				delay_in_mills := c.Config.Retry.GetDelayBeforeNextRetryInMillis(err, retries)
				time.Sleep(delay_in_mills)
			} else {
				return err
			}
			retries++
			log.Warnf("send request failed, retry for %d time(s)", retries)
			continue
		}
		return nil
	}
}

func (c *BceClient) GetBceClientConfig() *BceClientConfiguration {
	return c.Config
}

func NewBceClient(conf *BceClientConfiguration, sign auth.Signer) *BceClient {
	clientConfig := http.ClientConfig{RedirectDisabled: conf.RedirectDisabled}
	http.InitClient(clientConfig)
	return &BceClient{conf, sign}
}

func NewBceClientWithAkSk(ak, sk, endPoint string) (*BceClient, error) {
	credentials, err := auth.NewBceCredentials(ak, sk)
	if err != nil {
		return nil, err
	}
	defaultSignOptions := &auth.SignOptions{
		HeadersToSign: auth.DEFAULT_HEADERS_TO_SIGN,
		ExpireSeconds: auth.DEFAULT_EXPIRE_SECONDS}
	defaultConf := &BceClientConfiguration{
		Endpoint:    endPoint,
		Region:      DEFAULT_REGION,
		UserAgent:   DEFAULT_USER_AGENT,
		Credentials: credentials,
		SignOption:  defaultSignOptions,
		Retry:       DEFAULT_RETRY_POLICY,
		ConnectionTimeoutInMillis: DEFAULT_CONNECTION_TIMEOUT_IN_MILLIS,
		RedirectDisabled:          false}
	v1Signer := &auth.BceV1Signer{}

	return NewBceClient(defaultConf, v1Signer), nil
}
