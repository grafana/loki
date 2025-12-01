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
	"bytes"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var regex = regexp.MustCompile("^[\u4e00-\u9fa5]$")
var ipRegex = regexp.MustCompile("^((2[0-4]\\d|25[0-5]|[01]?\\d\\d?)\\.){3}(2[0-4]\\d|25[0-5]|[01]?\\d\\d?)$")
var v4AuthRegex = regexp.MustCompile("Credential=(.+?),SignedHeaders=(.+?),Signature=.+")
var regionRegex = regexp.MustCompile(".+/\\d+/(.+?)/.+")

// StringContains replaces subStr in src with subTranscoding and returns the new string
func StringContains(src string, subStr string, subTranscoding string) string {
	return strings.Replace(src, subStr, subTranscoding, -1)
}

// XmlTranscoding replaces special characters with their escaped form
func XmlTranscoding(src string) string {
	srcTmp := StringContains(src, "&", "&amp;")
	srcTmp = StringContains(srcTmp, "<", "&lt;")
	srcTmp = StringContains(srcTmp, ">", "&gt;")
	srcTmp = StringContains(srcTmp, "'", "&apos;")
	srcTmp = StringContains(srcTmp, "\"", "&quot;")
	return srcTmp
}

func HandleHttpResponse(action string, headers map[string][]string, output IBaseModel, resp *http.Response, xmlResult bool, isObs bool) (err error) {
	if IsHandleCallbackResponse(action, headers, isObs) {
		if err = ParseCallbackResponseToBaseModel(resp, output, isObs); err != nil {
			doLog(LEVEL_WARN, "Parse callback response to BaseModel with error: %v", err)
		}
	} else {
		if err = ParseResponseToBaseModel(resp, output, xmlResult, isObs); err != nil {
			doLog(LEVEL_WARN, "Parse response to BaseModel with error: %v", err)
		}
	}
	return
}

func IsHandleCallbackResponse(action string, headers map[string][]string, isObs bool) bool {
	var headerPrefix = HEADER_PREFIX
	if isObs == true {
		headerPrefix = HEADER_PREFIX_OBS
	}
	supportCallbackActions := []string{PUT_OBJECT, PUT_FILE, "CompleteMultipartUpload"}
	return len(headers[headerPrefix+CALLBACK]) != 0 && IsContain(supportCallbackActions, action)
}

func IsContain(items []string, item string) bool {
	for _, eachItem := range items {
		if eachItem == item {
			return true
		}
	}
	return false
}

// StringToInt converts string value to int value with default value
func StringToInt(value string, def int) int {
	ret, err := strconv.Atoi(value)
	if err != nil {
		ret = def
	}
	return ret
}

// StringToInt64 converts string value to int64 value with default value
func StringToInt64(value string, def int64) int64 {
	ret, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		ret = def
	}
	return ret
}

// IntToString converts int value to string value
func IntToString(value int) string {
	return strconv.Itoa(value)
}

// Int64ToString converts int64 value to string value
func Int64ToString(value int64) string {
	return strconv.FormatInt(value, 10)
}

// GetCurrentTimestamp gets unix time in milliseconds
func GetCurrentTimestamp() int64 {
	return time.Now().UnixNano() / 1000000
}

// FormatUtcNow gets a textual representation of the UTC format time value
func FormatUtcNow(format string) string {
	return time.Now().UTC().Format(format)
}

// FormatNowWithLoc gets a textual representation of the format time value with loc
func FormatNowWithLoc(format string, loc *time.Location) string {
	return time.Now().In(loc).Format(format)
}

// FormatUtcToRfc1123 gets a textual representation of the RFC1123 format time value
func FormatUtcToRfc1123(t time.Time) string {
	ret := t.UTC().Format(time.RFC1123)
	return ret[:strings.LastIndex(ret, "UTC")] + "GMT"
}

// Md5 gets the md5 value of input
func Md5(value []byte) []byte {
	m := md5.New()
	_, err := m.Write(value)
	if err != nil {
		doLog(LEVEL_WARN, "MD5 failed to write")
	}
	return m.Sum(nil)
}

// HmacSha1 gets hmac sha1 value of input
func HmacSha1(key, value []byte) []byte {
	mac := hmac.New(sha1.New, key)
	_, err := mac.Write(value)
	if err != nil {
		doLog(LEVEL_WARN, "HmacSha1 failed to write")
	}
	return mac.Sum(nil)
}

