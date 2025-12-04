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

const (
	OBS_SDK_VERSION        = "3.25.9"
	USER_AGENT             = "obs-sdk-go/" + OBS_SDK_VERSION
	HEADER_PREFIX          = "x-amz-"
	HEADER_PREFIX_META     = "x-amz-meta-"
	HEADER_PREFIX_OBS      = "x-obs-"
	HEADER_PREFIX_META_OBS = "x-obs-meta-"
	HEADER_DATE_AMZ        = "x-amz-date"
	HEADER_DATE_OBS        = "x-obs-date"
	HEADER_STS_TOKEN_AMZ   = "x-amz-security-token"
	HEADER_STS_TOKEN_OBS   = "x-obs-security-token"
	HEADER_ACCESSS_KEY_AMZ = "AWSAccessKeyId"
	PREFIX_META            = "meta-"

	HEADER_CONTENT_SHA256_AMZ               = "x-amz-content-sha256"
	HEADER_ACL_AMZ                          = "x-amz-acl"
	HEADER_ACL_OBS                          = "x-obs-acl"
	HEADER_ACL                              = "acl"
	HEADER_LOCATION_AMZ                     = "location"
	HEADER_BUCKET_LOCATION_OBS              = "bucket-location"
	HEADER_COPY_SOURCE                      = "copy-source"
	HEADER_COPY_SOURCE_RANGE                = "copy-source-range"
	HEADER_RANGE                            = "Range"
	HEADER_STORAGE_CLASS                    = "x-default-storage-class"
	HEADER_STORAGE_CLASS_OBS                = "x-obs-storage-class"
	HEADER_FS_FILE_INTERFACE_OBS            = "x-obs-fs-file-interface"
	HEADER_MODE                             = "mode"
	HEADER_VERSION_OBS                      = "version"
	HEADER_REQUEST_PAYER                    = "x-amz-request-payer"
	HEADER_GRANT_READ_OBS                   = "grant-read"
	HEADER_GRANT_WRITE_OBS                  = "grant-write"
	HEADER_GRANT_READ_ACP_OBS               = "grant-read-acp"
	HEADER_GRANT_WRITE_ACP_OBS              = "grant-write-acp"
	HEADER_GRANT_FULL_CONTROL_OBS           = "grant-full-control"
	HEADER_GRANT_READ_DELIVERED_OBS         = "grant-read-delivered"
	HEADER_GRANT_FULL_CONTROL_DELIVERED_OBS = "grant-full-control-delivered"
	HEADER_REQUEST_ID                       = "request-id"
	HEADER_ERROR_CODE                       = "error-code"
	HEADER_ERROR_INDICATOR                  = "x-reserved-indicator"
	HEADER_ERROR_MESSAGE                    = "error-message"
	HEADER_BUCKET_REGION                    = "bucket-region"
	HEADER_ACCESS_CONRTOL_ALLOW_ORIGIN      = "access-control-allow-origin"
	HEADER_ACCESS_CONRTOL_ALLOW_HEADERS     = "access-control-allow-headers"
	HEADER_ACCESS_CONRTOL_MAX_AGE           = "access-control-max-age"
	HEADER_ACCESS_CONRTOL_ALLOW_METHODS     = "access-control-allow-methods"
	HEADER_ACCESS_CONRTOL_EXPOSE_HEADERS    = "access-control-expose-headers"
	HEADER_EPID_HEADERS                     = "epid"
	HEADER_VERSION_ID                       = "version-id"
	HEADER_COPY_SOURCE_VERSION_ID           = "copy-source-version-id"
	HEADER_DELETE_MARKER                    = "delete-marker"
	HEADER_WEBSITE_REDIRECT_LOCATION        = "website-redirect-location"
	HEADER_METADATA_DIRECTIVE               = "metadata-directive"
	HEADER_EXPIRATION                       = "expiration"
	HEADER_EXPIRES_OBS                      = "x-obs-expires"
	HEADER_RESTORE                          = "restore"
	HEADER_OBJECT_TYPE                      = "object-type"
	HEADER_NEXT_APPEND_POSITION             = "next-append-position"
	HEADER_STORAGE_CLASS2                   = "storage-class"
	HEADER_CONTENT_LENGTH                   = "content-length"
	HEADER_CONTENT_TYPE                     = "content-type"
	HEADER_CONTENT_LANGUAGE                 = "content-language"
	HEADER_EXPIRES                          = "expires"
	HEADER_CACHE_CONTROL                    = "cache-control"
	HEADER_CONTENT_DISPOSITION              = "content-disposition"
	HEADER_CONTENT_ENCODING                 = "content-encoding"
	HEADER_AZ_REDUNDANCY                    = "az-redundancy"
	HEADER_BUCKET_TYPE                      = "bucket-type"
	HEADER_BUCKET_REDUNDANCY                = "bucket-redundancy"
	HEADER_FUSION_ALLOW_UPGRADE             = "fusion-allow-upgrade"
	HEADER_FUSION_ALLOW_ALT                 = "fusion-allow-alternative"
	headerOefMarker                         = "oef-marker"

	HEADER_ETAG         = "etag"
	HEADER_LASTMODIFIED = "last-modified"

	HEADER_COPY_SOURCE_IF_MATCH            = "copy-source-if-match"
	HEADER_COPY_SOURCE_IF_NONE_MATCH       = "copy-source-if-none-match"
	HEADER_COPY_SOURCE_IF_MODIFIED_SINCE   = "copy-source-if-modified-since"
	HEADER_COPY_SOURCE_IF_UNMODIFIED_SINCE = "copy-source-if-unmodified-since"

	HEADER_IF_MATCH            = "If-Match"
	HEADER_IF_NONE_MATCH       = "If-None-Match"
	HEADER_IF_MODIFIED_SINCE   = "If-Modified-Since"
	HEADER_IF_UNMODIFIED_SINCE = "If-Unmodified-Since"

	HEADER_SSEC_ENCRYPTION = "server-side-encryption-customer-algorithm"
	HEADER_SSEC_KEY        = "server-side-encryption-customer-key"
	HEADER_SSEC_KEY_MD5    = "server-side-encryption-customer-key-MD5"

	HEADER_SSEKMS_ENCRYPTION      = "server-side-encryption"
	HEADER_SSEKMS_KEY             = "server-side-encryption-aws-kms-key-id"
	HEADER_SSEKMS_ENCRYPT_KEY_OBS = "server-side-encryption-kms-key-id"

	HEADER_SSEC_COPY_SOURCE_ENCRYPTION = "copy-source-server-side-encryption-customer-algorithm"
	HEADER_SSEC_COPY_SOURCE_KEY        = "copy-source-server-side-encryption-customer-key"
	HEADER_SSEC_COPY_SOURCE_KEY_MD5    = "copy-source-server-side-encryption-customer-key-MD5"

	HEADER_SSEKMS_KEY_AMZ = "x-amz-server-side-encryption-aws-kms-key-id"

	HEADER_SSEKMS_KEY_OBS = "x-obs-server-side-encryption-kms-key-id"

	HEADER_SUCCESS_ACTION_REDIRECT = "success_action_redirect"

	headerFSFileInterface = "fs-file-interface"

	HEADER_DATE_CAMEL                          = "Date"
	HEADER_HOST_CAMEL                          = "Host"
	HEADER_HOST                                = "host"
	HEADER_AUTH_CAMEL                          = "Authorization"
	HEADER_MD5_CAMEL                           = "Content-MD5"
	HEADER_SHA256_CAMEL                        = "Content-SHA256"
	HEADER_SHA256                              = "content-sha256"
	HEADER_LOCATION_CAMEL                      = "Location"
	HEADER_CONTENT_LENGTH_CAMEL                = "Content-Length"
	HEADER_CONTENT_TYPE_CAML                   = "Content-Type"
	HEADER_USER_AGENT_CAMEL                    = "User-Agent"
	HEADER_ORIGIN_CAMEL                        = "Origin"
	HEADER_ACCESS_CONTROL_REQUEST_HEADER_CAMEL = "Access-Control-Request-Headers"
	HEADER_CACHE_CONTROL_CAMEL                 = "Cache-Control"
	HEADER_CONTENT_DISPOSITION_CAMEL           = "Content-Disposition"
	HEADER_CONTENT_ENCODING_CAMEL              = "Content-Encoding"
	HEADER_CONTENT_LANGUAGE_CAMEL              = "Content-Language"
	HEADER_EXPIRES_CAMEL                       = "Expires"
	HEADER_ACCEPT_ENCODING                     = "Accept-Encoding"

	PARAM_VERSION_ID                   = "versionId"
	PARAM_RESPONSE_CONTENT_TYPE        = "response-content-type"
	PARAM_RESPONSE_CONTENT_LANGUAGE    = "response-content-language"
	PARAM_RESPONSE_EXPIRES             = "response-expires"
	PARAM_RESPONSE_CACHE_CONTROL       = "response-cache-control"
	PARAM_RESPONSE_CONTENT_DISPOSITION = "response-content-disposition"
	PARAM_RESPONSE_CONTENT_ENCODING    = "response-content-encoding"
	PARAM_IMAGE_PROCESS                = "x-image-process"

	PARAM_ALGORITHM_AMZ_CAMEL     = "X-Amz-Algorithm"
	PARAM_CREDENTIAL_AMZ_CAMEL    = "X-Amz-Credential"
	PARAM_DATE_AMZ_CAMEL          = "X-Amz-Date"
	PARAM_DATE_OBS_CAMEL          = "X-Obs-Date"
	PARAM_EXPIRES_AMZ_CAMEL       = "X-Amz-Expires"
	PARAM_SIGNEDHEADERS_AMZ_CAMEL = "X-Amz-SignedHeaders"
	PARAM_SIGNATURE_AMZ_CAMEL     = "X-Amz-Signature"

	DEFAULT_SIGNATURE            = SignatureV2
	DEFAULT_REGION               = "region"
	DEFAULT_CONNECT_TIMEOUT      = 60
	DEFAULT_SOCKET_TIMEOUT       = 60
	DEFAULT_HEADER_TIMEOUT       = 60
	DEFAULT_IDLE_CONN_TIMEOUT    = 30
	DEFAULT_MAX_RETRY_COUNT      = 3
	DEFAULT_MAX_REDIRECT_COUNT   = 3
	DEFAULT_MAX_CONN_PER_HOST    = 1000
	UNSIGNED_PAYLOAD             = "UNSIGNED-PAYLOAD"
	LONG_DATE_FORMAT             = "20060102T150405Z"
	SHORT_DATE_FORMAT            = "20060102"
	ISO8601_DATE_FORMAT          = "2006-01-02T15:04:05Z"
	ISO8601_MIDNIGHT_DATE_FORMAT = "2006-01-02T00:00:00Z"
	RFC1123_FORMAT               = "Mon, 02 Jan 2006 15:04:05 GMT"

	V4_SERVICE_NAME   = "s3"
	V4_SERVICE_SUFFIX = "aws4_request"

	V2_HASH_PREFIX  = "AWS"
	OBS_HASH_PREFIX = "OBS"

	V4_HASH_PREFIX = "AWS4-HMAC-SHA256"
	V4_HASH_PRE    = "AWS4"

	DEFAULT_SSE_KMS_ENCRYPTION     = "aws:kms"
	DEFAULT_SSE_KMS_ENCRYPTION_OBS = "kms"

	DEFAULT_SSE_C_ENCRYPTION = "AES256"

	HTTP_GET     = "GET"
	HTTP_POST    = "POST"
	HTTP_PUT     = "PUT"
	HTTP_DELETE  = "DELETE"
	HTTP_HEAD    = "HEAD"
	HTTP_OPTIONS = "OPTIONS"

	REQUEST_PAYER = "request-payer"
	TRAFFIC_LIMIT = "traffic-limit"
	CALLBACK      = "callback"
	MULTI_AZ      = "3az"

	MAX_PART_SIZE     = 5 * 1024 * 1024 * 1024
	MIN_PART_SIZE     = 100 * 1024
	DEFAULT_PART_SIZE = 9 * 1024 * 1024
	MAX_PART_NUM      = 10000

	GET_OBJECT                  = "GetObject"
	PUT_OBJECT                  = "PutObject"
	PUT_FILE                    = "PutFile"
	APPEND_OBJECT               = "AppendObject"
	MAX_CERT_XML_BODY_SIZE      = 40 * 1024
	CERT_ID_SIZE                = 16
	MAX_CERTIFICATE_NAME_LENGTH = 63
	MIN_CERTIFICATE_NAME_LENGTH = 3
	CERTIFICATE_FIELD_NAME      = "CERTIFICATE ID SIZE"
	NAME_LENGTH                 = "Name Length"
	XML_SIZE                    = "XML SIZE"
)

