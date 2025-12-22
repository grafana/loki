// Copyright (c) 2016, 2018, 2025, Oracle and/or its affiliates.  All rights reserved.
// This software is dual-licensed to you under the Universal Permissive License (UPL) 1.0 as shown at https://oss.oracle.com/licenses/upl or Apache License 2.0 as shown at http://www.apache.org/licenses/LICENSE-2.0. You may choose either license.
// Code generated. DO NOT EDIT.

// Object Storage Service API
//
// Use Object Storage and Archive Storage APIs to manage buckets, objects, and related resources.
// For more information, see Overview of Object Storage (https://docs.oracle.com/iaas/Content/Object/Concepts/objectstorageoverview.htm) and
// Overview of Archive Storage (https://docs.oracle.com/iaas/Content/Archive/Concepts/archivestorageoverview.htm).
//

package objectstorage

import (
	"fmt"
	"github.com/oracle/oci-go-sdk/v65/common"
	"strings"
)

// PrivateEndpoint A private endpoint makes your service accessible through a private IP in the customer's private network. A private endpoint has a name and is associated with a namespace and a single compartment.
type PrivateEndpoint struct {

	// This name associated with the endpoint. Valid characters are uppercase or lowercase letters, numbers, hyphens,
	//  underscores, and periods.
	// Example: my-new-private-endpoint1
	Name *string `mandatory:"true" json:"name"`

	// The Object Storage namespace associated with the private enpoint.
	Namespace *string `mandatory:"true" json:"namespace"`

	// The compartment which is associated with the Private Endpoint.
	CompartmentId *string `mandatory:"true" json:"compartmentId"`

	// The OCID (https://docs.oracle.com/iaas/Content/General/Concepts/identifiers.htm) of the user who created the Private Endpoint.
	CreatedBy *string `mandatory:"true" json:"createdBy"`

	// The date and time the Private Endpoint was created, as described in RFC 2616 (https://tools.ietf.org/html/rfc2616#section-14.29).
	TimeCreated *common.SDKTime `mandatory:"true" json:"timeCreated"`

	// The date and time the Private Endpoint was updated, as described in RFC 2616 (https://tools.ietf.org/html/rfc2616#section-14.29).
	TimeModified *common.SDKTime `mandatory:"true" json:"timeModified"`

	// The OCID of the customer's subnet where the private endpoint VNIC will reside.
	SubnetId *string `mandatory:"true" json:"subnetId"`

	// The private IP address to assign to this private endpoint. If you provide a value,
	// it must be an available IP address in the customer's subnet. If it's not available, an error
	// is returned.
	// If you do not provide a value, an available IP address in the subnet is automatically chosen.
	PrivateEndpointIp *string `mandatory:"true" json:"privateEndpointIp"`

	// A prefix to use for the private endpoint. The customer VCN's DNS records are
	// updated with this prefix. The prefix input from the customer will be the first sub-domain in the endpointFqdn.
	// Example: If the prefix chosen is "abc", then the endpointFqdn will be 'abc.private.objectstorage.<region>.oraclecloud.com'
	Prefix *string `mandatory:"true" json:"prefix"`

	Fqdns *Fqdns `mandatory:"true" json:"fqdns"`

	// The entity tag (ETag) for the Private Endpoint.
	Etag *string `mandatory:"true" json:"etag"`

	// A list of targets that can be accessed by the private endpoint. At least one or more access targets is required for a private endpoint.
	AccessTargets []AccessTargetDetails `mandatory:"true" json:"accessTargets"`

	// A list of additional prefix that you can provide along with any other prefix. These resulting endpointFqdn's are added to the
	// customer VCN's DNS record.
	AdditionalPrefixes []string `mandatory:"false" json:"additionalPrefixes"`

	// A list of the OCIDs of the network security groups (NSGs) to add the private endpoint's VNIC to.
	// For more information about NSGs, see
	// NetworkSecurityGroup.
	NsgIds []string `mandatory:"false" json:"nsgIds"`

	// The Private Endpoint's lifecycle state.
	LifecycleState PrivateEndpointLifecycleStateEnum `mandatory:"false" json:"lifecycleState,omitempty"`

	// Free-form tags for this resource. Each tag is a simple key-value pair with no predefined name, type, or namespace.
	// For more information, see Resource Tags (https://docs.oracle.com/iaas/Content/General/Concepts/resourcetags.htm).
	// Example: `{"Department": "Finance"}`
	FreeformTags map[string]string `mandatory:"false" json:"freeformTags"`

	// Defined tags for this resource. Each key is predefined and scoped to a namespace.
	// For more information, see Resource Tags (https://docs.oracle.com/iaas/Content/General/Concepts/resourcetags.htm).
	// Example: `{"Operations": {"CostCenter": "42"}}`
	DefinedTags map[string]map[string]interface{} `mandatory:"false" json:"definedTags"`

	// The OCID (https://docs.oracle.com/iaas/Content/General/Concepts/identifiers.htm) of the PrivateEndpoint.
	Id *string `mandatory:"false" json:"id"`
}