// HmacSha256 get hmac sha256 value if input
func HmacSha256(key, value []byte) []byte {
	mac := hmac.New(sha256.New, key)
	_, err := mac.Write(value)
	if err != nil {
		doLog(LEVEL_WARN, "HmacSha256 failed to write")
	}
	return mac.Sum(nil)
}

// Base64Encode wrapper of base64.StdEncoding.EncodeToString
func Base64Encode(value []byte) string {
	return base64.StdEncoding.EncodeToString(value)
}

// Base64Decode wrapper of base64.StdEncoding.DecodeString
func Base64Decode(value string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(value)
}

// HexMd5 returns the md5 value of input in hexadecimal format
func HexMd5(value []byte) string {
	return Hex(Md5(value))
}

// Base64Md5 returns the md5 value of input with Base64Encode
func Base64Md5(value []byte) string {
	return Base64Encode(Md5(value))
}

// Base64Md5OrSha256 returns the md5 or sha256 value of input with Base64Encode
func Base64Md5OrSha256(value []byte, enableSha256 bool) string {
	if enableSha256 {
		return Base64Sha256(value)
	}
	return Base64Md5(value)
}

// Base64Sha256 returns the sha256 value of input with Base64Encode
func Base64Sha256(value []byte) string {
	return Base64Encode(Sha256Hash(value))
}

// Sha256Hash returns sha256 checksum
func Sha256Hash(value []byte) []byte {
	hash := sha256.New()
	_, err := hash.Write(value)
	if err != nil {
		doLog(LEVEL_WARN, "Sha256Hash failed to write")
	}
	return hash.Sum(nil)
}

// ParseXml wrapper of xml.Unmarshal
func ParseXml(value []byte, result interface{}) error {
	if len(value) == 0 {
		return nil
	}
	return xml.Unmarshal(value, result)
}

// parseJSON wrapper of json.Unmarshal
func parseJSON(value []byte, result interface{}) error {
	if len(value) == 0 {
		return nil
	}
	return json.Unmarshal(value, result)
}

// TransToXml wrapper of xml.Marshal
func TransToXml(value interface{}) ([]byte, error) {
	if value == nil {
		return []byte{}, nil
	}
	return xml.Marshal(value)
}

// TransToJSON wrapper of json.Marshal
func TransToJSON(value interface{}) ([]byte, error) {
	if value == nil {
		return []byte{}, nil
	}
	return json.Marshal(value)
}

// Hex wrapper of hex.EncodeToString
func Hex(value []byte) string {
	return hex.EncodeToString(value)
}

// HexSha256 returns the Sha256Hash value of input in hexadecimal format
func HexSha256(value []byte) string {
	return Hex(Sha256Hash(value))
}

// UrlDecode wrapper of url.QueryUnescape
func UrlDecode(value string) (string, error) {
	ret, err := url.QueryUnescape(value)
	if err == nil {
		return ret, nil
	}
	return "", err
}

// UrlDecodeWithoutError wrapper of UrlDecode
func UrlDecodeWithoutError(value string) string {
	ret, err := UrlDecode(value)
	if err == nil {
		return ret
	}
	if isErrorLogEnabled() {
		doLog(LEVEL_ERROR, "Url decode error")
	}
	return ""
}

// IsIP checks whether the value matches ip address
func IsIP(value string) bool {
	return ipRegex.MatchString(value)
}

// UrlEncode encodes the input value
func UrlEncode(value string, chineseOnly bool) string {
	if chineseOnly {
		values := make([]string, 0, len(value))
		for _, val := range value {
			_value := string(val)
			if regex.MatchString(_value) {
				_value = url.QueryEscape(_value)
			}
			values = append(values, _value)
		}
		return strings.Join(values, "")
	}
	return url.QueryEscape(value)
}

func copyHeaders(m map[string][]string) (ret map[string][]string) {
	if m != nil {
		ret = make(map[string][]string, len(m))
		for key, values := range m {
			_values := make([]string, 0, len(values))
			for _, value := range values {
				_values = append(_values, value)
			}
			ret[strings.ToLower(key)] = _values
		}
	} else {
		ret = make(map[string][]string)
	}

	return
}

