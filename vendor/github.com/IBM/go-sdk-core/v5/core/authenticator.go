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

// Authenticator describes the set of methods implemented by each authenticator.
type Authenticator interface {
	AuthenticationType() string
	Authenticate(*http.Request) error
	Validate() error
}

// AuthenticationError describes the error returned when authentication fails
type AuthenticationError struct {
	Response *DetailedResponse
	Err      error
}

func (e *AuthenticationError) Error() string {
	return e.Err.Error()
}

func NewAuthenticationError(response *DetailedResponse, err error) *AuthenticationError {
	return &AuthenticationError{
		Response: response,
		Err:      err,
	}
}
