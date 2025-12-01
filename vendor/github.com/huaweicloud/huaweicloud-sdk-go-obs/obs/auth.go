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
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

func setURLWithPolicy(bucketName, canonicalizedUrl string) string {
	if strings.HasPrefix(canonicalizedUrl, "/"+bucketName+"/") {
		canonicalizedUrl = canonicalizedUrl[len("/"+bucketName+"/"):]
	} else if strings.HasPrefix(canonicalizedUrl, "/"+bucketName) {
		canonicalizedUrl = canonicalizedUrl[len("/"+bucketName):]
	}
	return canonicalizedUrl
}

func (obsClient ObsClient) doAuthTemporary(method, bucketName, objectKey string, policy string, params map[string]string,
	headers map[string][]string, expires int64) (requestURL string, err error) {
	sh := obsClient.getSecurity()
	isAkSkEmpty := sh.ak == "" || sh.sk == ""
	if isAkSkEmpty == false && sh.securityToken != "" {
		if obsClient.conf.signature == SignatureObs {
			params[HEADER_STS_TOKEN_OBS] = sh.securityToken
		} else {
			params[HEADER_STS_TOKEN_AMZ] = sh.securityToken
		}
	}

	if policy != "" {
		objectKey = ""
	}

	requestURL, canonicalizedURL := obsClient.conf.formatUrls(bucketName, objectKey, params, true)
	parsedRequestURL, err := url.Parse(requestURL)
	if err != nil {
		return "", err
	}
	encodeHeaders(headers)
	hostName := parsedRequestURL.Host

	isV4 := obsClient.conf.signature == SignatureV4
	prepareHostAndDate(headers, hostName, isV4)

	if isAkSkEmpty {
		doLog(LEVEL_WARN, "No ak/sk provided, skip to construct authorization")
	} else {
		if isV4 {
			date, parseDateErr := time.Parse(RFC1123_FORMAT, headers[HEADER_DATE_CAMEL][0])
			if parseDateErr != nil {
				doLog(LEVEL_WARN, "Failed to parse date with reason: %v", parseDateErr)
				return "", parseDateErr
			}
			delete(headers, HEADER_DATE_CAMEL)
			shortDate := date.Format(SHORT_DATE_FORMAT)
			longDate := date.Format(LONG_DATE_FORMAT)
			if len(headers[HEADER_HOST_CAMEL]) != 0 {
				index := strings.LastIndex(headers[HEADER_HOST_CAMEL][0], ":")
				if index != -1 {
					port := headers[HEADER_HOST_CAMEL][0][index+1:]
					if port == "80" || port == "443" {
						headers[HEADER_HOST_CAMEL] = []string{headers[HEADER_HOST_CAMEL][0][:index]}
					}
				}

			}

			signedHeaders, _headers := getSignedHeaders(headers)

			credential, scope := getCredential(sh.ak, obsClient.conf.region, shortDate)
			params[PARAM_ALGORITHM_AMZ_CAMEL] = V4_HASH_PREFIX
			params[PARAM_CREDENTIAL_AMZ_CAMEL] = credential
			params[PARAM_DATE_AMZ_CAMEL] = longDate
			params[PARAM_EXPIRES_AMZ_CAMEL] = Int64ToString(expires)
			params[PARAM_SIGNEDHEADERS_AMZ_CAMEL] = strings.Join(signedHeaders, ";")

			requestURL, canonicalizedURL = obsClient.conf.formatUrls(bucketName, objectKey, params, true)
			parsedRequestURL, _err := url.Parse(requestURL)
			if _err != nil {
				return "", _err
			}

			stringToSign := getV4StringToSign(method, canonicalizedURL, parsedRequestURL.RawQuery, scope, longDate, UNSIGNED_PAYLOAD, signedHeaders, _headers)
			signature := getSignature(stringToSign, sh.sk, obsClient.conf.region, shortDate)

			requestURL += fmt.Sprintf("&%s=%s", PARAM_SIGNATURE_AMZ_CAMEL, UrlEncode(signature, false))

		} else {
			originDate := headers[HEADER_DATE_CAMEL][0]
			date, parseDateErr := time.Parse(RFC1123_FORMAT, originDate)
			if parseDateErr != nil {
				doLog(LEVEL_WARN, "Failed to parse date with reason: %v", parseDateErr)
				return "", parseDateErr
			}
			expires += date.Unix()
			if policy == "" {
				headers[HEADER_DATE_CAMEL] = []string{Int64ToString(expires)}
			} else {
				policy = Base64Encode([]byte(policy))
				headers[HEADER_DATE_CAMEL] = []string{policy}
				canonicalizedURL = setURLWithPolicy(bucketName, canonicalizedURL)
			}

			stringToSign := getV2StringToSign(method, canonicalizedURL, headers, obsClient.conf.signature == SignatureObs)
			signature := UrlEncode(Base64Encode(HmacSha1([]byte(sh.sk), []byte(stringToSign))), false)
			if strings.Index(requestURL, "?") < 0 {
				requestURL += "?"
			} else {
				requestURL += "&"
			}
			delete(headers, HEADER_DATE_CAMEL)

			if obsClient.conf.signature != SignatureObs {
				requestURL += "AWS"
			}
			if policy == "" {
				requestURL += fmt.Sprintf("AccessKeyId=%s&Expires=%d&Signature=%s", UrlEncode(sh.ak, false),
					expires, signature)
				return

			}
			requestURL += fmt.Sprintf("AccessKeyId=%s&Policy=%s&Signature=%s", UrlEncode(sh.ak, false),
				UrlEncode(policy, false), signature)
		}
	}

	return
}

