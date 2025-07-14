package core

// (C) Copyright IBM Corp. 2019.
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
	"errors"
	"fmt"
	"net/http"
)

// BearerTokenAuthenticator will take a user-supplied bearer token and adds
// it to requests via an Authorization header of the form:
//
//	Authorization: Bearer <bearer-token>
type BearerTokenAuthenticator struct {
	// The bearer token value to be used to authenticate request [required].
	BearerToken string
}

// NewBearerTokenAuthenticator constructs a new BearerTokenAuthenticator instance.
func NewBearerTokenAuthenticator(bearerToken string) (*BearerTokenAuthenticator, error) {
	obj := &BearerTokenAuthenticator{
		BearerToken: bearerToken,
	}
	if err := obj.Validate(); err != nil {
		err = RepurposeSDKProblem(err, "validation-failed")
		return nil, err
	}
	return obj, nil
}

// newBearerTokenAuthenticator : Constructs a new BearerTokenAuthenticator instance from a map.
func newBearerTokenAuthenticatorFromMap(properties map[string]string) (*BearerTokenAuthenticator, error) {
	if properties == nil {
		err := errors.New(ERRORMSG_PROPS_MAP_NIL)
		return nil, SDKErrorf(err, "", "missing-props", getComponentInfo())
	}

	return NewBearerTokenAuthenticator(properties[PROPNAME_BEARER_TOKEN])
}

// AuthenticationType returns the authentication type for this authenticator.
func (BearerTokenAuthenticator) AuthenticationType() string {
	return AUTHTYPE_BEARER_TOKEN
}

// Authenticate adds bearer authentication information to the request.
//
// The bearer token will be added to the request's headers in the form:
//
//	Authorization: Bearer <bearer-token>
func (authenticator *BearerTokenAuthenticator) Authenticate(request *http.Request) error {
	request.Header.Set("Authorization", fmt.Sprintf(`Bearer %s`, authenticator.BearerToken))
	GetLogger().Debug("Authenticated outbound request (type=%s)\n", authenticator.AuthenticationType())
	return nil
}

// Validate the authenticator's configuration.
//
// Ensures the bearer token is not Nil.
func (authenticator BearerTokenAuthenticator) Validate() error {
	if authenticator.BearerToken == "" {
		err := fmt.Errorf(ERRORMSG_PROP_MISSING, "BearerToken")
		return SDKErrorf(err, "", "no-token", getComponentInfo())
	}
	return nil
}
