// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package azblob

const (
	// ETagNone represents an empty entity tag.
	ETagNone = ""

	// ETagAny matches any entity tag.
	ETagAny = "*"
)

// ContainerAccessConditions identifies container-specific access conditions which you optionally set.
type ContainerAccessConditions struct {
	ModifiedAccessConditions *ModifiedAccessConditions
	LeaseAccessConditions    *LeaseAccessConditions
}

func (ac *ContainerAccessConditions) pointers() (*ModifiedAccessConditions, *LeaseAccessConditions) {
	if ac == nil {
		return nil, nil
	}

	return ac.ModifiedAccessConditions, ac.LeaseAccessConditions
}

// BlobAccessConditions identifies blob-specific access conditions which you optionally set.
type BlobAccessConditions struct {
	LeaseAccessConditions    *LeaseAccessConditions
	ModifiedAccessConditions *ModifiedAccessConditions
}

func (ac *BlobAccessConditions) pointers() (*LeaseAccessConditions, *ModifiedAccessConditions) {
	if ac == nil {
		return nil, nil
	}

	return ac.LeaseAccessConditions, ac.ModifiedAccessConditions
}
