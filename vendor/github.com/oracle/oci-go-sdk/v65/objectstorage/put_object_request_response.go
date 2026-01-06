// Copyright (c) 2016, 2018, 2025, Oracle and/or its affiliates.  All rights reserved.
// This software is dual-licensed to you under the Universal Permissive License (UPL) 1.0 as shown at https://oss.oracle.com/licenses/upl or Apache License 2.0 as shown at http://www.apache.org/licenses/LICENSE-2.0. You may choose either license.
// Code generated. DO NOT EDIT.

package objectstorage

import (
	"fmt"
	"github.com/oracle/oci-go-sdk/v65/common"
	"io"
	"net/http"
	"strings"
)

// PutObjectRequest wrapper for the PutObject operation
//
// # See also
//
// Click https://docs.oracle.com/en-us/iaas/tools/go-sdk-examples/latest/objectstorage/PutObject.go.html to see an example of how to use PutObjectRequest.
type PutObjectRequest struct {

	// The Object Storage namespace used for the request.
	NamespaceName *string `mandatory:"true" contributesTo:"path" name:"namespaceName"`

	// The name of the bucket. Avoid entering confidential information.
	// Example: `my-new-bucket1`
	BucketName *string `mandatory:"true" contributesTo:"path" name:"bucketName"`

	// The name of the object. Avoid entering confidential information.
	// Example: `test/object1.log`
	ObjectName *string `mandatory:"true" contributesTo:"path" name:"objectName"`

	// The content length of the body.
	ContentLength *int64 `mandatory:"false" contributesTo:"header" name:"Content-Length"`

	// The object to upload to the object store.
	PutObjectBody io.ReadCloser `mandatory:"true" contributesTo:"body" encoding:"binary"`

	// The entity tag (ETag) to match with the ETag of an existing resource. If the specified ETag matches the ETag of
	// the existing resource, GET and HEAD requests will return the resource and PUT and POST requests will upload
	// the resource.
	IfMatch *string `mandatory:"false" contributesTo:"header" name:"if-match"`

	// The entity tag (ETag) to avoid matching. The only valid value is '*', which indicates that the request should
	// fail if the resource already exists.
	IfNoneMatch *string `mandatory:"false" contributesTo:"header" name:"if-none-match"`

	// The client request ID for tracing.
	OpcClientRequestId *string `mandatory:"false" contributesTo:"header" name:"opc-client-request-id"`

	// A value of `100-continue` requests preliminary verification of the request method, path, and headers before the request body is sent.
	// If no error results from such verification, the server will send a 100 (Continue) interim response to indicate readiness for the request body.
	// The only allowed value for this parameter is "100-Continue" (case-insensitive).
	Expect *string `mandatory:"false" contributesTo:"header" name:"Expect"`

	// The optional header that defines the base64-encoded MD5 hash of the body. If the optional Content-MD5 header is present, Object
	// Storage performs an integrity check on the body of the HTTP request by computing the MD5 hash for the body and comparing it to the
	// MD5 hash supplied in the header. If the two hashes do not match, the object is rejected and an HTTP-400 Unmatched Content MD5 error
	// is returned with the message:
	// "The computed MD5 of the request body (ACTUAL_MD5) does not match the Content-MD5 header (HEADER_MD5)"
	ContentMD5 *string `mandatory:"false" contributesTo:"header" name:"Content-MD5"`

	// The optional checksum algorithm to use to compute and store the checksum of the body of the HTTP request (or the parts in case of multipart uploads),
	// in addition to the default MD5 checksum.
	OpcChecksumAlgorithm PutObjectOpcChecksumAlgorithmEnum `mandatory:"false" contributesTo:"header" name:"opc-checksum-algorithm"`

	// Applicable only if CRC32C is specified in the opc-checksum-algorithm request header.
	// The optional header that defines the base64-encoded, 32-bit CRC32C (Castagnoli) checksum of the body. If the optional opc-content-crc32c header
	// is present, Object Storage performs an integrity check on the body of the HTTP request by computing the CRC32C checksum for the body and comparing
	// it to the CRC32C checksum supplied in the header. If the two checksums do not match, the object is rejected and an HTTP-400 Unmatched Content CRC32C error
	// is returned with the message:
	// "The computed CRC32C of the request body (ACTUAL_CRC32C) does not match the opc-content-crc32c header (HEADER_CRC32C)"
	OpcContentCrc32c *string `mandatory:"false" contributesTo:"header" name:"opc-content-crc32c"`

	// Applicable only if SHA256 is specified in the opc-checksum-algorithm request header.
	// The optional header that defines the base64-encoded SHA256 hash of the body. If the optional opc-content-sha256 header is present, Object
	// Storage performs an integrity check on the body of the HTTP request by computing the SHA256 hash for the body and comparing it to the
	// SHA256 hash supplied in the header. If the two hashes do not match, the object is rejected and an HTTP-400 Unmatched Content SHA256 error
	// is returned with the message:
	// "The computed SHA256 of the request body (ACTUAL_SHA256) does not match the opc-content-sha256 header (HEADER_SHA256)"
	OpcContentSha256 *string `mandatory:"false" contributesTo:"header" name:"opc-content-sha256"`

	// Applicable only if SHA384 is specified in the opc-checksum-algorithm request header.
	// The optional header that defines the base64-encoded SHA384 hash of the body. If the optional opc-content-sha384 header is present, Object
	// Storage performs an integrity check on the body of the HTTP request by computing the SHA384 hash for the body and comparing it to the
	// SHA384 hash supplied in the header. If the two hashes do not match, the object is rejected and an HTTP-400 Unmatched Content SHA384 error
	// is returned with the message:
	// "The computed SHA384 of the request body (ACTUAL_SHA384) does not match the opc-content-sha384 header (HEADER_SHA384)"
	OpcContentSha384 *string `mandatory:"false" contributesTo:"header" name:"opc-content-sha384"`

	// The optional Content-Type header that defines the standard MIME type format of the object. Content type defaults to
	// 'application/octet-stream' if not specified in the PutObject call. Specifying values for this header has no effect
	// on Object Storage behavior. Programs that read the object determine what to do based on the value provided. For example,
	// you could use this header to identify and perform special operations on text only objects.
	ContentType *string `mandatory:"false" contributesTo:"header" name:"Content-Type"`

	// The optional Content-Language header that defines the content language of the object to upload. Specifying
	// values for this header has no effect on Object Storage behavior. Programs that read the object determine what
	// to do based on the value provided. For example, you could use this header to identify and differentiate objects
	// based on a particular language.
	ContentLanguage *string `mandatory:"false" contributesTo:"header" name:"Content-Language"`

	// The optional Content-Encoding header that defines the content encodings that were applied to the object to
	// upload. Specifying values for this header has no effect on Object Storage behavior. Programs that read the
	// object determine what to do based on the value provided. For example, you could use this header to determine
	// what decoding mechanisms need to be applied to obtain the media-type specified by the Content-Type header of
	// the object.
	ContentEncoding *string `mandatory:"false" contributesTo:"header" name:"Content-Encoding"`

	// The optional Content-Disposition header that defines presentational information for the object to be
	// returned in GetObject and HeadObject responses. Specifying values for this header has no effect on Object
	// Storage behavior. Programs that read the object determine what to do based on the value provided.
	// For example, you could use this header to let users download objects with custom filenames in a browser.
	ContentDisposition *string `mandatory:"false" contributesTo:"header" name:"Content-Disposition"`

	// The optional Cache-Control header that defines the caching behavior value to be returned in GetObject and
	// HeadObject responses. Specifying values for this header has no effect on Object Storage behavior. Programs
	// that read the object determine what to do based on the value provided.
	// For example, you could use this header to identify objects that require caching restrictions.
	CacheControl *string `mandatory:"false" contributesTo:"header" name:"Cache-Control"`

	// The optional header that specifies "AES256" as the encryption algorithm. For more information, see
	// Using Your Own Keys for Server-Side Encryption (https://docs.oracle.com/iaas/Content/Object/Tasks/usingyourencryptionkeys.htm).
	OpcSseCustomerAlgorithm *string `mandatory:"false" contributesTo:"header" name:"opc-sse-customer-algorithm"`

	// The optional header that specifies the base64-encoded 256-bit encryption key to use to encrypt or
	// decrypt the data. For more information, see
	// Using Your Own Keys for Server-Side Encryption (https://docs.oracle.com/iaas/Content/Object/Tasks/usingyourencryptionkeys.htm).
	OpcSseCustomerKey *string `mandatory:"false" contributesTo:"header" name:"opc-sse-customer-key"`

	// The optional header that specifies the base64-encoded SHA256 hash of the encryption key. This
	// value is used to check the integrity of the encryption key. For more information, see
	// Using Your Own Keys for Server-Side Encryption (https://docs.oracle.com/iaas/Content/Object/Tasks/usingyourencryptionkeys.htm).
	OpcSseCustomerKeySha256 *string `mandatory:"false" contributesTo:"header" name:"opc-sse-customer-key-sha256"`

	// The OCID (https://docs.oracle.com/iaas/Content/General/Concepts/identifiers.htm) of a master encryption key used to call the Key
	// Management service to generate a data encryption key or to encrypt or decrypt a data encryption key.
	OpcSseKmsKeyId *string `mandatory:"false" contributesTo:"header" name:"opc-sse-kms-key-id"`

	// The storage tier that the object should be stored in. If not specified, the object will be stored in
	// the same storage tier as the bucket.
	StorageTier PutObjectStorageTierEnum `mandatory:"false" contributesTo:"header" name:"storage-tier"`

	// Optional user-defined metadata key and value.
	OpcMeta map[string]string `mandatory:"false" contributesTo:"header-collection" prefix:"opc-meta-"`

	// Metadata about the request. This information will not be transmitted to the service, but
	// represents information that the SDK will consume to drive retry behavior.
	RequestMetadata common.RequestMetadata
}

