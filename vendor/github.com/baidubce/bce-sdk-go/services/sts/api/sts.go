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

// sts.go - define the response for STS service

// Package api defines all APIs supported by the STS service of BCE.
package api

import (
	"fmt"
	"strconv"

	"github.com/baidubce/bce-sdk-go/bce"
	"github.com/baidubce/bce-sdk-go/http"
)

const (
	DEFAULT_DURATION_SECONDS = 43200                 // default duration is 12 hours
	URI_PREFIX               = bce.URI_PREFIX + "v1" // sts service not support uri without prefix "v1"
	REQUEST_URI              = "/sessionToken"

	DEFAULT_ASSUMEROLE_DURATION_SECONDS = 7200       // default duration is 2 hours
	REQUEST_ASSUMEROLE_URI   = "/credential"
)


// GetSessionToken - get the session token from the STS service
//
// PARAMS:
//     - cli: the client object which can perform sending request
//     - durationSec: the duration seconds of the token
//     - acl: the acl string
//
// RETURNS:
//     - *GetSessionTokenResult: result of this api
//     - error: nil if ok otherwise the specific error
func GetSessionToken(cli bce.Client, durationSec int, acl string) (*GetSessionTokenResult, error) {
	// If the duration seconds is not a positive, use the default value
	if durationSec <= 0 {
		durationSec = DEFAULT_DURATION_SECONDS
	}

	// Build the request
	req := &bce.BceRequest{}
	req.SetUri(URI_PREFIX + REQUEST_URI)
	req.SetMethod(http.POST)
	req.SetParam("durationSeconds", strconv.Itoa(durationSec))
	if len(acl) > 0 {
		req.SetHeader(http.CONTENT_TYPE, bce.DEFAULT_CONTENT_TYPE)
		body, err := bce.NewBodyFromString(acl)
		if err != nil {
			return nil, err
		}
		req.SetBody(body)
	}

	// Send requet and get response
	resp := &bce.BceResponse{}
	if err := cli.SendRequest(req, resp); err != nil {
		return nil, err
	}
	if resp.IsFail() {
		return nil, resp.ServiceError()
	}
	jsonBody := &GetSessionTokenResult{}
	if err := resp.ParseJsonBody(jsonBody); err != nil {
		return nil, err
	}
	return jsonBody, nil
}

// AssumeRole - get the credential for the assume role from the STS service
//
// PARAMS:
//     - cli: the client object which can perform sending request
//     - args: the args for assumeRole
// RETURNS:
//     - *Credential: result of this api
//     - error: nil if ok otherwise the specific error
func AssumeRole(cli bce.Client, args *AssumeRoleArgs) (*Credential, error) {
	// If the duration seconds is not a positive, use the default value
	if args.DurationSeconds <= 0 {
		args.DurationSeconds = DEFAULT_ASSUMEROLE_DURATION_SECONDS
	}

	if args.AccountId == "" {
		return nil, fmt.Errorf("please set accountId")
	}

	if args.RoleName == "" {
		return nil, fmt.Errorf("please set roleName")
	}

	// Build the request
	req := &bce.BceRequest{}
	req.SetUri(URI_PREFIX + REQUEST_ASSUMEROLE_URI)
	req.SetMethod(http.POST)
	req.SetParam("assumeRole", "")
	req.SetParam("durationSeconds", strconv.Itoa(args.DurationSeconds))
	req.SetParam("accountId", args.AccountId)
	req.SetParam("roleName", args.RoleName)

	if args.UserId != "" {
		req.SetParam("userId", args.UserId)
	}

	if len(args.Acl) > 0 {
		req.SetHeader(http.CONTENT_TYPE, bce.DEFAULT_CONTENT_TYPE)
		body, err := bce.NewBodyFromString(args.Acl)
		if err != nil {
			return nil, err
		}
		req.SetBody(body)
	}

	// Send requet and get response
	resp := &bce.BceResponse{}
	if err := cli.SendRequest(req, resp); err != nil {
		return nil, err
	}
	if resp.IsFail() {
		return nil, resp.ServiceError()
	}
	jsonBody := &Credential{}
	if err := resp.ParseJsonBody(jsonBody); err != nil {
		return nil, err
	}
	return jsonBody, nil
}
