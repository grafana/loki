// Copyright  The OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package configauth // import "go.opentelemetry.io/collector/config/configauth"

import (
	"context"
	"net/http"

	"google.golang.org/grpc/credentials"

	"go.opentelemetry.io/collector/component"
)

var _ ClientAuthenticator = (*defaultClientAuthenticator)(nil)

// Option represents the possible options for NewServerAuthenticator.
type ClientOption func(*defaultClientAuthenticator)

type defaultClientAuthenticator struct {
	component.StartFunc
	component.ShutdownFunc
	roundTripperFunc      func(base http.RoundTripper) (http.RoundTripper, error)
	perRPCCredentialsFunc func() (credentials.PerRPCCredentials, error)
}

// WithClientStart overrides the default `Start` function for a component.Component.
// The default always returns nil.
func WithClientStart(startFunc component.StartFunc) ClientOption {
	return func(o *defaultClientAuthenticator) {
		o.StartFunc = startFunc
	}
}

// WithClientShutdown overrides the default `Shutdown` function for a component.Component.
// The default always returns nil.
func WithClientShutdown(shutdownFunc component.ShutdownFunc) ClientOption {
	return func(o *defaultClientAuthenticator) {
		o.ShutdownFunc = shutdownFunc
	}
}

// WithClientRoundTripper provides a `RoundTripper` function for this client authenticator.
// The default round tripper is no-op.
func WithClientRoundTripper(roundTripperFunc func(base http.RoundTripper) (http.RoundTripper, error)) ClientOption {
	return func(o *defaultClientAuthenticator) {
		o.roundTripperFunc = roundTripperFunc
	}
}

// WithPerRPCCredentials provides a `PerRPCCredentials` function for this client authenticator.
// There's no default.
func WithPerRPCCredentials(perRPCCredentialsFunc func() (credentials.PerRPCCredentials, error)) ClientOption {
	return func(o *defaultClientAuthenticator) {
		o.perRPCCredentialsFunc = perRPCCredentialsFunc
	}
}

// NewClientAuthenticator returns a ClientAuthenticator configured with the provided options.
func NewClientAuthenticator(options ...ClientOption) ClientAuthenticator {
	bc := &defaultClientAuthenticator{
		StartFunc:             func(ctx context.Context, host component.Host) error { return nil },
		ShutdownFunc:          func(ctx context.Context) error { return nil },
		roundTripperFunc:      func(base http.RoundTripper) (http.RoundTripper, error) { return base, nil },
		perRPCCredentialsFunc: func() (credentials.PerRPCCredentials, error) { return nil, nil },
	}

	for _, op := range options {
		op(bc)
	}

	return bc
}

// Start the component.
func (a *defaultClientAuthenticator) Start(ctx context.Context, host component.Host) error {
	return a.StartFunc(ctx, host)
}

// Shutdown stops the component.
func (a *defaultClientAuthenticator) Shutdown(ctx context.Context) error {
	return a.ShutdownFunc(ctx)
}

// RoundTripper adds the base HTTP RoundTripper in this authenticator's round tripper.
func (a *defaultClientAuthenticator) RoundTripper(base http.RoundTripper) (http.RoundTripper, error) {
	return a.roundTripperFunc(base)
}

// PerRPCCredentials returns this authenticator's credentials.PerRPCCredentials implementation.
func (a *defaultClientAuthenticator) PerRPCCredentials() (credentials.PerRPCCredentials, error) {
	return a.perRPCCredentialsFunc()
}