func (obsClient ObsClient) doAuth(method, bucketName, objectKey string, params map[string]string,
	headers map[string][]string, hostName string) (requestURL string, err error) {
	sh := obsClient.getSecurity()
	isAkSkEmpty := sh.ak == "" || sh.sk == ""
	if isAkSkEmpty == false && sh.securityToken != "" {
		if obsClient.conf.signature == SignatureObs {
			headers[HEADER_STS_TOKEN_OBS] = []string{sh.securityToken}
		} else {
			headers[HEADER_STS_TOKEN_AMZ] = []string{sh.securityToken}
		}
	}
	isObs := obsClient.conf.signature == SignatureObs
	requestURL, canonicalizedURL := obsClient.conf.formatUrls(bucketName, objectKey, params, true)
	parsedRequestURL, err := url.Parse(requestURL)
	if err != nil {
		return "", err
	}
	encodeHeaders(headers)

	if hostName == "" {
		hostName = parsedRequestURL.Host
	}

	isV4 := obsClient.conf.signature == SignatureV4
	prepareHostAndDate(headers, hostName, isV4)

	if isAkSkEmpty {
		doLog(LEVEL_WARN, "No ak/sk provided, skip to construct authorization")
	} else {
		ak := sh.ak
		sk := sh.sk
		var authorization string
		if isV4 {
			headers[HEADER_CONTENT_SHA256_AMZ] = []string{UNSIGNED_PAYLOAD}
			ret := v4Auth(ak, sk, obsClient.conf.region, method, canonicalizedURL, parsedRequestURL.RawQuery, headers)
			authorization = fmt.Sprintf("%s Credential=%s,SignedHeaders=%s,Signature=%s", V4_HASH_PREFIX, ret["Credential"], ret["SignedHeaders"], ret["Signature"])
		} else {
			ret := v2Auth(ak, sk, method, canonicalizedURL, headers, isObs)
			hashPrefix := V2_HASH_PREFIX
			if isObs {
				hashPrefix = OBS_HASH_PREFIX
			}
			authorization = fmt.Sprintf("%s %s:%s", hashPrefix, ak, ret["Signature"])
		}
		headers[HEADER_AUTH_CAMEL] = []string{authorization}
	}
	return
}

func prepareHostAndDate(headers map[string][]string, hostName string, isV4 bool) {
	headers[HEADER_HOST_CAMEL] = []string{hostName}
	if date, ok := headers[HEADER_DATE_AMZ]; ok {
		flag := false
		if len(date) == 1 {
			if isV4 {
				if t, err := time.Parse(LONG_DATE_FORMAT, date[0]); err == nil {
					headers[HEADER_DATE_CAMEL] = []string{FormatUtcToRfc1123(t)}
					flag = true
				}
			} else {
				if strings.HasSuffix(date[0], "GMT") {
					headers[HEADER_DATE_CAMEL] = []string{date[0]}
					flag = true
				}
			}
		}
		if !flag {
			delete(headers, HEADER_DATE_AMZ)
		}
	}
	if _, ok := headers[HEADER_DATE_CAMEL]; !ok {
		headers[HEADER_DATE_CAMEL] = []string{FormatUtcToRfc1123(time.Now().UTC())}
	}

}

func encodeHeaders(headers map[string][]string) {
	for key, values := range headers {
		for index, value := range values {
			values[index] = UrlEncode(value, true)
		}
		headers[key] = values
	}
}

