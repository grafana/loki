package core

// (C) Copyright IBM Corp. 2019, 2024.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

import (
	"fmt"
	"strings"
)

// GetAuthenticatorFromEnvironment instantiates an Authenticator using service properties
// retrieved from external config sources.
func GetAuthenticatorFromEnvironment(credentialKey string) (authenticator Authenticator, err error) {
	GetLogger().Debug("Get authenticator from environment, key=%s\n", credentialKey)
	properties, err := getServiceProperties(credentialKey)
	if len(properties) == 0 {
		return
	}

	// Determine the authentication type if not specified explicitly.
	authType := properties[PROPNAME_AUTH_TYPE]

	// Support alternate "AUTHTYPE" property.
	if authType == "" {
		authType = properties["AUTHTYPE"]
	}

	// Determine a default auth type if one wasn't specified.
	if authType == "" {
		// If the APIKEY property is specified, then we'll guess IAM... otherwise CR Auth.
		if properties[PROPNAME_APIKEY] != "" {
			authType = AUTHTYPE_IAM
		} else {
			authType = AUTHTYPE_CONTAINER
		}
	}

	// Create the authenticator appropriate for the auth type.
	if strings.EqualFold(authType, AUTHTYPE_BASIC) {
		authenticator, err = newBasicAuthenticatorFromMap(properties)
	} else if strings.EqualFold(authType, AUTHTYPE_BEARER_TOKEN) {
		authenticator, err = newBearerTokenAuthenticatorFromMap(properties)
	} else if strings.EqualFold(authType, AUTHTYPE_IAM) {
		authenticator, err = newIamAuthenticatorFromMap(properties)
	} else if strings.EqualFold(authType, AUTHTYPE_IAM_ASSUME) {
		authenticator, err = newIamAssumeAuthenticatorFromMap(properties)
	} else if strings.EqualFold(authType, AUTHTYPE_CONTAINER) {
		authenticator, err = newContainerAuthenticatorFromMap(properties)
	} else if strings.EqualFold(authType, AUTHTYPE_VPC) {
		authenticator, err = newVpcInstanceAuthenticatorFromMap(properties)
	} else if strings.EqualFold(authType, AUTHTYPE_CP4D) {
		authenticator, err = newCloudPakForDataAuthenticatorFromMap(properties)
	} else if strings.EqualFold(authType, AUTHTYPE_MCSP) {
		authenticator, err = newMCSPAuthenticatorFromMap(properties)
	} else if strings.EqualFold(authType, AUTHTYPE_NOAUTH) {
		authenticator, err = NewNoAuthAuthenticator()
	} else {
		err = SDKErrorf(
			nil,
			fmt.Sprintf(ERRORMSG_AUTHTYPE_UNKNOWN, authType),
			"unknown-auth-type",
			getComponentInfo(),
		)
	}

	if authenticator != nil {
		GetLogger().Debug("Returning authenticator, type=%s\n", authenticator.AuthenticationType())
	}

	return
}
