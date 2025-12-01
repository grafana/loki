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
	"io"
	"net/url"
	"strconv"
	"strings"
)

func (output *GetObjectOutput) setReadCloser(body io.ReadCloser) {
	output.Body = body
}

func (input ListObjsInput) trans(isObs bool) (params map[string]string, headers map[string][]string, data interface{}, err error) {
	params = make(map[string]string)
	if input.Prefix != "" {
		params["prefix"] = input.Prefix
	}
	if input.Delimiter != "" {
		params["delimiter"] = input.Delimiter
	}
	if input.MaxKeys > 0 {
		params["max-keys"] = IntToString(input.MaxKeys)
	}
	if input.EncodingType != "" {
		params["encoding-type"] = input.EncodingType
	}
	headers = make(map[string][]string)
	if origin := strings.TrimSpace(input.Origin); origin != "" {
		headers[HEADER_ORIGIN_CAMEL] = []string{origin}
	}
	if requestHeader := strings.TrimSpace(input.RequestHeader); requestHeader != "" {
		headers[HEADER_ACCESS_CONTROL_REQUEST_HEADER_CAMEL] = []string{requestHeader}
	}
	return
}

func (input ListObjectsInput) trans(isObs bool) (params map[string]string, headers map[string][]string, data interface{}, err error) {
	params, headers, data, err = input.ListObjsInput.trans(isObs)
	if err != nil {
		return
	}
	if input.Marker != "" {
		params["marker"] = input.Marker
	}
	return
}

func (input ListPosixObjectsInput) trans(isObs bool) (params map[string]string, headers map[string][]string, data interface{}, err error) {
	params, headers, data, err = input.ListObjsInput.trans(isObs)
	if err != nil {
		return
	}
	if input.Marker != "" {
		params["marker"] = input.Marker
	}
	return
}

func (input ListVersionsInput) trans(isObs bool) (params map[string]string, headers map[string][]string, data interface{}, err error) {
	params, headers, data, err = input.ListObjsInput.trans(isObs)
	if err != nil {
		return
	}
	params[string(SubResourceVersions)] = ""
	if input.KeyMarker != "" {
		params["key-marker"] = input.KeyMarker
	}
	if input.VersionIdMarker != "" {
		params["version-id-marker"] = input.VersionIdMarker
	}
	return
}

func (input DeleteObjectInput) trans(isObs bool) (params map[string]string, headers map[string][]string, data interface{}, err error) {
	params = make(map[string]string)
	if input.VersionId != "" {
		params[PARAM_VERSION_ID] = input.VersionId
	}
	return
}

func (input DeleteObjectsInput) trans(isObs bool) (params map[string]string, headers map[string][]string, data interface{}, err error) {
	params = map[string]string{string(SubResourceDelete): ""}
	if strings.ToLower(input.EncodingType) == "url" {
		for index, object := range input.Objects {
			input.Objects[index].Key = url.QueryEscape(object.Key)
		}
	}
	data, md5 := convertDeleteObjectsToXML(input)

	headers = map[string][]string{HEADER_MD5_CAMEL: {md5}}
	return
}

func (input SetObjectAclInput) trans(isObs bool) (params map[string]string, headers map[string][]string, data interface{}, err error) {
	params = map[string]string{string(SubResourceAcl): ""}
	if input.VersionId != "" {
		params[PARAM_VERSION_ID] = input.VersionId
	}
	headers = make(map[string][]string)
	if acl := string(input.ACL); acl != "" {
		setHeaders(headers, HEADER_ACL, []string{acl}, isObs)
	} else {
		data, _ = ConvertAclToXml(input.AccessControlPolicy, false, isObs)
	}
	return
}

func (input GetObjectAclInput) trans(isObs bool) (params map[string]string, headers map[string][]string, data interface{}, err error) {
	params = map[string]string{string(SubResourceAcl): ""}
	if input.VersionId != "" {
		params[PARAM_VERSION_ID] = input.VersionId
	}
	return
}

func (input RestoreObjectInput) trans(isObs bool) (params map[string]string, headers map[string][]string, data interface{}, err error) {
	params = map[string]string{string(SubResourceRestore): ""}
	if input.VersionId != "" {
		params[PARAM_VERSION_ID] = input.VersionId
	}
	if !isObs {
		data, err = ConvertRequestToIoReader(input)
	} else {
		data = ConventObsRestoreToXml(input)
	}
	return
}