func (request PutObjectRequest) String() string {
	return common.PointerString(request)
}

// HTTPRequest implements the OCIRequest interface
func (request PutObjectRequest) HTTPRequest(method, path string, binaryRequestBody *common.OCIReadSeekCloser, extraHeaders map[string]string) (http.Request, error) {
	httpRequest, err := common.MakeDefaultHTTPRequestWithTaggedStructAndExtraHeaders(method, path, request, extraHeaders)
	if err == nil && binaryRequestBody.Seekable() {
		common.UpdateRequestBinaryBody(&httpRequest, binaryRequestBody)
	}
	return httpRequest, err
}

// BinaryRequestBody implements the OCIRequest interface
func (request PutObjectRequest) BinaryRequestBody() (*common.OCIReadSeekCloser, bool) {
	rsc := common.NewOCIReadSeekCloser(request.PutObjectBody)
	if rsc.Seekable() {
		return rsc, true
	}
	return nil, true

}

// ReplaceMandatoryParamInPath replaces the mandatory parameter in the path with the value provided.
// Not all services are supporting this feature and this method will be a no-op for those services.
func (request PutObjectRequest) ReplaceMandatoryParamInPath(client *common.BaseClient, mandatoryParamMap map[string][]common.TemplateParamForPerRealmEndpoint) {
	if mandatoryParamMap["namespaceName"] != nil {
		templateParam := mandatoryParamMap["namespaceName"]
		for _, template := range templateParam {
			replacementParam := *request.NamespaceName
			if template.EndsWithDot {
				replacementParam = replacementParam + "."
			}
			client.Host = strings.Replace(client.Host, template.Template, replacementParam, -1)
		}
	}
	if mandatoryParamMap["bucketName"] != nil {
		templateParam := mandatoryParamMap["bucketName"]
		for _, template := range templateParam {
			replacementParam := *request.BucketName
			if template.EndsWithDot {
				replacementParam = replacementParam + "."
			}
			client.Host = strings.Replace(client.Host, template.Template, replacementParam, -1)
		}
	}
	if mandatoryParamMap["objectName"] != nil {
		templateParam := mandatoryParamMap["objectName"]
		for _, template := range templateParam {
			replacementParam := *request.ObjectName
			if template.EndsWithDot {
				replacementParam = replacementParam + "."
			}
			client.Host = strings.Replace(client.Host, template.Template, replacementParam, -1)
		}
	}
}

