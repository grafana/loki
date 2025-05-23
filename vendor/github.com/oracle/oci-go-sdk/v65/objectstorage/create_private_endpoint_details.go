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

// CreatePrivateEndpointDetails Details to create a private endpoint
type CreatePrivateEndpointDetails struct {

	// This name associated with the endpoint. Valid characters are uppercase or lowercase letters, numbers, hyphens,
	//  underscores, and periods.
	// Example: my-new-private-endpoint1
	Name *string `mandatory:"true" json:"name"`

	// The ID of the compartment in which to create the Private Endpoint.
	CompartmentId *string `mandatory:"true" json:"compartmentId"`

	// The OCID of the customer's subnet where the private endpoint VNIC will reside.
	SubnetId *string `mandatory:"true" json:"subnetId"`

	// A prefix to use for the private endpoint. The customer VCN's DNS records are
	// updated with this prefix. The prefix input from the customer will be the first sub-domain in the endpointFqdn.
	// Example: If the prefix chosen is "abc", then the endpointFqdn will be 'abc.private.objectstorage.<region>.oraclecloud.com'
	Prefix *string `mandatory:"true" json:"prefix"`

	// A list of targets that can be accessed by the private endpoint.
	AccessTargets []AccessTargetDetails `mandatory:"true" json:"accessTargets"`

	// A list of additional prefix that you can provide along with any other prefix. These resulting endpointFqdn's are added to the
	// customer VCN's DNS record.
	AdditionalPrefixes []string `mandatory:"false" json:"additionalPrefixes"`

	// The private IP address to assign to this private endpoint. If you provide a value,
	// it must be an available IP address in the customer's subnet. If it's not available, an error
	// is returned.
	// If you do not provide a value, an available IP address in the subnet is automatically chosen.
	PrivateEndpointIp *string `mandatory:"false" json:"privateEndpointIp"`

	// A list of the OCIDs of the network security groups (NSGs) to add the private endpoint's VNIC to.
	// For more information about NSGs, see
	// NetworkSecurityGroup.
	NsgIds []string `mandatory:"false" json:"nsgIds"`

	// Free-form tags for this resource. Each tag is a simple key-value pair with no predefined name, type, or namespace.
	// For more information, see Resource Tags (https://docs.oracle.com/iaas/Content/General/Concepts/resourcetags.htm).
	// Example: `{"Department": "Finance"}`
	FreeformTags map[string]string `mandatory:"false" json:"freeformTags"`

	// Defined tags for this resource. Each key is predefined and scoped to a namespace.
	// For more information, see Resource Tags (https://docs.oracle.com/iaas/Content/General/Concepts/resourcetags.htm).
	// Example: `{"Operations": {"CostCenter": "42"}}`
	DefinedTags map[string]map[string]interface{} `mandatory:"false" json:"definedTags"`
}

func (m CreatePrivateEndpointDetails) String() string {
	return common.PointerString(m)
}

// ValidateEnumValue returns an error when providing an unsupported enum value
// This function is being called during constructing API request process
// Not recommended for calling this function directly
func (m CreatePrivateEndpointDetails) ValidateEnumValue() (bool, error) {
	errMessage := []string{}

	if len(errMessage) > 0 {
		return true, fmt.Errorf("%s", strings.Join(errMessage, "\n"))
	}
	return false, nil
}
