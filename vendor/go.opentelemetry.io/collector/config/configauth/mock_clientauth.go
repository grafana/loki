// Copyright The OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//       http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package configauth // import "go.opentelemetry.io/collector/config/configauth"

import (
	"context"
	"errors"
	"net/http"

	"google.golang.org/grpc/credentials"

	"go.opentelemetry.io/collector/component"
)

var (
	_            ClientAuthenticator = (*MockClientAuthenticator)(nil)
	errMockError                     = errors.New("mock Error")
)

// MockClientAuthenticator provides a mock implementation of GRPCClientAuthenticator and HTTPClientAuthenticator interfaces
type MockClientAuthenticator struct {
	ResultRoundTripper      http.RoundTripper
	ResultPerRPCCredentials credentials.PerRPCCredentials
	MustError               bool
}

// Start for the MockClientAuthenticator does nothing
func (m *MockClientAuthenticator) Start(ctx context.Context, host component.Host) error {
	return nil
}

// Shutdown for the MockClientAuthenticator does nothing
func (m *MockClientAuthenticator) Shutdown(ctx context.Context) error {
	return nil
}

// RoundTripper for the MockClientAuthenticator either returns error if the mock authenticator is forced to or
// returns the supplied resultRoundTripper.
func (m *MockClientAuthenticator) RoundTripper(base http.RoundTripper) (http.RoundTripper, error) {
	if m.MustError {
		return nil, errMockError
	}
	return m.ResultRoundTripper, nil
}

// PerRPCCredentials for the MockClientAuthenticator either returns error if the mock authenticator is forced to or
// returns the supplied resultPerRPCCredentials.
func (m *MockClientAuthenticator) PerRPCCredentials() (credentials.PerRPCCredentials, error) {
	if m.MustError {
		return nil, errMockError
	}
	return m.ResultPerRPCCredentials, nil
}
