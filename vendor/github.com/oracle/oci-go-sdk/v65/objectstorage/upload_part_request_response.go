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

// UploadPartRequest wrapper for the UploadPart operation
//
// # See also
//
// Click https://docs.oracle.com/en-us/iaas/tools/go-sdk-examples/latest/objectstorage/UploadPart.go.html to see an example of how to use UploadPartRequest.
type UploadPartRequest struct {

	// The Object Storage namespace used for the request.
	NamespaceName *string `mandatory:"true" contributesTo:"path" name:"namespaceName"`

	// The name of the bucket. Avoid entering confidential information.
	// Example: `my-new-bucket1`
	BucketName *string `mandatory:"true" contributesTo:"path" name:"bucketName"`

	// The name of the object. Avoid entering confidential information.
	// Example: `test/object1.log`
	ObjectName *string `mandatory:"true" contributesTo:"path" name:"objectName"`

	// The upload ID for a multipart upload.
	UploadId *string `mandatory:"true" contributesTo:"query" name:"uploadId"`

	// The part number that identifies the object part currently being uploaded.
	UploadPartNum *int `mandatory:"true" contributesTo:"query" name:"uploadPartNum"`

	// The content length of the body.
	ContentLength *int64 `mandatory:"false" contributesTo:"header" name:"Content-Length"`

	// The part being uploaded to the Object Storage service.
	UploadPartBody io.ReadCloser `mandatory:"true" contributesTo:"body" encoding:"binary"`

	// The client request ID for tracing.
	OpcClientRequestId *string `mandatory:"false" contributesTo:"header" name:"opc-client-request-id"`

	// The entity tag (ETag) to match with the ETag of an existing resource. If the specified ETag matches the ETag of
	// the existing resource, GET and HEAD requests will return the resource and PUT and POST requests will upload
	// the resource.
	IfMatch *string `mandatory:"false" contributesTo:"header" name:"if-match"`

	// The entity tag (ETag) to avoid matching. The only valid value is '*', which indicates that the request should
	// fail if the resource already exists.
	IfNoneMatch *string `mandatory:"false" contributesTo:"header" name:"if-none-match"`

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
	OpcChecksumAlgorithm UploadPartOpcChecksumAlgorithmEnum `mandatory:"false" contributesTo:"header" name:"opc-checksum-algorithm"`

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

	// Metadata about the request. This information will not be transmitted to the service, but
	// represents information that the SDK will consume to drive retry behavior.
	RequestMetadata common.RequestMetadata
}

func (request UploadPartRequest) String() string {
	return common.PointerString(request)
}

// HTTPRequest implements the OCIRequest interface
func (request UploadPartRequest) HTTPRequest(method, path string, binaryRequestBody *common.OCIReadSeekCloser, extraHeaders map[string]string) (http.Request, error) {
	httpRequest, err := common.MakeDefaultHTTPRequestWithTaggedStructAndExtraHeaders(method, path, request, extraHeaders)
	if err == nil && binaryRequestBody.Seekable() {
		common.UpdateRequestBinaryBody(&httpRequest, binaryRequestBody)
	}
	return httpRequest, err
}

// BinaryRequestBody implements the OCIRequest interface
func (request UploadPartRequest) BinaryRequestBody() (*common.OCIReadSeekCloser, bool) {
	rsc := common.NewOCIReadSeekCloser(request.UploadPartBody)
	if rsc.Seekable() {
		return rsc, true
	}
	return nil, true

}

// ReplaceMandatoryParamInPath replaces the mandatory parameter in the path with the value provided.
// Not all services are supporting this feature and this method will be a no-op for those services.
func (request UploadPartRequest) ReplaceMandatoryParamInPath(client *common.BaseClient, mandatoryParamMap map[string][]common.TemplateParamForPerRealmEndpoint) {
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
	if mandatoryParamMap["uploadId"] != nil {
		templateParam := mandatoryParamMap["uploadId"]
		for _, template := range templateParam {
			replacementParam := *request.UploadId
			if template.EndsWithDot {
				replacementParam = replacementParam + "."
			}
			client.Host = strings.Replace(client.Host, template.Template, replacementParam, -1)
		}
	}
}

// RetryPolicy implements the OCIRetryableRequest interface. This retrieves the specified retry policy.
func (request UploadPartRequest) RetryPolicy() *common.RetryPolicy {
	return request.RequestMetadata.RetryPolicy
}

