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
	"net/http"
)

// NoAuthAuthenticator is simply a placeholder implementation of the Authenticator interface
// that performs no authentication. This might be useful in testing/debugging situations.
type NoAuthAuthenticator struct {
}

func NewNoAuthAuthenticator() (*NoAuthAuthenticator, error) {
	return &NoAuthAuthenticator{}, nil
}

func (NoAuthAuthenticator) AuthenticationType() string {
	return AUTHTYPE_NOAUTH
}

func (NoAuthAuthenticator) Validate() error {
	return nil
}

func (this *NoAuthAuthenticator) Authenticate(request *http.Request) error {
	// Nothing to do since we're not providing any authentication.
	return nil
}
