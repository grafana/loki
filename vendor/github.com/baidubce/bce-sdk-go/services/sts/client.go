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

// client.go - define the client for STS service which is derived from BceClient

// Package sts defines the STS service of BCE.
// It contains the model sub package to implement the concrete request and response of the
// GetSessionToken API.
package sts

import (
	"github.com/baidubce/bce-sdk-go/auth"
	"github.com/baidubce/bce-sdk-go/bce"
	"github.com/baidubce/bce-sdk-go/services/sts/api"
)

const DEFAULT_SERVICE_DOMAIN = "sts." + bce.DEFAULT_REGION + "." + bce.DEFAULT_DOMAIN

// Client of STS service is a kind of BceClient, so it derived from the BceClient and it only
// supports the GetSessionToken API. There is no other fields needed.
type Client struct {
	*bce.BceClient
}

func (c *Client) GetSessionToken(duration int, acl string) (*api.GetSessionTokenResult, error) {
	return api.GetSessionToken(c, duration, acl)
}

func (c *Client) AssumeRole(args *api.AssumeRoleArgs) (*api.Credential, error) {
	return api.AssumeRole(c, args)
}

// NewClient make the STS service client with default configuration.
// Use `cli.Config.xxx` to access the config or change it to non-default value.
func NewClient(ak, sk string) (*Client, error) {
	return NewStsClient(ak, sk, DEFAULT_SERVICE_DOMAIN)
}

func NewStsClient(ak, sk, endpoint string) (*Client, error) {
	credentials, err := auth.NewBceCredentials(ak, sk)
	if err != nil {
		return nil, err
	}
	if len(endpoint) == 0 {
		endpoint = DEFAULT_SERVICE_DOMAIN
	}
	defaultSignOptions := &auth.SignOptions{
		HeadersToSign: auth.DEFAULT_HEADERS_TO_SIGN,
		Timestamp:     0,
		ExpireSeconds: auth.DEFAULT_EXPIRE_SECONDS}
	defaultConf := &bce.BceClientConfiguration{
		Endpoint:                  endpoint,
		Region:                    bce.DEFAULT_REGION,
		UserAgent:                 bce.DEFAULT_USER_AGENT,
		Credentials:               credentials,
		SignOption:                defaultSignOptions,
		Retry:                     bce.DEFAULT_RETRY_POLICY,
		ConnectionTimeoutInMillis: bce.DEFAULT_CONNECTION_TIMEOUT_IN_MILLIS}
	v1Signer := &auth.BceV1Signer{}

	client := &Client{bce.NewBceClient(defaultConf, v1Signer)}
	return client, nil
}