func (input GetObjectMetadataInput) trans(isObs bool) (params map[string]string, headers map[string][]string, data interface{}, err error) {
	params = make(map[string]string)
	if input.VersionId != "" {
		params[PARAM_VERSION_ID] = input.VersionId
	}
	headers = make(map[string][]string)

	if input.Origin != "" {
		headers[HEADER_ORIGIN_CAMEL] = []string{input.Origin}
	}

	if input.RequestHeader != "" {
		headers[HEADER_ACCESS_CONTROL_REQUEST_HEADER_CAMEL] = []string{input.RequestHeader}
	}
	setSseHeader(headers, input.SseHeader, true, isObs)
	return
}

func (input SetObjectMetadataInput) prepareContentHeaders(headers map[string][]string) {
	if input.CacheControl != "" {
		headers[HEADER_CACHE_CONTROL_CAMEL] = []string{input.CacheControl}
	}
	if input.ContentDisposition != "" {
		headers[HEADER_CONTENT_DISPOSITION_CAMEL] = []string{input.ContentDisposition}
	}
	if input.ContentEncoding != "" {
		headers[HEADER_CONTENT_ENCODING_CAMEL] = []string{input.ContentEncoding}
	}
	if input.ContentLanguage != "" {
		headers[HEADER_CONTENT_LANGUAGE_CAMEL] = []string{input.ContentLanguage}
	}
	if input.ContentType != "" {
		headers[HEADER_CONTENT_TYPE_CAML] = []string{input.ContentType}
	}
	// 这里为了兼容老版本，默认以Expire值为准，但如果Expires没有，则以HttpExpires为准。
	if input.Expires != "" {
		headers[HEADER_EXPIRES_CAMEL] = []string{input.Expires}
	} else if input.HttpExpires != "" {
		headers[HEADER_EXPIRES_CAMEL] = []string{input.HttpExpires}
	}
}

func (input SetObjectMetadataInput) prepareStorageClass(headers map[string][]string, isObs bool) {
	if storageClass := string(input.StorageClass); storageClass != "" {
		if !isObs {
			if storageClass == string(StorageClassWarm) {
				storageClass = string(storageClassStandardIA)
			} else if storageClass == string(StorageClassCold) {
				storageClass = string(storageClassGlacier)
			} else if storageClass == string(StorageClassIntelligentTiering) {
				doLog(LEVEL_WARN, "Intelligent tiering supports only OBS signature.")
			}
		}
		setHeaders(headers, HEADER_STORAGE_CLASS2, []string{storageClass}, isObs)
	}
}

func (input SetObjectMetadataInput) trans(isObs bool) (params map[string]string, headers map[string][]string, data interface{}, err error) {
	params = map[string]string{string(SubResourceMetadata): ""}
	if input.VersionId != "" {
		params[PARAM_VERSION_ID] = input.VersionId
	}
	headers = make(map[string][]string)

	if directive := string(input.MetadataDirective); directive != "" {
		setHeaders(headers, HEADER_METADATA_DIRECTIVE, []string{string(input.MetadataDirective)}, isObs)
	} else {
		setHeaders(headers, HEADER_METADATA_DIRECTIVE, []string{string(ReplaceNew)}, isObs)
	}

	input.prepareContentHeaders(headers)
	if input.WebsiteRedirectLocation != "" {
		setHeaders(headers, HEADER_WEBSITE_REDIRECT_LOCATION, []string{input.WebsiteRedirectLocation}, isObs)
	}
	input.prepareStorageClass(headers, isObs)
	if input.Metadata != nil {
		for key, value := range input.Metadata {
			key = strings.TrimSpace(key)
			setHeadersNext(headers, HEADER_PREFIX_META_OBS+key, HEADER_PREFIX_META+key, []string{value}, isObs)
		}
	}
	return
}

func (input GetObjectInput) prepareResponseParams(params map[string]string) {
	if input.ResponseCacheControl != "" {
		params[PARAM_RESPONSE_CACHE_CONTROL] = input.ResponseCacheControl
	}
	if input.ResponseContentDisposition != "" {
		params[PARAM_RESPONSE_CONTENT_DISPOSITION] = input.ResponseContentDisposition
	}
	if input.ResponseContentEncoding != "" {
		params[PARAM_RESPONSE_CONTENT_ENCODING] = input.ResponseContentEncoding
	}
	if input.ResponseContentLanguage != "" {
		params[PARAM_RESPONSE_CONTENT_LANGUAGE] = input.ResponseContentLanguage
	}
	if input.ResponseContentType != "" {
		params[PARAM_RESPONSE_CONTENT_TYPE] = input.ResponseContentType
	}
	if input.ResponseExpires != "" {
		params[PARAM_RESPONSE_EXPIRES] = input.ResponseExpires
	}
}

