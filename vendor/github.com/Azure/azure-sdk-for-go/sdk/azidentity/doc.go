//go:build go1.16
// +build go1.16

// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

/*
azidentity provides Azure Active Directory token authentication for Azure SDK clients.

Azure SDK clients supporting token authentication can use any azidentity credential.
For example, authenticating a resource group client with DefaultAzureCredential:

	cred, err := azidentity.NewDefaultAzureCredential(nil)
	...
	client := armresources.NewResourceGroupsClient("subscription ID", cred, nil)

Different credential types implement different authentication flows. Each credential's
documentation describes how it authenticates.
*/
package azidentity
