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
	"strings"
)

// ChecksumAlgorithmEnum Enum with underlying type: string
type ChecksumAlgorithmEnum string

// Set of constants representing the allowable values for ChecksumAlgorithmEnum
const (
	ChecksumAlgorithmCrc32C ChecksumAlgorithmEnum = "CRC32C"
	ChecksumAlgorithmSha256 ChecksumAlgorithmEnum = "SHA256"
	ChecksumAlgorithmSha384 ChecksumAlgorithmEnum = "SHA384"
)

var mappingChecksumAlgorithmEnum = map[string]ChecksumAlgorithmEnum{
	"CRC32C": ChecksumAlgorithmCrc32C,
	"SHA256": ChecksumAlgorithmSha256,
	"SHA384": ChecksumAlgorithmSha384,
}

var mappingChecksumAlgorithmEnumLowerCase = map[string]ChecksumAlgorithmEnum{
	"crc32c": ChecksumAlgorithmCrc32C,
	"sha256": ChecksumAlgorithmSha256,
	"sha384": ChecksumAlgorithmSha384,
}

// GetChecksumAlgorithmEnumValues Enumerates the set of values for ChecksumAlgorithmEnum
func GetChecksumAlgorithmEnumValues() []ChecksumAlgorithmEnum {
	values := make([]ChecksumAlgorithmEnum, 0)
	for _, v := range mappingChecksumAlgorithmEnum {
		values = append(values, v)
	}
	return values
}

// GetChecksumAlgorithmEnumStringValues Enumerates the set of values in String for ChecksumAlgorithmEnum
func GetChecksumAlgorithmEnumStringValues() []string {
	return []string{
		"CRC32C",
		"SHA256",
		"SHA384",
	}
}

// GetMappingChecksumAlgorithmEnum performs case Insensitive comparison on enum value and return the desired enum
func GetMappingChecksumAlgorithmEnum(val string) (ChecksumAlgorithmEnum, bool) {
	enum, ok := mappingChecksumAlgorithmEnumLowerCase[strings.ToLower(val)]
	return enum, ok
}