func (input GetObjectInput) trans(isObs bool) (params map[string]string, headers map[string][]string, data interface{}, err error) {
	params, headers, data, err = input.GetObjectMetadataInput.trans(isObs)
	if err != nil {
		return
	}
	input.prepareResponseParams(params)
	if input.ImageProcess != "" {
		params[PARAM_IMAGE_PROCESS] = input.ImageProcess
	}
	if input.RangeStart >= 0 && input.RangeEnd > input.RangeStart {
		headers[HEADER_RANGE] = []string{fmt.Sprintf("bytes=%d-%d", input.RangeStart, input.RangeEnd)}
	}
	if input.Range != "" {
		headers[HEADER_RANGE] = []string{input.Range}
	}
	if input.AcceptEncoding != "" {
		headers[HEADER_ACCEPT_ENCODING] = []string{input.AcceptEncoding}
	}
	if input.IfMatch != "" {
		headers[HEADER_IF_MATCH] = []string{input.IfMatch}
	}
	if input.IfNoneMatch != "" {
		headers[HEADER_IF_NONE_MATCH] = []string{input.IfNoneMatch}
	}
	if !input.IfModifiedSince.IsZero() {
		headers[HEADER_IF_MODIFIED_SINCE] = []string{FormatUtcToRfc1123(input.IfModifiedSince)}
	}
	if !input.IfUnmodifiedSince.IsZero() {
		headers[HEADER_IF_UNMODIFIED_SINCE] = []string{FormatUtcToRfc1123(input.IfUnmodifiedSince)}
	}
	return
}

func (input ObjectOperationInput) prepareGrantHeaders(headers map[string][]string, isObs bool) {
	if GrantReadID := input.GrantReadId; GrantReadID != "" {
		setHeaders(headers, HEADER_GRANT_READ_OBS, []string{GrantReadID}, isObs)
	}
	if GrantReadAcpID := input.GrantReadAcpId; GrantReadAcpID != "" {
		setHeaders(headers, HEADER_GRANT_READ_ACP_OBS, []string{GrantReadAcpID}, isObs)
	}
	if GrantWriteAcpID := input.GrantWriteAcpId; GrantWriteAcpID != "" {
		setHeaders(headers, HEADER_GRANT_WRITE_ACP_OBS, []string{GrantWriteAcpID}, isObs)
	}
	if GrantFullControlID := input.GrantFullControlId; GrantFullControlID != "" {
		setHeaders(headers, HEADER_GRANT_FULL_CONTROL_OBS, []string{GrantFullControlID}, isObs)
	}
}

func (input ObjectOperationInput) trans(isObs bool) (params map[string]string, headers map[string][]string, data interface{}, err error) {
	headers = make(map[string][]string)
	params = make(map[string]string)
	if acl := string(input.ACL); acl != "" {
		setHeaders(headers, HEADER_ACL, []string{acl}, isObs)
	}
	input.prepareGrantHeaders(headers, isObs)
	if storageClass := string(input.StorageClass); storageClass != "" {
		if !isObs {
			if storageClass == string(StorageClassWarm) {
				storageClass = string(storageClassStandardIA)
			} else if storageClass == string(StorageClassCold) {
				storageClass = string(storageClassGlacier)
			} else if storageClass == string(StorageClassIntelligentTiering) {
				doLog(LEVEL_WARN, "Intelligent tiering supports only OBS signature.")
			}
		}
		setHeaders(headers, HEADER_STORAGE_CLASS2, []string{storageClass}, isObs)
	}
	if input.WebsiteRedirectLocation != "" {
		setHeaders(headers, HEADER_WEBSITE_REDIRECT_LOCATION, []string{input.WebsiteRedirectLocation}, isObs)

	}
	setSseHeader(headers, input.SseHeader, false, isObs)
	if input.Expires != 0 {
		setHeaders(headers, HEADER_EXPIRES, []string{Int64ToString(input.Expires)}, true)
	}
	if input.Metadata != nil {
		for key, value := range input.Metadata {
			key = strings.TrimSpace(key)
			setHeadersNext(headers, HEADER_PREFIX_META_OBS+key, HEADER_PREFIX_META+key, []string{value}, isObs)
		}
	}
	return
}

