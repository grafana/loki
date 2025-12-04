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
)

func getV2StringToSign(method, canonicalizedURL string, headers map[string][]string, isObs bool) string {
	stringToSign := strings.Join([]string{method, "\n", attachHeaders(headers, isObs), "\n", canonicalizedURL}, "")

	var isSecurityToken bool
	var securityToken []string
	if isObs {
		securityToken, isSecurityToken = headers[HEADER_STS_TOKEN_OBS]
	} else {
		securityToken, isSecurityToken = headers[HEADER_STS_TOKEN_AMZ]
	}
	var query []string
	if !isSecurityToken {
		parmas := strings.Split(canonicalizedURL, "?")
		if len(parmas) > 1 {
			query = strings.Split(parmas[1], "&")
			for _, value := range query {
				if strings.HasPrefix(value, HEADER_STS_TOKEN_AMZ+"=") || strings.HasPrefix(value, HEADER_STS_TOKEN_OBS+"=") {
					if value[len(HEADER_STS_TOKEN_AMZ)+1:] != "" {
						securityToken = []string{value[len(HEADER_STS_TOKEN_AMZ)+1:]}
						isSecurityToken = true
					}
				}
			}
		}
	}
	logStringToSign := stringToSign
	if isSecurityToken && len(securityToken) > 0 {
		logStringToSign = strings.Replace(logStringToSign, securityToken[0], "******", -1)
	}
	doLog(LEVEL_DEBUG, "The v2 auth stringToSign:\n%s", logStringToSign)
	return stringToSign
}

func v2Auth(ak, sk, method, canonicalizedURL string, headers map[string][]string, isObs bool) map[string]string {
	stringToSign := getV2StringToSign(method, canonicalizedURL, headers, isObs)
	return map[string]string{"Signature": Base64Encode(HmacSha1([]byte(sk), []byte(stringToSign)))}
}