// ValidateEnumValue returns an error when providing an unsupported enum value
// This function is being called during constructing API request process
// Not recommended for calling this function directly
func (request UploadPartRequest) ValidateEnumValue() (bool, error) {
	errMessage := []string{}
	if _, ok := GetMappingUploadPartOpcChecksumAlgorithmEnum(string(request.OpcChecksumAlgorithm)); !ok && request.OpcChecksumAlgorithm != "" {
		errMessage = append(errMessage, fmt.Sprintf("unsupported enum value for OpcChecksumAlgorithm: %s. Supported values are: %s.", request.OpcChecksumAlgorithm, strings.Join(GetUploadPartOpcChecksumAlgorithmEnumStringValues(), ",")))
	}
	if len(errMessage) > 0 {
		return true, fmt.Errorf("%s", strings.Join(errMessage, "\n"))
	}
	return false, nil
}

// UploadPartResponse wrapper for the UploadPart operation
type UploadPartResponse struct {

	// The underlying http response
	RawResponse *http.Response

	// Echoes back the value passed in the opc-client-request-id header, for use by clients when debugging.
	OpcClientRequestId *string `presentIn:"header" name:"opc-client-request-id"`

	// Unique Oracle-assigned identifier for the request. If you need to contact Oracle about a particular
	// request, provide this request ID.
	OpcRequestId *string `presentIn:"header" name:"opc-request-id"`

	// The base64-encoded MD5 hash of the request body, as computed by the server.
	OpcContentMd5 *string `presentIn:"header" name:"opc-content-md5"`

	// The base64-encoded, 32-bit CRC32C (Castagnoli) checksum of the request body as computed by the server. Applicable only if CRC32C was specified in opc-checksum-algorithm request header during upload.
	OpcContentCrc32c *string `presentIn:"header" name:"opc-content-crc32c"`

	// The base64-encoded SHA256 hash of the request body as computed by the server. Applicable only if SHA256 was specified in opc-checksum-algorithm request header during upload.
	OpcContentSha256 *string `presentIn:"header" name:"opc-content-sha256"`

	// The base64-encoded SHA384 hash of the request body as computed by the server. Applicable only if SHA384 was specified in opc-checksum-algorithm request header during upload.
	OpcContentSha384 *string `presentIn:"header" name:"opc-content-sha384"`

	// The entity tag (ETag) for the object.
	ETag *string `presentIn:"header" name:"etag"`
}

func (response UploadPartResponse) String() string {
	return common.PointerString(response)
}

// HTTPResponse implements the OCIResponse interface
func (response UploadPartResponse) HTTPResponse() *http.Response {
	return response.RawResponse
}

// UploadPartOpcChecksumAlgorithmEnum Enum with underlying type: string
type UploadPartOpcChecksumAlgorithmEnum string

// Set of constants representing the allowable values for UploadPartOpcChecksumAlgorithmEnum
const (
	UploadPartOpcChecksumAlgorithmCrc32c UploadPartOpcChecksumAlgorithmEnum = "CRC32C"
	UploadPartOpcChecksumAlgorithmSha256 UploadPartOpcChecksumAlgorithmEnum = "SHA256"
	UploadPartOpcChecksumAlgorithmSha384 UploadPartOpcChecksumAlgorithmEnum = "SHA384"
)

var mappingUploadPartOpcChecksumAlgorithmEnum = map[string]UploadPartOpcChecksumAlgorithmEnum{
	"CRC32C": UploadPartOpcChecksumAlgorithmCrc32c,
	"SHA256": UploadPartOpcChecksumAlgorithmSha256,
	"SHA384": UploadPartOpcChecksumAlgorithmSha384,
}

var mappingUploadPartOpcChecksumAlgorithmEnumLowerCase = map[string]UploadPartOpcChecksumAlgorithmEnum{
	"crc32c": UploadPartOpcChecksumAlgorithmCrc32c,
	"sha256": UploadPartOpcChecksumAlgorithmSha256,
	"sha384": UploadPartOpcChecksumAlgorithmSha384,
}

// GetUploadPartOpcChecksumAlgorithmEnumValues Enumerates the set of values for UploadPartOpcChecksumAlgorithmEnum
func GetUploadPartOpcChecksumAlgorithmEnumValues() []UploadPartOpcChecksumAlgorithmEnum {
	values := make([]UploadPartOpcChecksumAlgorithmEnum, 0)
	for _, v := range mappingUploadPartOpcChecksumAlgorithmEnum {
		values = append(values, v)
	}
	return values
}

// GetUploadPartOpcChecksumAlgorithmEnumStringValues Enumerates the set of values in String for UploadPartOpcChecksumAlgorithmEnum
func GetUploadPartOpcChecksumAlgorithmEnumStringValues() []string {
	return []string{
		"CRC32C",
		"SHA256",
		"SHA384",
	}
}

// GetMappingUploadPartOpcChecksumAlgorithmEnum performs case Insensitive comparison on enum value and return the desired enum
func GetMappingUploadPartOpcChecksumAlgorithmEnum(val string) (UploadPartOpcChecksumAlgorithmEnum, bool) {
	enum, ok := mappingUploadPartOpcChecksumAlgorithmEnumLowerCase[strings.ToLower(val)]
	return enum, ok
}