func (input PutObjectBasicInput) trans(isObs bool) (params map[string]string, headers map[string][]string, data interface{}, err error) {
	params, headers, data, err = input.ObjectOperationInput.trans(isObs)
	if err != nil {
		return
	}

	if input.ContentMD5 != "" {
		headers[HEADER_MD5_CAMEL] = []string{input.ContentMD5}
	}

	if input.ContentSHA256 != "" {
		setHeaders(headers, HEADER_SHA256, []string{input.ContentSHA256}, isObs)
	}

	if input.ContentLength > 0 {
		headers[HEADER_CONTENT_LENGTH_CAMEL] = []string{Int64ToString(input.ContentLength)}
	}
	if input.ContentType != "" {
		headers[HEADER_CONTENT_TYPE_CAML] = []string{input.ContentType}
	}
	if input.ContentEncoding != "" {
		headers[HEADER_CONTENT_ENCODING_CAMEL] = []string{input.ContentEncoding}
	}
	if input.CacheControl != "" {
		headers[HEADER_CACHE_CONTROL_CAMEL] = []string{input.CacheControl}
	}
	if input.ContentDisposition != "" {
		headers[HEADER_CONTENT_DISPOSITION_CAMEL] = []string{input.ContentDisposition}
	}
	if input.ContentLanguage != "" {
		headers[HEADER_CONTENT_LANGUAGE_CAMEL] = []string{input.ContentLanguage}
	}
	if input.HttpExpires != "" {
		headers[HEADER_EXPIRES_CAMEL] = []string{input.HttpExpires}
	}
	return
}

func (input PutObjectInput) trans(isObs bool) (params map[string]string, headers map[string][]string, data interface{}, err error) {
	params, headers, data, err = input.PutObjectBasicInput.trans(isObs)
	if err != nil {
		return
	}
	if input.Body != nil {
		data = input.Body
	}
	return
}

func (input AppendObjectInput) trans(isObs bool) (params map[string]string, headers map[string][]string, data interface{}, err error) {
	params, headers, data, err = input.PutObjectBasicInput.trans(isObs)
	if err != nil {
		return
	}
	params[string(SubResourceAppend)] = ""
	params["position"] = strconv.FormatInt(input.Position, 10)
	if input.Body != nil {
		data = input.Body
	}
	return
}

func (input ModifyObjectInput) trans(isObs bool) (params map[string]string, headers map[string][]string, data interface{}, err error) {
	headers = make(map[string][]string)
	params = make(map[string]string)
	params[string(SubResourceModify)] = ""
	params["position"] = strconv.FormatInt(input.Position, 10)
	if input.ContentLength > 0 {
		headers[HEADER_CONTENT_LENGTH_CAMEL] = []string{Int64ToString(input.ContentLength)}
	}

	if input.Body != nil {
		data = input.Body
	}
	return
}

func (input CopyObjectInput) prepareReplaceHeaders(headers map[string][]string) {
	if input.ContentType != "" {
		headers[HEADER_CONTENT_TYPE] = []string{input.ContentType}
	}
	if input.CacheControl != "" {
		headers[HEADER_CACHE_CONTROL] = []string{input.CacheControl}
	}
	if input.ContentDisposition != "" {
		headers[HEADER_CONTENT_DISPOSITION] = []string{input.ContentDisposition}
	}
	if input.ContentEncoding != "" {
		headers[HEADER_CONTENT_ENCODING] = []string{input.ContentEncoding}
	}
	if input.ContentLanguage != "" {
		headers[HEADER_CONTENT_LANGUAGE] = []string{input.ContentLanguage}
	}
	// 这里为了兼容老版本，默认以Expire值为准，但如果Expires没有，则以HttpExpires为准。
	if input.Expires != "" {
		headers[HEADER_EXPIRES_CAMEL] = []string{input.Expires}
	} else if input.HttpExpires != "" {
		headers[HEADER_EXPIRES_CAMEL] = []string{input.HttpExpires}
	}
}

