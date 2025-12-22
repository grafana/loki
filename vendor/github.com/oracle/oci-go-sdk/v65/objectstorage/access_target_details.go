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

// AccessTargetDetails Details of the targets that can be accessed by the private endpoint.
type AccessTargetDetails struct {

	// The Object Storage namespace which the private endpoint can access. Wildcards ('*') are allowed. If value is '*', it means all namespaces can be accessed. It cannot be a regex.
	Namespace *string `mandatory:"true" json:"namespace"`

	// The compartment ID which the private endpoint can access. Wildcards ('*') are allowed. If value is '*', it means all compartments in the specified namespace can be accessed. It cannot be a regex.
	CompartmentId *string `mandatory:"true" json:"compartmentId"`

	// The name of the bucket. Avoid entering confidential information. Wildcards ('*') are allowed. If value is '*', it means all buckets in the specified namespace and compartment can be accessed. It cannot be a regex.
	// Example: my-new-bucket1
	Bucket *string `mandatory:"true" json:"bucket"`
}

func (m AccessTargetDetails) String() string {
	return common.PointerString(m)
}

// ValidateEnumValue returns an error when providing an unsupported enum value
// This function is being called during constructing API request process
// Not recommended for calling this function directly
func (m AccessTargetDetails) ValidateEnumValue() (bool, error) {
	errMessage := []string{}

	if len(errMessage) > 0 {
		return true, fmt.Errorf("%s", strings.Join(errMessage, "\n"))
	}
	return false, nil
}