// RetryPolicy implements the OCIRetryableRequest interface. This retrieves the specified retry policy.
func (request PutObjectRequest) RetryPolicy() *common.RetryPolicy {
	return request.RequestMetadata.RetryPolicy
}

// ValidateEnumValue returns an error when providing an unsupported enum value
// This function is being called during constructing API request process
// Not recommended for calling this function directly
func (request PutObjectRequest) ValidateEnumValue() (bool, error) {
	errMessage := []string{}
	if _, ok := GetMappingPutObjectOpcChecksumAlgorithmEnum(string(request.OpcChecksumAlgorithm)); !ok && request.OpcChecksumAlgorithm != "" {
		errMessage = append(errMessage, fmt.Sprintf("unsupported enum value for OpcChecksumAlgorithm: %s. Supported values are: %s.", request.OpcChecksumAlgorithm, strings.Join(GetPutObjectOpcChecksumAlgorithmEnumStringValues(), ",")))
	}
	if _, ok := GetMappingPutObjectStorageTierEnum(string(request.StorageTier)); !ok && request.StorageTier != "" {
		errMessage = append(errMessage, fmt.Sprintf("unsupported enum value for StorageTier: %s. Supported values are: %s.", request.StorageTier, strings.Join(GetPutObjectStorageTierEnumStringValues(), ",")))
	}
	if len(errMessage) > 0 {
		return true, fmt.Errorf("%s", strings.Join(errMessage, "\n"))
	}
	return false, nil
}