func parseHeaders(headers map[string][]string) (signature string, region string, signedHeaders string) {
	signature = "v2"
	if receviedAuthorization, ok := headers[strings.ToLower(HEADER_AUTH_CAMEL)]; ok && len(receviedAuthorization) > 0 {
		if strings.HasPrefix(receviedAuthorization[0], V4_HASH_PREFIX) {
			signature = "v4"
			matches := v4AuthRegex.FindStringSubmatch(receviedAuthorization[0])
			if len(matches) >= 3 {
				region = matches[1]
				regions := regionRegex.FindStringSubmatch(region)
				if len(regions) >= 2 {
					region = regions[1]
				}
				signedHeaders = matches[2]
			}

		} else if strings.HasPrefix(receviedAuthorization[0], V2_HASH_PREFIX) {
			signature = "v2"
		}
	}
	return
}

func getTemporaryKeys() []string {
	return []string{
		"Signature",
		"signature",
		"X-Amz-Signature",
		"x-amz-signature",
	}
}

func getIsObs(isTemporary bool, querys []string, headers map[string][]string) bool {
	isObs := true
	if isTemporary {
		for _, value := range querys {
			keyPrefix := strings.ToLower(value)
			if strings.HasPrefix(keyPrefix, HEADER_PREFIX) {
				isObs = false
			} else if strings.HasPrefix(value, HEADER_ACCESSS_KEY_AMZ) {
				isObs = false
			}
		}
	} else {
		for key := range headers {
			keyPrefix := strings.ToLower(key)
			if strings.HasPrefix(keyPrefix, HEADER_PREFIX) {
				isObs = false
				break
			}
		}
	}
	return isObs
}

func isPathStyle(headers map[string][]string, bucketName string) bool {
	if receviedHost, ok := headers[HEADER_HOST]; ok && len(receviedHost) > 0 && !strings.HasPrefix(receviedHost[0], bucketName+".") {
		return true
	}
	return false
}

// GetV2Authorization v2 Authorization
func GetV2Authorization(ak, sk, method, bucketName, objectKey, queryURL string, headers map[string][]string) (ret map[string]string) {

	if strings.HasPrefix(queryURL, "?") {
		queryURL = queryURL[1:]
	}

	method = strings.ToUpper(method)

	querys := strings.Split(queryURL, "&")
	querysResult := make([]string, 0)
	for _, value := range querys {
		if value != "=" && len(value) != 0 {
			querysResult = append(querysResult, value)
		}
	}
	params := make(map[string]string)

	for _, value := range querysResult {
		kv := strings.Split(value, "=")
		length := len(kv)
		if length == 1 {
			key := UrlDecodeWithoutError(kv[0])
			params[key] = ""
		} else if length >= 2 {
			key := UrlDecodeWithoutError(kv[0])
			vals := make([]string, 0, length-1)
			for i := 1; i < length; i++ {
				val := UrlDecodeWithoutError(kv[i])
				vals = append(vals, val)
			}
			params[key] = strings.Join(vals, "=")
		}
	}
	headers = copyHeaders(headers)
	pathStyle := isPathStyle(headers, bucketName)
	conf := &config{securityProviders: []securityProvider{NewBasicSecurityProvider(ak, sk, "")},
		urlHolder: &urlHolder{scheme: "https", host: "dummy", port: 443},
		pathStyle: pathStyle}
	conf.signature = SignatureObs
	_, canonicalizedURL := conf.formatUrls(bucketName, objectKey, params, false)
	ret = v2Auth(ak, sk, method, canonicalizedURL, headers, true)
	v2HashPrefix := OBS_HASH_PREFIX
	ret[HEADER_AUTH_CAMEL] = fmt.Sprintf("%s %s:%s", v2HashPrefix, ak, ret["Signature"])
	return
}

func getQuerysResult(querys []string) []string {
	querysResult := make([]string, 0)
	for _, value := range querys {
		if value != "=" && len(value) != 0 {
			querysResult = append(querysResult, value)
		}
	}
	return querysResult
}

func getParams(querysResult []string) map[string]string {
	params := make(map[string]string)
	for _, value := range querysResult {
		kv := strings.Split(value, "=")
		length := len(kv)
		if length == 1 {
			key := UrlDecodeWithoutError(kv[0])
			params[key] = ""
		} else if length >= 2 {
			key := UrlDecodeWithoutError(kv[0])
			vals := make([]string, 0, length-1)
			for i := 1; i < length; i++ {
				val := UrlDecodeWithoutError(kv[i])
				vals = append(vals, val)
			}
			params[key] = strings.Join(vals, "=")
		}
	}
	return params
}