var (
	interestedHeaders = []string{"content-md5", "content-type", "date"}

	allowedRequestHTTPHeaderMetadataNames = map[string]bool{
		"content-type":                   true,
		"content-md5":                    true,
		"content-sha256":                 true,
		"content-length":                 true,
		"content-language":               true,
		"expires":                        true,
		"origin":                         true,
		"cache-control":                  true,
		"content-disposition":            true,
		"content-encoding":               true,
		"access-control-request-method":  true,
		"access-control-request-headers": true,
		"x-default-storage-class":        true,
		"location":                       true,
		"date":                           true,
		"etag":                           true,
		"range":                          true,
		"host":                           true,
		"if-modified-since":              true,
		"if-unmodified-since":            true,
		"if-match":                       true,
		"if-none-match":                  true,
		"last-modified":                  true,
		"content-range":                  true,
		"accept-encoding":                true,
		"x-hic-info":                     true,
		"safe-area":                      true,
	}

	allowedLogResponseHTTPHeaderNames = map[string]bool{
		"content-type":         true,
		"etag":                 true,
		"connection":           true,
		"content-length":       true,
		"date":                 true,
		"server":               true,
		"x-reserved-indicator": true,
	}

	allowedResourceParameterNames = map[string]bool{
		"acl":                          true,
		"backtosource":                 true,
		"metadata":                     true,
		"policy":                       true,
		"torrent":                      true,
		"logging":                      true,
		"location":                     true,
		"storageinfo":                  true,
		"quota":                        true,
		"storageclass":                 true,
		"storagepolicy":                true,
		"requestpayment":               true,
		"versions":                     true,
		"versioning":                   true,
		"versionid":                    true,
		"uploads":                      true,
		"uploadid":                     true,
		"partnumber":                   true,
		"website":                      true,
		"notification":                 true,
		"lifecycle":                    true,
		"deletebucket":                 true,
		"delete":                       true,
		"cors":                         true,
		"restore":                      true,
		"encryption":                   true,
		"tagging":                      true,
		"append":                       true,
		"modify":                       true,
		"position":                     true,
		"replication":                  true,
		"response-content-type":        true,
		"response-content-language":    true,
		"response-expires":             true,
		"response-cache-control":       true,
		"response-content-disposition": true,
		"response-content-encoding":    true,
		"x-image-process":              true,
		"x-oss-process":                true,
		"x-image-save-bucket":          true,
		"x-image-save-object":          true,
		"ignore-sign-in-query":         true,
		"name":                         true,
		"rename":                       true,
		"customdomain":                 true,
		"mirrorbacktosource":           true,
		"x-obs-accesslabel":            true,
		"object-lock":                  true,
		"retention":                    true,
		"x-obs-security-token":         true,
		"truncate":                     true,
		"length":                       true,
		"inventory":                    true,
		"directcoldaccess":             true,
		"attname":                      true,
		"cdnnotifyconfiguration":       true,
		"publicaccessblock":            true,
		"bucketstatus":                 true,
		"policystatus":                 true,
	}

	obsStorageClasses = []string{
		string(StorageClassStandard),
		string(StorageClassWarm),
		string(StorageClassCold),
		string(StorageClassDeepArchive),
		string(StorageClassIntelligentTiering),
	}
)
