// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package azblob

type CreateContainerOptions struct {
	// Specifies whether data in the container may be accessed publicly and the level of access
	Access *PublicAccessType

	// Optional. Specifies a user-defined name-value pair associated with the blob.
	Metadata map[string]string

	// Optional. Specifies the encryption scope settings to set on the container.
	cpkScope *ContainerCpkScopeInfo
}

func (o *CreateContainerOptions) pointers() (*ContainerCreateOptions, *ContainerCpkScopeInfo) {
	if o == nil {
		return nil, nil
	}

	basicOptions := ContainerCreateOptions{
		Access:   o.Access,
		Metadata: o.Metadata,
	}

	return &basicOptions, o.cpkScope
}

type DeleteContainerOptions struct {
	LeaseAccessConditions    *LeaseAccessConditions
	ModifiedAccessConditions *ModifiedAccessConditions
}

func (o *DeleteContainerOptions) pointers() (*ContainerDeleteOptions, *LeaseAccessConditions, *ModifiedAccessConditions) {
	if o == nil {
		return nil, nil, nil
	}

	return nil, o.LeaseAccessConditions, o.ModifiedAccessConditions
}

type GetPropertiesOptionsContainer struct {
	ContainerGetPropertiesOptions *ContainerGetPropertiesOptions
	LeaseAccessConditions         *LeaseAccessConditions
}

func (o *GetPropertiesOptionsContainer) pointers() (*ContainerGetPropertiesOptions, *LeaseAccessConditions) {
	if o == nil {
		return nil, nil
	}

	return o.ContainerGetPropertiesOptions, o.LeaseAccessConditions
}

type GetAccessPolicyOptions struct {
	ContainerGetAccessPolicyOptions *ContainerGetAccessPolicyOptions
	LeaseAccessConditions           *LeaseAccessConditions
}

func (o *GetAccessPolicyOptions) pointers() (*ContainerGetAccessPolicyOptions, *LeaseAccessConditions) {
	if o == nil {
		return nil, nil
	}

	return o.ContainerGetAccessPolicyOptions, o.LeaseAccessConditions
}

type SetAccessPolicyOptions struct {
	// At least Access and ContainerACL must be specified
	ContainerSetAccessPolicyOptions ContainerSetAccessPolicyOptions
	AccessConditions                *ContainerAccessConditions
}

func (o *SetAccessPolicyOptions) pointers() (ContainerSetAccessPolicyOptions, *LeaseAccessConditions, *ModifiedAccessConditions) {
	if o == nil {
		return ContainerSetAccessPolicyOptions{}, nil, nil
	}
	mac, lac := o.AccessConditions.pointers()
	return o.ContainerSetAccessPolicyOptions, lac, mac
}

type SetMetadataContainerOptions struct {
	Metadata                 map[string]string
	LeaseAccessConditions    *LeaseAccessConditions
	ModifiedAccessConditions *ModifiedAccessConditions
}

func (o *SetMetadataContainerOptions) pointers() (*ContainerSetMetadataOptions, *LeaseAccessConditions, *ModifiedAccessConditions) {
	if o == nil {
		return nil, nil, nil
	}

	options := ContainerSetMetadataOptions{Metadata: o.Metadata}
	return &options, o.LeaseAccessConditions, o.ModifiedAccessConditions
}