func getTemporaryAndSignature(params map[string]string) (bool, string) {
	isTemporary := false
	signature := "v2"
	temporaryKeys := getTemporaryKeys()
	for _, key := range temporaryKeys {
		if _, ok := params[key]; ok {
			isTemporary = true
			if strings.ToLower(key) == "signature" {
				signature = "v2"
			} else if strings.ToLower(key) == "x-amz-signature" {
				signature = "v4"
			}
			break
		}
	}
	return isTemporary, signature
}

// GetAuthorization Authorization
func GetAuthorization(ak, sk, method, bucketName, objectKey, queryURL string, headers map[string][]string) (ret map[string]string) {

	if strings.HasPrefix(queryURL, "?") {
		queryURL = queryURL[1:]
	}

	method = strings.ToUpper(method)

	querys := strings.Split(queryURL, "&")
	querysResult := getQuerysResult(querys)
	params := getParams(querysResult)

	isTemporary, signature := getTemporaryAndSignature(params)

	isObs := getIsObs(isTemporary, querysResult, headers)
	headers = copyHeaders(headers)
	pathStyle := false
	if receviedHost, ok := headers[HEADER_HOST]; ok && len(receviedHost) > 0 && !strings.HasPrefix(receviedHost[0], bucketName+".") {
		pathStyle = true
	}
	conf := &config{securityProviders: []securityProvider{NewBasicSecurityProvider(ak, sk, "")},
		urlHolder: &urlHolder{scheme: "https", host: "dummy", port: 443},
		pathStyle: pathStyle}

	if isTemporary {
		return getTemporaryAuthorization(ak, sk, method, bucketName, objectKey, signature, conf, params, headers, isObs)
	}
	signature, region, signedHeaders := parseHeaders(headers)
	if signature == "v4" {
		conf.signature = SignatureV4
		requestURL, canonicalizedURL := conf.formatUrls(bucketName, objectKey, params, false)
		parsedRequestURL, _err := url.Parse(requestURL)
		if _err != nil {
			doLog(LEVEL_WARN, "Failed to parse requestURL")
			return nil
		}
		headerKeys := strings.Split(signedHeaders, ";")
		_headers := make(map[string][]string, len(headerKeys))
		for _, headerKey := range headerKeys {
			_headers[headerKey] = headers[headerKey]
		}
		ret = v4Auth(ak, sk, region, method, canonicalizedURL, parsedRequestURL.RawQuery, _headers)
		ret[HEADER_AUTH_CAMEL] = fmt.Sprintf("%s Credential=%s,SignedHeaders=%s,Signature=%s", V4_HASH_PREFIX, ret["Credential"], ret["SignedHeaders"], ret["Signature"])
	} else if signature == "v2" {
		if isObs {
			conf.signature = SignatureObs
		} else {
			conf.signature = SignatureV2
		}
		_, canonicalizedURL := conf.formatUrls(bucketName, objectKey, params, false)
		ret = v2Auth(ak, sk, method, canonicalizedURL, headers, isObs)
		v2HashPrefix := V2_HASH_PREFIX
		if isObs {
			v2HashPrefix = OBS_HASH_PREFIX
		}
		ret[HEADER_AUTH_CAMEL] = fmt.Sprintf("%s %s:%s", v2HashPrefix, ak, ret["Signature"])
	}
	return

}

