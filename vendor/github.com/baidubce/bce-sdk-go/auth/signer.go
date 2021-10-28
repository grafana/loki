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

// signer.go - implement the specific sign algorithm of BCE V1 protocol

package auth

import (
	"fmt"
	"sort"
	"strings"

	"github.com/baidubce/bce-sdk-go/http"
	"github.com/baidubce/bce-sdk-go/util"
	"github.com/baidubce/bce-sdk-go/util/log"
)

var (
	BCE_AUTH_VERSION        = "bce-auth-v1"
	SIGN_JOINER             = "\n"
	SIGN_HEADER_JOINER      = ";"
	DEFAULT_EXPIRE_SECONDS  = 1800
	DEFAULT_HEADERS_TO_SIGN = map[string]struct{}{
		strings.ToLower(http.HOST):           {},
		strings.ToLower(http.CONTENT_LENGTH): {},
		strings.ToLower(http.CONTENT_TYPE):   {},
		strings.ToLower(http.CONTENT_MD5):    {},
	}
)

// Signer abstracts the entity that implements the `Sign` method
type Signer interface {
	// Sign the given Request with the Credentials and SignOptions
	Sign(*http.Request, *BceCredentials, *SignOptions)
}

// SignOptions defines the data structure used by Signer
type SignOptions struct {
	HeadersToSign map[string]struct{}
	Timestamp     int64
	ExpireSeconds int
}

func (opt *SignOptions) String() string {
	return fmt.Sprintf(`SignOptions [
        HeadersToSign=%s;
        Timestamp=%d;
        ExpireSeconds=%d
    ]`, opt.HeadersToSign, opt.Timestamp, opt.ExpireSeconds)
}

// BceV1Signer implements the v1 sign algorithm
type BceV1Signer struct{}

// Sign - generate the authorization string from the BceCredentials and SignOptions
//
// PARAMS:
//     - req: *http.Request for this sign
//     - cred: *BceCredentials to access the serice
//     - opt: *SignOptions for this sign algorithm
func (b *BceV1Signer) Sign(req *http.Request, cred *BceCredentials, opt *SignOptions) {
	if req == nil {
		log.Fatal("request should not be null for sign")
		return
	}
	if cred == nil {
		log.Fatal("credentials should not be null for sign")
		return
	}

	// Prepare parameters
	accessKeyId := cred.AccessKeyId
	secretAccessKey := cred.SecretAccessKey
	signDate := util.FormatISO8601Date(util.NowUTCSeconds())
	// Modify the sign time if it is not the default value but specified by client
	if opt.Timestamp != 0 {
		signDate = util.FormatISO8601Date(opt.Timestamp)
	}

	// Set security token if using session credentials
	if len(cred.SessionToken) != 0 {
		req.SetHeader(http.BCE_SECURITY_TOKEN, cred.SessionToken)
	}

	// Prepare the canonical request components
	signKeyInfo := fmt.Sprintf("%s/%s/%s/%d",
		BCE_AUTH_VERSION,
		accessKeyId,
		signDate,
		opt.ExpireSeconds)
	signKey := util.HmacSha256Hex(secretAccessKey, signKeyInfo)
	canonicalUri := getCanonicalURIPath(req.Uri())
	canonicalQueryString := getCanonicalQueryString(req.Params())
	canonicalHeaders, signedHeadersArr := getCanonicalHeaders(req.Headers(), opt.HeadersToSign)

	// Generate signed headers string
	signedHeaders := ""
	if len(signedHeadersArr) > 0 {
		sort.Strings(signedHeadersArr)
		signedHeaders = strings.Join(signedHeadersArr, SIGN_HEADER_JOINER)
	}

	// Generate signature
	canonicalParts := []string{req.Method(), canonicalUri, canonicalQueryString, canonicalHeaders}
	canonicalReq := strings.Join(canonicalParts, SIGN_JOINER)
	log.Debug("CanonicalRequest data:\n" + canonicalReq)
	signature := util.HmacSha256Hex(signKey, canonicalReq)

	// Generate auth string and add to the reqeust header
	authStr := signKeyInfo + "/" + signedHeaders + "/" + signature
	log.Info("Authorization=" + authStr)

	req.SetHeader(http.AUTHORIZATION, authStr)
}

func getCanonicalURIPath(path string) string {
	if len(path) == 0 {
		return "/"
	}
	canonical_path := path
	if strings.HasPrefix(path, "/") {
		canonical_path = path[1:]
	}
	canonical_path = util.UriEncode(canonical_path, false)
	return "/" + canonical_path
}

func getCanonicalQueryString(params map[string]string) string {
	if len(params) == 0 {
		return ""
	}

	result := make([]string, 0, len(params))
	for k, v := range params {
		if strings.ToLower(k) == strings.ToLower(http.AUTHORIZATION) {
			continue
		}
		item := ""
		if len(v) == 0 {
			item = fmt.Sprintf("%s=", util.UriEncode(k, true))
		} else {
			item = fmt.Sprintf("%s=%s", util.UriEncode(k, true), util.UriEncode(v, true))
		}
		result = append(result, item)
	}
	sort.Strings(result)
	return strings.Join(result, "&")
}

func getCanonicalHeaders(headers map[string]string,
	headersToSign map[string]struct{}) (string, []string) {
	canonicalHeaders := make([]string, 0, len(headers))
	signHeaders := make([]string, 0, len(headersToSign))
	for k, v := range headers {
		headKey := strings.ToLower(k)
		if headKey == strings.ToLower(http.AUTHORIZATION) {
			continue
		}
		_, headExists := headersToSign[headKey]
		if headExists ||
			(strings.HasPrefix(headKey, http.BCE_PREFIX) &&
				(headKey != http.BCE_REQUEST_ID)) {

			headVal := strings.TrimSpace(v)
			encoded := util.UriEncode(headKey, true) + ":" + util.UriEncode(headVal, true)
			canonicalHeaders = append(canonicalHeaders, encoded)
			signHeaders = append(signHeaders, headKey)
		}
	}
	sort.Strings(canonicalHeaders)
	sort.Strings(signHeaders)
	return strings.Join(canonicalHeaders, SIGN_JOINER), signHeaders
}
