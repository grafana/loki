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
	"strings"
	"time"
)

func getV4StringToSign(method, canonicalizedURL, queryURL, scope, longDate, payload string, signedHeaders []string, headers map[string][]string) string {
	canonicalRequest := make([]string, 0, 10+len(signedHeaders)*4)
	canonicalRequest = append(canonicalRequest, method)
	canonicalRequest = append(canonicalRequest, "\n")
	canonicalRequest = append(canonicalRequest, canonicalizedURL)
	canonicalRequest = append(canonicalRequest, "\n")
	canonicalRequest = append(canonicalRequest, queryURL)
	canonicalRequest = append(canonicalRequest, "\n")

	for _, signedHeader := range signedHeaders {
		values, _ := headers[signedHeader]
		for _, value := range values {
			canonicalRequest = append(canonicalRequest, signedHeader)
			canonicalRequest = append(canonicalRequest, ":")
			canonicalRequest = append(canonicalRequest, value)
			canonicalRequest = append(canonicalRequest, "\n")
		}
	}
	canonicalRequest = append(canonicalRequest, "\n")
	canonicalRequest = append(canonicalRequest, strings.Join(signedHeaders, ";"))
	canonicalRequest = append(canonicalRequest, "\n")
	canonicalRequest = append(canonicalRequest, payload)

	_canonicalRequest := strings.Join(canonicalRequest, "")

	var isSecurityToken bool
	var securityToken []string
	if securityToken, isSecurityToken = headers[HEADER_STS_TOKEN_OBS]; !isSecurityToken {
		securityToken, isSecurityToken = headers[HEADER_STS_TOKEN_AMZ]
	}
	var query []string
	if !isSecurityToken {
		query = strings.Split(queryURL, "&")
		for _, value := range query {
			if strings.HasPrefix(value, HEADER_STS_TOKEN_AMZ+"=") || strings.HasPrefix(value, HEADER_STS_TOKEN_OBS+"=") {
				if value[len(HEADER_STS_TOKEN_AMZ)+1:] != "" {
					securityToken = []string{value[len(HEADER_STS_TOKEN_AMZ)+1:]}
					isSecurityToken = true
				}
			}
		}
	}
	logCanonicalRequest := _canonicalRequest
	if isSecurityToken && len(securityToken) > 0 {
		logCanonicalRequest = strings.Replace(logCanonicalRequest, securityToken[0], "******", -1)
	}
	doLog(LEVEL_DEBUG, "The v4 auth canonicalRequest:\n%s", logCanonicalRequest)

	stringToSign := make([]string, 0, 7)
	stringToSign = append(stringToSign, V4_HASH_PREFIX)
	stringToSign = append(stringToSign, "\n")
	stringToSign = append(stringToSign, longDate)
	stringToSign = append(stringToSign, "\n")
	stringToSign = append(stringToSign, scope)
	stringToSign = append(stringToSign, "\n")
	stringToSign = append(stringToSign, HexSha256([]byte(_canonicalRequest)))

	_stringToSign := strings.Join(stringToSign, "")

	return _stringToSign
}

// V4Auth is a wrapper for v4Auth
func V4Auth(ak, sk, region, method, canonicalizedURL, queryURL string, headers map[string][]string) map[string]string {
	return v4Auth(ak, sk, region, method, canonicalizedURL, queryURL, headers)
}

func v4Auth(ak, sk, region, method, canonicalizedURL, queryURL string, headers map[string][]string) map[string]string {
	var t time.Time
	if val, ok := headers[HEADER_DATE_AMZ]; ok {
		var err error
		t, err = time.Parse(LONG_DATE_FORMAT, val[0])
		if err != nil {
			t = time.Now().UTC()
		}
	} else if val, ok := headers[PARAM_DATE_AMZ_CAMEL]; ok {
		var err error
		t, err = time.Parse(LONG_DATE_FORMAT, val[0])
		if err != nil {
			t = time.Now().UTC()
		}
	} else if val, ok := headers[HEADER_DATE_CAMEL]; ok {
		var err error
		t, err = time.Parse(RFC1123_FORMAT, val[0])
		if err != nil {
			t = time.Now().UTC()
		}
	} else if val, ok := headers[strings.ToLower(HEADER_DATE_CAMEL)]; ok {
		var err error
		t, err = time.Parse(RFC1123_FORMAT, val[0])
		if err != nil {
			t = time.Now().UTC()
		}
	} else {
		t = time.Now().UTC()
	}
	shortDate := t.Format(SHORT_DATE_FORMAT)
	longDate := t.Format(LONG_DATE_FORMAT)

	signedHeaders, _headers := getSignedHeaders(headers)

	credential, scope := getCredential(ak, region, shortDate)

	payload := UNSIGNED_PAYLOAD
	if val, ok := headers[HEADER_CONTENT_SHA256_AMZ]; ok {
		payload = val[0]
	}
	stringToSign := getV4StringToSign(method, canonicalizedURL, queryURL, scope, longDate, payload, signedHeaders, _headers)

	signature := getSignature(stringToSign, sk, region, shortDate)

	ret := make(map[string]string, 3)
	ret["Credential"] = credential
	ret["SignedHeaders"] = strings.Join(signedHeaders, ";")
	ret["Signature"] = signature
	return ret
}