func getTemporaryAuthorization(ak, sk, method, bucketName, objectKey, signature string, conf *config, params map[string]string,
	headers map[string][]string, isObs bool) (ret map[string]string) {

	if signature == "v4" {
		conf.signature = SignatureV4

		longDate, ok := params[PARAM_DATE_AMZ_CAMEL]
		if !ok {
			longDate = params[HEADER_DATE_AMZ]
		}
		shortDate := longDate[:8]

		credential, ok := params[PARAM_CREDENTIAL_AMZ_CAMEL]
		if !ok {
			credential = params[strings.ToLower(PARAM_CREDENTIAL_AMZ_CAMEL)]
		}

		_credential := UrlDecodeWithoutError(credential)

		regions := regionRegex.FindStringSubmatch(_credential)
		var region string
		if len(regions) >= 2 {
			region = regions[1]
		}

		_, scope := getCredential(ak, region, shortDate)

		expires, ok := params[PARAM_EXPIRES_AMZ_CAMEL]
		if !ok {
			expires = params[strings.ToLower(PARAM_EXPIRES_AMZ_CAMEL)]
		}

		signedHeaders, ok := params[PARAM_SIGNEDHEADERS_AMZ_CAMEL]
		if !ok {
			signedHeaders = params[strings.ToLower(PARAM_SIGNEDHEADERS_AMZ_CAMEL)]
		}

		algorithm, ok := params[PARAM_ALGORITHM_AMZ_CAMEL]
		if !ok {
			algorithm = params[strings.ToLower(PARAM_ALGORITHM_AMZ_CAMEL)]
		}

		if _, ok := params[PARAM_SIGNATURE_AMZ_CAMEL]; ok {
			delete(params, PARAM_SIGNATURE_AMZ_CAMEL)
		} else if _, ok := params[strings.ToLower(PARAM_SIGNATURE_AMZ_CAMEL)]; ok {
			delete(params, strings.ToLower(PARAM_SIGNATURE_AMZ_CAMEL))
		}

		ret = make(map[string]string, 6)
		ret[PARAM_ALGORITHM_AMZ_CAMEL] = algorithm
		ret[PARAM_CREDENTIAL_AMZ_CAMEL] = credential
		ret[PARAM_DATE_AMZ_CAMEL] = longDate
		ret[PARAM_EXPIRES_AMZ_CAMEL] = expires
		ret[PARAM_SIGNEDHEADERS_AMZ_CAMEL] = signedHeaders

		requestURL, canonicalizedURL := conf.formatUrls(bucketName, objectKey, params, false)
		parsedRequestURL, _err := url.Parse(requestURL)
		if _err != nil {
			doLog(LEVEL_WARN, "Failed to parse requestUrl")
			return nil
		}
		stringToSign := getV4StringToSign(method, canonicalizedURL, parsedRequestURL.RawQuery, scope, longDate, UNSIGNED_PAYLOAD, strings.Split(signedHeaders, ";"), headers)
		ret[PARAM_SIGNATURE_AMZ_CAMEL] = UrlEncode(getSignature(stringToSign, sk, region, shortDate), false)
	} else if signature == "v2" {
		if isObs {
			conf.signature = SignatureObs
		} else {
			conf.signature = SignatureV2
		}
		_, canonicalizedURL := conf.formatUrls(bucketName, objectKey, params, false)
		expires, ok := params["Expires"]
		if !ok {
			expires = params["expires"]
		}
		headers[HEADER_DATE_CAMEL] = []string{expires}
		stringToSign := getV2StringToSign(method, canonicalizedURL, headers, isObs)
		ret = make(map[string]string, 3)
		ret["Signature"] = UrlEncode(Base64Encode(HmacSha1([]byte(sk), []byte(stringToSign))), false)
		ret["AWSAccessKeyId"] = UrlEncode(ak, false)
		ret["Expires"] = UrlEncode(expires, false)
	}

	return
}

func GetContentType(key string) (string, bool) {
	if ct, ok := mimeTypes[strings.ToLower(key[strings.LastIndex(key, ".")+1:])]; ok {
		return ct, ok
	}
	return "", false
}

func GetReaderLen(reader io.Reader) (int64, error) {
	var contentLength int64
	var err error
	switch v := reader.(type) {
	case *bytes.Buffer:
		contentLength = int64(v.Len())
	case *bytes.Reader:
		contentLength = int64(v.Len())
	case *strings.Reader:
		contentLength = int64(v.Len())
	case *os.File:
		fInfo, fError := v.Stat()
		if fError != nil {
			err = fmt.Errorf("can't get reader content length,%s", fError.Error())
		} else {
			contentLength = fInfo.Size()
		}
	case *io.LimitedReader:
		contentLength = int64(v.N)
	case *fileReaderWrapper:
		contentLength = int64(v.totalCount)
	case *readerWrapper:
		contentLength = int64(v.totalCount)
	default:
		err = fmt.Errorf("can't get reader content length,unkown reader type")
	}
	return contentLength, err
}

func validateLength(value int, minLen int, maxLen int, fieldName string) error {
	if minLen > maxLen {
		return fmt.Errorf("Min Value can not be greater than Max Value")
	}
	if minLen == maxLen && value != minLen {
		return fmt.Errorf("%s length must be %d characters. (value len: %d)", fieldName, maxLen, value)
	}
	if value < minLen || value > maxLen {
		return fmt.Errorf("%s length must be between %d and %d characters. (value len: %d)", fieldName, minLen, maxLen, value)
	}
	return nil
}
