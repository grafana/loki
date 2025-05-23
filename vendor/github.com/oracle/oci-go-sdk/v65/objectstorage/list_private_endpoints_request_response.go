// Copyright (c) 2016, 2018, 2025, Oracle and/or its affiliates.  All rights reserved.
// This software is dual-licensed to you under the Universal Permissive License (UPL) 1.0 as shown at https://oss.oracle.com/licenses/upl or Apache License 2.0 as shown at http://www.apache.org/licenses/LICENSE-2.0. You may choose either license.
// Code generated. DO NOT EDIT.

package objectstorage

import (
	"fmt"
	"github.com/oracle/oci-go-sdk/v65/common"
	"net/http"
	"strings"
)

// ListPrivateEndpointsRequest wrapper for the ListPrivateEndpoints operation
//
// # See also
//
// Click https://docs.oracle.com/en-us/iaas/tools/go-sdk-examples/latest/objectstorage/ListPrivateEndpoints.go.html to see an example of how to use ListPrivateEndpointsRequest.
type ListPrivateEndpointsRequest struct {

	// The Object Storage namespace used for the request.
	NamespaceName *string `mandatory:"true" contributesTo:"path" name:"namespaceName"`

	// The ID of the compartment in which to list buckets.
	CompartmentId *string `mandatory:"true" contributesTo:"query" name:"compartmentId"`

	// For list pagination. The maximum number of results per page, or items to return in a paginated
	// "List" call. For important details about how pagination works, see
	// List Pagination (https://docs.oracle.com/iaas/Content/API/Concepts/usingapi.htm#nine).
	Limit *int `mandatory:"false" contributesTo:"query" name:"limit"`

	// For list pagination. The value of the `opc-next-page` response header from the previous "List" call. For important
	// details about how pagination works, see List Pagination (https://docs.oracle.com/iaas/Content/API/Concepts/usingapi.htm#nine).
	Page *string `mandatory:"false" contributesTo:"query" name:"page"`

	// PrivateEndpoint summary in list of PrivateEndpoints includes the 'namespace', 'name', 'compartmentId',
	// 'createdBy', 'timeCreated', 'timeModified' and 'etag' fields.
	// This parameter can also include 'tags' (freeformTags and definedTags).
	// The only supported value of this parameter is 'tags' for now. Example 'tags'.
	Fields []ListPrivateEndpointsFieldsEnum `contributesTo:"query" name:"fields" omitEmpty:"true" collectionFormat:"csv"`

	// The client request ID for tracing.
	OpcClientRequestId *string `mandatory:"false" contributesTo:"header" name:"opc-client-request-id"`

	// The lifecycle state of the Private Endpoint
	LifecycleState PrivateEndpointLifecycleStateEnum `mandatory:"false" contributesTo:"query" name:"lifecycleState" omitEmpty:"true"`

	// Metadata about the request. This information will not be transmitted to the service, but
	// represents information that the SDK will consume to drive retry behavior.
	RequestMetadata common.RequestMetadata
}

func (request ListPrivateEndpointsRequest) String() string {
	return common.PointerString(request)
}

// HTTPRequest implements the OCIRequest interface
func (request ListPrivateEndpointsRequest) HTTPRequest(method, path string, binaryRequestBody *common.OCIReadSeekCloser, extraHeaders map[string]string) (http.Request, error) {

	_, err := request.ValidateEnumValue()
	if err != nil {
		return http.Request{}, err
	}
	return common.MakeDefaultHTTPRequestWithTaggedStructAndExtraHeaders(method, path, request, extraHeaders)
}

// BinaryRequestBody implements the OCIRequest interface
func (request ListPrivateEndpointsRequest) BinaryRequestBody() (*common.OCIReadSeekCloser, bool) {

	return nil, false

}

// ReplaceMandatoryParamInPath replaces the mandatory parameter in the path with the value provided.
// Not all services are supporting this feature and this method will be a no-op for those services.
func (request ListPrivateEndpointsRequest) ReplaceMandatoryParamInPath(client *common.BaseClient, mandatoryParamMap map[string][]common.TemplateParamForPerRealmEndpoint) {
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
	if mandatoryParamMap["compartmentId"] != nil {
		templateParam := mandatoryParamMap["compartmentId"]
		for _, template := range templateParam {
			replacementParam := *request.CompartmentId
			if template.EndsWithDot {
				replacementParam = replacementParam + "."
			}
			client.Host = strings.Replace(client.Host, template.Template, replacementParam, -1)
		}
	}
}

// RetryPolicy implements the OCIRetryableRequest interface. This retrieves the specified retry policy.
func (request ListPrivateEndpointsRequest) RetryPolicy() *common.RetryPolicy {
	return request.RequestMetadata.RetryPolicy
}