func (m PrivateEndpoint) String() string {
	return common.PointerString(m)
}

// ValidateEnumValue returns an error when providing an unsupported enum value
// This function is being called during constructing API request process
// Not recommended for calling this function directly
func (m PrivateEndpoint) ValidateEnumValue() (bool, error) {
	errMessage := []string{}

	if _, ok := GetMappingPrivateEndpointLifecycleStateEnum(string(m.LifecycleState)); !ok && m.LifecycleState != "" {
		errMessage = append(errMessage, fmt.Sprintf("unsupported enum value for LifecycleState: %s. Supported values are: %s.", m.LifecycleState, strings.Join(GetPrivateEndpointLifecycleStateEnumStringValues(), ",")))
	}
	if len(errMessage) > 0 {
		return true, fmt.Errorf("%s", strings.Join(errMessage, "\n"))
	}
	return false, nil
}

// PrivateEndpointLifecycleStateEnum Enum with underlying type: string
type PrivateEndpointLifecycleStateEnum string

// Set of constants representing the allowable values for PrivateEndpointLifecycleStateEnum
const (
	PrivateEndpointLifecycleStateCreating PrivateEndpointLifecycleStateEnum = "CREATING"
	PrivateEndpointLifecycleStateActive   PrivateEndpointLifecycleStateEnum = "ACTIVE"
	PrivateEndpointLifecycleStateInactive PrivateEndpointLifecycleStateEnum = "INACTIVE"
	PrivateEndpointLifecycleStateUpdating PrivateEndpointLifecycleStateEnum = "UPDATING"
	PrivateEndpointLifecycleStateDeleting PrivateEndpointLifecycleStateEnum = "DELETING"
	PrivateEndpointLifecycleStateDeleted  PrivateEndpointLifecycleStateEnum = "DELETED"
	PrivateEndpointLifecycleStateFailed   PrivateEndpointLifecycleStateEnum = "FAILED"
)

var mappingPrivateEndpointLifecycleStateEnum = map[string]PrivateEndpointLifecycleStateEnum{
	"CREATING": PrivateEndpointLifecycleStateCreating,
	"ACTIVE":   PrivateEndpointLifecycleStateActive,
	"INACTIVE": PrivateEndpointLifecycleStateInactive,
	"UPDATING": PrivateEndpointLifecycleStateUpdating,
	"DELETING": PrivateEndpointLifecycleStateDeleting,
	"DELETED":  PrivateEndpointLifecycleStateDeleted,
	"FAILED":   PrivateEndpointLifecycleStateFailed,
}

var mappingPrivateEndpointLifecycleStateEnumLowerCase = map[string]PrivateEndpointLifecycleStateEnum{
	"creating": PrivateEndpointLifecycleStateCreating,
	"active":   PrivateEndpointLifecycleStateActive,
	"inactive": PrivateEndpointLifecycleStateInactive,
	"updating": PrivateEndpointLifecycleStateUpdating,
	"deleting": PrivateEndpointLifecycleStateDeleting,
	"deleted":  PrivateEndpointLifecycleStateDeleted,
	"failed":   PrivateEndpointLifecycleStateFailed,
}

// GetPrivateEndpointLifecycleStateEnumValues Enumerates the set of values for PrivateEndpointLifecycleStateEnum
func GetPrivateEndpointLifecycleStateEnumValues() []PrivateEndpointLifecycleStateEnum {
	values := make([]PrivateEndpointLifecycleStateEnum, 0)
	for _, v := range mappingPrivateEndpointLifecycleStateEnum {
		values = append(values, v)
	}
	return values
}

// GetPrivateEndpointLifecycleStateEnumStringValues Enumerates the set of values in String for PrivateEndpointLifecycleStateEnum
func GetPrivateEndpointLifecycleStateEnumStringValues() []string {
	return []string{
		"CREATING",
		"ACTIVE",
		"INACTIVE",
		"UPDATING",
		"DELETING",
		"DELETED",
		"FAILED",
	}
}

// GetMappingPrivateEndpointLifecycleStateEnum performs case Insensitive comparison on enum value and return the desired enum
func GetMappingPrivateEndpointLifecycleStateEnum(val string) (PrivateEndpointLifecycleStateEnum, bool) {
	enum, ok := mappingPrivateEndpointLifecycleStateEnumLowerCase[strings.ToLower(val)]
	return enum, ok
}