// PutObjectResponse wrapper for the PutObject operation
type PutObjectResponse struct {

	// The underlying http response
	RawResponse *http.Response

	// Echoes back the value passed in the opc-client-request-id header, for use by clients when debugging.
	OpcClientRequestId *string `presentIn:"header" name:"opc-client-request-id"`

	// Unique Oracle-assigned identifier for the request. If you need to contact Oracle about a particular
	// request, provide this request ID.
	OpcRequestId *string `presentIn:"header" name:"opc-request-id"`

	// The base64-encoded MD5 hash of the request body as computed by the server.
	OpcContentMd5 *string `presentIn:"header" name:"opc-content-md5"`

	// The base64-encoded, 32-bit CRC32C (Castagnoli) checksum of the request body as computed by the server. Applicable only if CRC32C was specified in opc-checksum-algorithm request header during upload.
	OpcContentCrc32c *string `presentIn:"header" name:"opc-content-crc32c"`

	// The base64-encoded SHA256 hash of the request body as computed by the server. Applicable only if SHA256 was specified in opc-checksum-algorithm request header during upload.
	OpcContentSha256 *string `presentIn:"header" name:"opc-content-sha256"`

	// The base64-encoded SHA384 hash of the request body as computed by the server. Applicable only if SHA384 was specified in opc-checksum-algorithm request header during upload.
	OpcContentSha384 *string `presentIn:"header" name:"opc-content-sha384"`

	// The entity tag (ETag) for the object.
	ETag *string `presentIn:"header" name:"etag"`

	// The time the object was modified, as described in RFC 2616 (https://tools.ietf.org/html/rfc2616#section-14.29).
	LastModified *common.SDKTime `presentIn:"header" name:"last-modified"`

	// VersionId of the newly created object
	VersionId *string `presentIn:"header" name:"version-id"`
}

func (response PutObjectResponse) String() string {
	return common.PointerString(response)
}

// HTTPResponse implements the OCIResponse interface
func (response PutObjectResponse) HTTPResponse() *http.Response {
	return response.RawResponse
}

// PutObjectOpcChecksumAlgorithmEnum Enum with underlying type: string
type PutObjectOpcChecksumAlgorithmEnum string

// Set of constants representing the allowable values for PutObjectOpcChecksumAlgorithmEnum
const (
	PutObjectOpcChecksumAlgorithmCrc32c PutObjectOpcChecksumAlgorithmEnum = "CRC32C"
	PutObjectOpcChecksumAlgorithmSha256 PutObjectOpcChecksumAlgorithmEnum = "SHA256"
	PutObjectOpcChecksumAlgorithmSha384 PutObjectOpcChecksumAlgorithmEnum = "SHA384"
)