// ValidateEnumValue returns an error when providing an unsupported enum value
// This function is being called during constructing API request process
// Not recommended for calling this function directly
func (request ListPrivateEndpointsRequest) ValidateEnumValue() (bool, error) {
	errMessage := []string{}
	for _, val := range request.Fields {
		if _, ok := GetMappingListPrivateEndpointsFieldsEnum(string(val)); !ok && val != "" {
			errMessage = append(errMessage, fmt.Sprintf("unsupported enum value for Fields: %s. Supported values are: %s.", val, strings.Join(GetListPrivateEndpointsFieldsEnumStringValues(), ",")))
		}
	}

	if _, ok := GetMappingPrivateEndpointLifecycleStateEnum(string(request.LifecycleState)); !ok && request.LifecycleState != "" {
		errMessage = append(errMessage, fmt.Sprintf("unsupported enum value for LifecycleState: %s. Supported values are: %s.", request.LifecycleState, strings.Join(GetPrivateEndpointLifecycleStateEnumStringValues(), ",")))
	}
	if len(errMessage) > 0 {
		return true, fmt.Errorf("%s", strings.Join(errMessage, "\n"))
	}
	return false, nil
}

// ListPrivateEndpointsResponse wrapper for the ListPrivateEndpoints operation
type ListPrivateEndpointsResponse struct {

	// The underlying http response
	RawResponse *http.Response

	// A list of []PrivateEndpointSummary instances
	Items []PrivateEndpointSummary `presentIn:"body"`

	// Echoes back the value passed in the opc-client-request-id header, for use by clients when debugging.
	OpcClientRequestId *string `presentIn:"header" name:"opc-client-request-id"`

	// Unique Oracle-assigned identifier for the request. If you need to contact Oracle about a particular
	// request, provide this request ID.
	OpcRequestId *string `presentIn:"header" name:"opc-request-id"`

	// For paginating a list of PEs.
	// In the GET request, set the limit to the number of Private Endpoint items that you want returned in the response.
	// If the `opc-next-page` header appears in the response, then this is a partial list and there are additional
	// Private Endpoint's to get. Include the header's value as the `page` parameter in the subsequent GET request to get the
	// next batch of PEs. Repeat this process to retrieve the entire list of Private Endpoint's.
	// By default, the page limit is set to 25 Private Endpoint's per page, but you can specify a value from 1 to 1000.
	// For more details about how pagination works, see List Pagination (https://docs.oracle.com/iaas/Content/API/Concepts/usingapi.htm#nine).
	OpcNextPage *string `presentIn:"header" name:"opc-next-page"`
}

func (response ListPrivateEndpointsResponse) String() string {
	return common.PointerString(response)
}

// HTTPResponse implements the OCIResponse interface
func (response ListPrivateEndpointsResponse) HTTPResponse() *http.Response {
	return response.RawResponse
}

// ListPrivateEndpointsFieldsEnum Enum with underlying type: string
type ListPrivateEndpointsFieldsEnum string

// Set of constants representing the allowable values for ListPrivateEndpointsFieldsEnum
const (
	ListPrivateEndpointsFieldsTags ListPrivateEndpointsFieldsEnum = "tags"
)

var mappingListPrivateEndpointsFieldsEnum = map[string]ListPrivateEndpointsFieldsEnum{
	"tags": ListPrivateEndpointsFieldsTags,
}

var mappingListPrivateEndpointsFieldsEnumLowerCase = map[string]ListPrivateEndpointsFieldsEnum{
	"tags": ListPrivateEndpointsFieldsTags,
}

// GetListPrivateEndpointsFieldsEnumValues Enumerates the set of values for ListPrivateEndpointsFieldsEnum
func GetListPrivateEndpointsFieldsEnumValues() []ListPrivateEndpointsFieldsEnum {
	values := make([]ListPrivateEndpointsFieldsEnum, 0)
	for _, v := range mappingListPrivateEndpointsFieldsEnum {
		values = append(values, v)
	}
	return values
}

// GetListPrivateEndpointsFieldsEnumStringValues Enumerates the set of values in String for ListPrivateEndpointsFieldsEnum
func GetListPrivateEndpointsFieldsEnumStringValues() []string {
	return []string{
		"tags",
	}
}

// GetMappingListPrivateEndpointsFieldsEnum performs case Insensitive comparison on enum value and return the desired enum
func GetMappingListPrivateEndpointsFieldsEnum(val string) (ListPrivateEndpointsFieldsEnum, bool) {
	enum, ok := mappingListPrivateEndpointsFieldsEnumLowerCase[strings.ToLower(val)]
	return enum, ok
}
