package core

// (C) Copyright IBM Corp. 2024.
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
)

// AuthenticationError describes the problem returned when
// authentication over HTTP fails.
type AuthenticationError struct {
	Err error
	*HTTPProblem
}

// NewAuthenticationError is a deprecated function that was previously used for creating new
// AuthenticationError structs. HTTPProblem types should be used instead of AuthenticationError types.
func NewAuthenticationError(response *DetailedResponse, err error) *AuthenticationError {
	GetLogger().Warn("NewAuthenticationError is deprecated and should not be used.")
	authError := authenticationErrorf(err, response, "unknown", NewProblemComponent("unknown", "unknown"))
	return authError
}

// authenticationErrorf creates and returns a new instance of "AuthenticationError".
func authenticationErrorf(err error, response *DetailedResponse, operationID string, component *ProblemComponent) *AuthenticationError {
	// This function should always be called with non-nil error instances.
	if err == nil {
		return nil
	}

	var httpErr *HTTPProblem
	if !errors.As(err, &httpErr) {
		if response == nil {
			return nil
		}
		httpErr = httpErrorf(err.Error(), response)
	}

	enrichHTTPProblem(httpErr, operationID, component)

	return &AuthenticationError{
		HTTPProblem: httpErr,
		Err:         err,
	}
}
