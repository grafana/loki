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

// config.go - define the client configuration for BCE

package bce

import (
	"fmt"
	"reflect"
	"runtime"

	"github.com/baidubce/bce-sdk-go/auth"
)

// Constants and default values for the package bce
const (
	SDK_VERSION                          = "0.9.189"
	URI_PREFIX                           = "/" // now support uri without prefix "v1" so just set root path
	DEFAULT_DOMAIN                       = "baidubce.com"
	DEFAULT_PROTOCOL                     = "http"
	HTTPS_PROTOCAL                       = "https"
	DEFAULT_REGION                       = "bj"
	DEFAULT_CONTENT_TYPE                 = "application/json;charset=utf-8"
	DEFAULT_CONNECTION_TIMEOUT_IN_MILLIS = 1200 * 1000
	DEFAULT_WARN_LOG_TIMEOUT_IN_MILLS    = 5 * 1000
)

var (
	DEFAULT_USER_AGENT   string
	DEFAULT_RETRY_POLICY = NewBackOffRetryPolicy(3, 20000, 300)
)

func init() {
	DEFAULT_USER_AGENT = "bce-sdk-go"
	DEFAULT_USER_AGENT += "/" + SDK_VERSION
	DEFAULT_USER_AGENT += "/" + runtime.Version()
	DEFAULT_USER_AGENT += "/" + runtime.GOOS
	DEFAULT_USER_AGENT += "/" + runtime.GOARCH
}

// BceClientConfiguration defines the config components structure.
type BceClientConfiguration struct {
	Endpoint                  string
	ProxyUrl                  string
	Region                    string
	UserAgent                 string
	Credentials               *auth.BceCredentials
	SignOption                *auth.SignOptions
	Retry                     RetryPolicy
	ConnectionTimeoutInMillis int
	// CnameEnabled should be true when use custom domain as endpoint to visit bos resource
	CnameEnabled     bool
	BackupEndpoint   string
	RedirectDisabled bool
}

func (c *BceClientConfiguration) String() string {
	return fmt.Sprintf(`BceClientConfiguration [
        Endpoint=%s;
        ProxyUrl=%s;
        Region=%s;
        UserAgent=%s;
        Credentials=%v;
        SignOption=%v;
        RetryPolicy=%v;
        ConnectionTimeoutInMillis=%v;
		RedirectDisabled=%v
    ]`, c.Endpoint, c.ProxyUrl, c.Region, c.UserAgent, c.Credentials,
		c.SignOption, reflect.TypeOf(c.Retry).Name(), c.ConnectionTimeoutInMillis, c.RedirectDisabled)
}