func (input CopyObjectInput) prepareCopySourceHeaders(headers map[string][]string, isObs bool) {
	if input.CopySourceIfMatch != "" {
		setHeaders(headers, HEADER_COPY_SOURCE_IF_MATCH, []string{input.CopySourceIfMatch}, isObs)
	}
	if input.CopySourceIfNoneMatch != "" {
		setHeaders(headers, HEADER_COPY_SOURCE_IF_NONE_MATCH, []string{input.CopySourceIfNoneMatch}, isObs)
	}
	if !input.CopySourceIfModifiedSince.IsZero() {
		setHeaders(headers, HEADER_COPY_SOURCE_IF_MODIFIED_SINCE, []string{FormatUtcToRfc1123(input.CopySourceIfModifiedSince)}, isObs)
	}
	if !input.CopySourceIfUnmodifiedSince.IsZero() {
		setHeaders(headers, HEADER_COPY_SOURCE_IF_UNMODIFIED_SINCE, []string{FormatUtcToRfc1123(input.CopySourceIfUnmodifiedSince)}, isObs)
	}
}

func (input CopyObjectInput) trans(isObs bool) (params map[string]string, headers map[string][]string, data interface{}, err error) {
	params, headers, data, err = input.ObjectOperationInput.trans(isObs)
	if err != nil {
		return
	}

	var copySource string
	if input.CopySourceVersionId != "" {
		copySource = fmt.Sprintf("%s/%s?versionId=%s", input.CopySourceBucket, UrlEncode(input.CopySourceKey, false), input.CopySourceVersionId)
	} else {
		copySource = fmt.Sprintf("%s/%s", input.CopySourceBucket, UrlEncode(input.CopySourceKey, false))
	}
	setHeaders(headers, HEADER_COPY_SOURCE, []string{copySource}, isObs)

	if directive := string(input.MetadataDirective); directive != "" {
		setHeaders(headers, HEADER_METADATA_DIRECTIVE, []string{directive}, isObs)
	}

	if input.MetadataDirective == ReplaceMetadata {
		input.prepareReplaceHeaders(headers)
	}

	input.prepareCopySourceHeaders(headers, isObs)
	if input.SourceSseHeader != nil {
		if sseCHeader, ok := input.SourceSseHeader.(SseCHeader); ok {
			setHeaders(headers, HEADER_SSEC_COPY_SOURCE_ENCRYPTION, []string{sseCHeader.GetEncryption()}, isObs)
			setHeaders(headers, HEADER_SSEC_COPY_SOURCE_KEY, []string{sseCHeader.GetKey()}, isObs)
			setHeaders(headers, HEADER_SSEC_COPY_SOURCE_KEY_MD5, []string{sseCHeader.GetKeyMD5()}, isObs)
		}
	}
	if input.SuccessActionRedirect != "" {
		headers[HEADER_SUCCESS_ACTION_REDIRECT] = []string{input.SuccessActionRedirect}
	}
	return
}

func (input HeadObjectInput) trans(isObs bool) (params map[string]string, headers map[string][]string, data interface{}, err error) {
	params = make(map[string]string)
	if input.VersionId != "" {
		params[PARAM_VERSION_ID] = input.VersionId
	}
	return
}

func (input RenameFileInput) trans(isObs bool) (params map[string]string, headers map[string][]string, data interface{}, err error) {
	params = map[string]string{string(SubResourceRename): ""}
	params["name"] = input.NewObjectKey
	headers = make(map[string][]string)
	if requestPayer := string(input.RequestPayer); requestPayer != "" {
		headers[HEADER_REQUEST_PAYER] = []string{requestPayer}
	}
	return
}

func (input RenameFolderInput) trans(isObs bool) (params map[string]string, headers map[string][]string, data interface{}, err error) {
	params = map[string]string{string(SubResourceRename): ""}
	params["name"] = input.NewObjectKey
	headers = make(map[string][]string)
	if requestPayer := string(input.RequestPayer); requestPayer != "" {
		headers[HEADER_REQUEST_PAYER] = []string{requestPayer}
	}
	return
}

func (input SetDirAccesslabelInput) trans(isObs bool) (params map[string]string, headers map[string][]string, data interface{}, err error) {
	params = map[string]string{string(SubResourceAccesslabel): ""}

	accesslabelJson, err := TransToJSON(input.Accesslabel)
	if err != nil {
		return
	}
	json := make([]string, 0, 2)
	json = append(json, fmt.Sprintf("{\"accesslabel\": %s}", accesslabelJson))
	data = strings.Join(json, "")

	return
}

func (input GetDirAccesslabelInput) trans(isObs bool) (params map[string]string, headers map[string][]string, data interface{}, err error) {
	params = map[string]string{string(SubResourceAccesslabel): ""}
	return
}

func (input DeleteDirAccesslabelInput) trans(isObs bool) (params map[string]string, headers map[string][]string, data interface{}, err error) {
	params = map[string]string{string(SubResourceAccesslabel): ""}
	return
}