func prepareDateHeader(dataHeader, dateCamelHeader string, headers, _headers map[string][]string) {
	if _, ok := _headers[HEADER_DATE_CAMEL]; ok {
		if _, ok := _headers[dataHeader]; ok {
			_headers[HEADER_DATE_CAMEL] = []string{""}
		} else if _, ok := headers[dateCamelHeader]; ok {
			_headers[HEADER_DATE_CAMEL] = []string{""}
		}
	} else if _, ok := _headers[strings.ToLower(HEADER_DATE_CAMEL)]; ok {
		if _, ok := _headers[dataHeader]; ok {
			_headers[HEADER_DATE_CAMEL] = []string{""}
		} else if _, ok := headers[dateCamelHeader]; ok {
			_headers[HEADER_DATE_CAMEL] = []string{""}
		}
	}
}

func getStringToSign(keys []string, isObs bool, _headers map[string][]string) []string {
	stringToSign := make([]string, 0, len(keys))
	for _, key := range keys {
		var value string
		prefixHeader := HEADER_PREFIX
		prefixMetaHeader := HEADER_PREFIX_META
		if isObs {
			prefixHeader = HEADER_PREFIX_OBS
			prefixMetaHeader = HEADER_PREFIX_META_OBS
		}
		if strings.HasPrefix(key, prefixHeader) {
			if strings.HasPrefix(key, prefixMetaHeader) {
				for index, v := range _headers[key] {
					value += strings.TrimSpace(v)
					if index != len(_headers[key])-1 {
						value += ","
					}
				}
			} else {
				value = strings.Join(_headers[key], ",")
			}
			value = fmt.Sprintf("%s:%s", key, value)
		} else {
			value = strings.Join(_headers[key], ",")
		}
		stringToSign = append(stringToSign, value)
	}
	return stringToSign
}

func attachHeaders(headers map[string][]string, isObs bool) string {
	length := len(headers)
	_headers := make(map[string][]string, length)
	keys := make([]string, 0, length)

	for key, value := range headers {
		_key := strings.ToLower(strings.TrimSpace(key))
		if _key != "" {
			prefixheader := HEADER_PREFIX
			if isObs {
				prefixheader = HEADER_PREFIX_OBS
			}
			if _key == "content-md5" || _key == "content-type" || _key == "date" || strings.HasPrefix(_key, prefixheader) {
				keys = append(keys, _key)
				_headers[_key] = value
			}
		} else {
			delete(headers, key)
		}
	}

	for _, interestedHeader := range interestedHeaders {
		if _, ok := _headers[interestedHeader]; !ok {
			_headers[interestedHeader] = []string{""}
			keys = append(keys, interestedHeader)
		}
	}
	dateCamelHeader := PARAM_DATE_AMZ_CAMEL
	dataHeader := HEADER_DATE_AMZ
	if isObs {
		dateCamelHeader = PARAM_DATE_OBS_CAMEL
		dataHeader = HEADER_DATE_OBS
	}
	prepareDateHeader(dataHeader, dateCamelHeader, headers, _headers)

	sort.Strings(keys)
	stringToSign := getStringToSign(keys, isObs, _headers)
	return strings.Join(stringToSign, "\n")
}

func getScope(region, shortDate string) string {
	return fmt.Sprintf("%s/%s/%s/%s", shortDate, region, V4_SERVICE_NAME, V4_SERVICE_SUFFIX)
}

func getCredential(ak, region, shortDate string) (string, string) {
	scope := getScope(region, shortDate)
	return fmt.Sprintf("%s/%s", ak, scope), scope
}

func getSignedHeaders(headers map[string][]string) ([]string, map[string][]string) {
	length := len(headers)
	_headers := make(map[string][]string, length)
	signedHeaders := make([]string, 0, length)
	for key, value := range headers {
		_key := strings.ToLower(strings.TrimSpace(key))
		if _key != "" {
			signedHeaders = append(signedHeaders, _key)
			_headers[_key] = value
		} else {
			delete(headers, key)
		}
	}
	sort.Strings(signedHeaders)
	return signedHeaders, _headers
}

func getSignature(stringToSign, sk, region, shortDate string) string {
	key := HmacSha256([]byte(V4_HASH_PRE+sk), []byte(shortDate))
	key = HmacSha256(key, []byte(region))
	key = HmacSha256(key, []byte(V4_SERVICE_NAME))
	key = HmacSha256(key, []byte(V4_SERVICE_SUFFIX))
	return Hex(HmacSha256(key, []byte(stringToSign)))
}
