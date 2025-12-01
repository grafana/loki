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
	"errors"
	"fmt"
	"strings"
	"time"
)

func (obsClient ObsClient) isSecurityToken(params map[string]string, sh securityHolder) {
	if sh.securityToken != "" {
		if obsClient.conf.signature == SignatureObs {
			params[HEADER_STS_TOKEN_OBS] = sh.securityToken
		} else {
			params[HEADER_STS_TOKEN_AMZ] = sh.securityToken
		}
	}
}

// CreateBrowserBasedSignature gets the browser based signature with the specified CreateBrowserBasedSignatureInput,
// and returns the CreateBrowserBasedSignatureOutput and error
func (obsClient ObsClient) CreateBrowserBasedSignature(input *CreateBrowserBasedSignatureInput) (output *CreateBrowserBasedSignatureOutput, err error) {
	if input == nil {
		return nil, errors.New("CreateBrowserBasedSignatureInput is nil")
	}

	params := make(map[string]string, len(input.FormParams))
	for key, value := range input.FormParams {
		params[key] = value
	}

	date := time.Now().UTC()
	shortDate := date.Format(SHORT_DATE_FORMAT)
	longDate := date.Format(LONG_DATE_FORMAT)
	sh := obsClient.getSecurity()

	credential, _ := getCredential(sh.ak, obsClient.conf.region, shortDate)

	if input.Expires <= 0 {
		input.Expires = 300
	}

	expiration := date.Add(time.Second * time.Duration(input.Expires)).Format(ISO8601_DATE_FORMAT)
	if obsClient.conf.signature == SignatureV4 {
		params[PARAM_ALGORITHM_AMZ_CAMEL] = V4_HASH_PREFIX
		params[PARAM_CREDENTIAL_AMZ_CAMEL] = credential
		params[PARAM_DATE_AMZ_CAMEL] = longDate
	}

	obsClient.isSecurityToken(params, sh)

	matchAnyBucket := true
	matchAnyKey := true
	count := 5
	if bucket := strings.TrimSpace(input.Bucket); bucket != "" {
		params["bucket"] = bucket
		matchAnyBucket = false
		count--
	}

	if key := strings.TrimSpace(input.Key); key != "" {
		params["key"] = key
		matchAnyKey = false
		count--
	}

	originPolicySlice := make([]string, 0, len(params)+count)
	originPolicySlice = append(originPolicySlice, fmt.Sprintf("{\"expiration\":\"%s\",", expiration))
	originPolicySlice = append(originPolicySlice, "\"conditions\":[")
	for key, value := range params {
		if _key := strings.TrimSpace(strings.ToLower(key)); _key != "" {
			originPolicySlice = append(originPolicySlice, fmt.Sprintf("{\"%s\":\"%s\"},", _key, value))
		}
	}

	if matchAnyBucket {
		originPolicySlice = append(originPolicySlice, "[\"starts-with\", \"$bucket\", \"\"],")
	}

	if matchAnyKey {
		originPolicySlice = append(originPolicySlice, "[\"starts-with\", \"$key\", \"\"],")
	}

	for _, v := range input.RangeParams {
		originPolicySlice = append(originPolicySlice, fmt.Sprintf("[\"%s\", %d, %d],", v.RangeName, v.Lower, v.Upper))
	}

	lastIndex := len(originPolicySlice) - 1
	originPolicySlice[lastIndex] = strings.TrimSuffix(originPolicySlice[lastIndex], ",")

	originPolicySlice = append(originPolicySlice, "]}")

	originPolicy := strings.Join(originPolicySlice, "")

	policy := Base64Encode([]byte(originPolicy))
	var signature string
	if obsClient.conf.signature == SignatureV4 {
		signature = getSignature(policy, sh.sk, obsClient.conf.region, shortDate)
	} else {
		signature = Base64Encode(HmacSha1([]byte(sh.sk), []byte(policy)))
	}

	output = &CreateBrowserBasedSignatureOutput{
		OriginPolicy: originPolicy,
		Policy:       policy,
		Algorithm:    params[PARAM_ALGORITHM_AMZ_CAMEL],
		Credential:   params[PARAM_CREDENTIAL_AMZ_CAMEL],
		Date:         params[PARAM_DATE_AMZ_CAMEL],
		Signature:    signature,
	}
	return
}