var mappingPutObjectOpcChecksumAlgorithmEnum = map[string]PutObjectOpcChecksumAlgorithmEnum{
	"CRC32C": PutObjectOpcChecksumAlgorithmCrc32c,
	"SHA256": PutObjectOpcChecksumAlgorithmSha256,
	"SHA384": PutObjectOpcChecksumAlgorithmSha384,
}

var mappingPutObjectOpcChecksumAlgorithmEnumLowerCase = map[string]PutObjectOpcChecksumAlgorithmEnum{
	"crc32c": PutObjectOpcChecksumAlgorithmCrc32c,
	"sha256": PutObjectOpcChecksumAlgorithmSha256,
	"sha384": PutObjectOpcChecksumAlgorithmSha384,
}

// GetPutObjectOpcChecksumAlgorithmEnumValues Enumerates the set of values for PutObjectOpcChecksumAlgorithmEnum
func GetPutObjectOpcChecksumAlgorithmEnumValues() []PutObjectOpcChecksumAlgorithmEnum {
	values := make([]PutObjectOpcChecksumAlgorithmEnum, 0)
	for _, v := range mappingPutObjectOpcChecksumAlgorithmEnum {
		values = append(values, v)
	}
	return values
}

// GetPutObjectOpcChecksumAlgorithmEnumStringValues Enumerates the set of values in String for PutObjectOpcChecksumAlgorithmEnum
func GetPutObjectOpcChecksumAlgorithmEnumStringValues() []string {
	return []string{
		"CRC32C",
		"SHA256",
		"SHA384",
	}
}

// GetMappingPutObjectOpcChecksumAlgorithmEnum performs case Insensitive comparison on enum value and return the desired enum
func GetMappingPutObjectOpcChecksumAlgorithmEnum(val string) (PutObjectOpcChecksumAlgorithmEnum, bool) {
	enum, ok := mappingPutObjectOpcChecksumAlgorithmEnumLowerCase[strings.ToLower(val)]
	return enum, ok
}

// PutObjectStorageTierEnum Enum with underlying type: string
type PutObjectStorageTierEnum string

// Set of constants representing the allowable values for PutObjectStorageTierEnum
const (
	PutObjectStorageTierStandard         PutObjectStorageTierEnum = "Standard"
	PutObjectStorageTierInfrequentaccess PutObjectStorageTierEnum = "InfrequentAccess"
	PutObjectStorageTierArchive          PutObjectStorageTierEnum = "Archive"
)

var mappingPutObjectStorageTierEnum = map[string]PutObjectStorageTierEnum{
	"Standard":         PutObjectStorageTierStandard,
	"InfrequentAccess": PutObjectStorageTierInfrequentaccess,
	"Archive":          PutObjectStorageTierArchive,
}

var mappingPutObjectStorageTierEnumLowerCase = map[string]PutObjectStorageTierEnum{
	"standard":         PutObjectStorageTierStandard,
	"infrequentaccess": PutObjectStorageTierInfrequentaccess,
	"archive":          PutObjectStorageTierArchive,
}

// GetPutObjectStorageTierEnumValues Enumerates the set of values for PutObjectStorageTierEnum
func GetPutObjectStorageTierEnumValues() []PutObjectStorageTierEnum {
	values := make([]PutObjectStorageTierEnum, 0)
	for _, v := range mappingPutObjectStorageTierEnum {
		values = append(values, v)
	}
	return values
}

// GetPutObjectStorageTierEnumStringValues Enumerates the set of values in String for PutObjectStorageTierEnum
func GetPutObjectStorageTierEnumStringValues() []string {
	return []string{
		"Standard",
		"InfrequentAccess",
		"Archive",
	}
}

// GetMappingPutObjectStorageTierEnum performs case Insensitive comparison on enum value and return the desired enum
func GetMappingPutObjectStorageTierEnum(val string) (PutObjectStorageTierEnum, bool) {
	enum, ok := mappingPutObjectStorageTierEnumLowerCase[strings.ToLower(val)]
	return enum, ok
}
