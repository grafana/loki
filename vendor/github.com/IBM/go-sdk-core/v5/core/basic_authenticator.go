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

// BasicAuthenticator takes a user-supplied username and password, and adds
// them to requests via an Authorization header of the form:
//
//	Authorization: Basic <encoded username and password>
type BasicAuthenticator struct {
	// Username is the user-supplied basic auth username [required].
	Username string
	// Password is the user-supplied basic auth password [required].
	Password string
}

// NewBasicAuthenticator constructs a new BasicAuthenticator instance.
func NewBasicAuthenticator(username string, password string) (*BasicAuthenticator, error) {
	obj := &BasicAuthenticator{
		Username: username,
		Password: password,
	}
	if err := obj.Validate(); err != nil {
		err = RepurposeSDKProblem(err, "validation-failed")
		return nil, err
	}
	return obj, nil
}

// newBasicAuthenticatorFromMap constructs a new BasicAuthenticator instance
// from a map.
func newBasicAuthenticatorFromMap(properties map[string]string) (*BasicAuthenticator, error) {
	if properties == nil {
		err := errors.New(ERRORMSG_PROPS_MAP_NIL)
		return nil, SDKErrorf(err, "", "missing-props", getComponentInfo())
	}

	return NewBasicAuthenticator(properties[PROPNAME_USERNAME], properties[PROPNAME_PASSWORD])
}

// AuthenticationType returns the authentication type for this authenticator.
func (BasicAuthenticator) AuthenticationType() string {
	return AUTHTYPE_BASIC
}

// Authenticate adds basic authentication information to a request.
//
// Basic Authorization will be added to the request's headers in the form:
//
//	Authorization: Basic <encoded username and password>
func (authenticator *BasicAuthenticator) Authenticate(request *http.Request) error {
	request.SetBasicAuth(authenticator.Username, authenticator.Password)
	GetLogger().Debug("Authenticated outbound request (type=%s)\n", authenticator.AuthenticationType())
	return nil
}

// Validate the authenticator's configuration.
//
// Ensures the username and password are not Nil. Additionally, ensures
// they do not contain invalid characters.
func (authenticator BasicAuthenticator) Validate() error {
	if authenticator.Username == "" {
		err := fmt.Errorf(ERRORMSG_PROP_MISSING, "Username")
		return SDKErrorf(err, "", "no-user", getComponentInfo())
	}

	if authenticator.Password == "" {
		err := fmt.Errorf(ERRORMSG_PROP_MISSING, "Password")
		return SDKErrorf(err, "", "no-pass", getComponentInfo())
	}

	if HasBadFirstOrLastChar(authenticator.Username) {
		err := fmt.Errorf(ERRORMSG_PROP_INVALID, "Username")
		return SDKErrorf(err, "", "bad-user", getComponentInfo())
	}

	if HasBadFirstOrLastChar(authenticator.Password) {
		err := fmt.Errorf(ERRORMSG_PROP_INVALID, "Password")
		return SDKErrorf(err, "", "bad-pass", getComponentInfo())
	}

	return nil
}
