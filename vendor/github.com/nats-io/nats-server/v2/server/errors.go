// Copyright 2012-2021 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package server

import (
	"errors"
	"fmt"
)

var (
	// ErrConnectionClosed represents an error condition on a closed connection.
	ErrConnectionClosed = errors.New("connection closed")

	// ErrAuthentication represents an error condition on failed authentication.
	ErrAuthentication = errors.New("authentication error")

	// ErrAuthTimeout represents an error condition on failed authorization due to timeout.
	ErrAuthTimeout = errors.New("authentication timeout")

	// ErrAuthExpired represents an expired authorization due to timeout.
	ErrAuthExpired = errors.New("authentication expired")

	// ErrMaxPayload represents an error condition when the payload is too big.
	ErrMaxPayload = errors.New("maximum payload exceeded")

	// ErrMaxControlLine represents an error condition when the control line is too big.
	ErrMaxControlLine = errors.New("maximum control line exceeded")

	// ErrReservedPublishSubject represents an error condition when sending to a reserved subject, e.g. _SYS.>
	ErrReservedPublishSubject = errors.New("reserved internal subject")

	// ErrBadPublishSubject represents an error condition for an invalid publish subject.
	ErrBadPublishSubject = errors.New("invalid publish subject")

	// ErrBadSubject represents an error condition for an invalid subject.
	ErrBadSubject = errors.New("invalid subject")

	// ErrBadQualifier is used to error on a bad qualifier for a transform.
	ErrBadQualifier = errors.New("bad qualifier")

	// ErrBadClientProtocol signals a client requested an invalid client protocol.
	ErrBadClientProtocol = errors.New("invalid client protocol")

	// ErrTooManyConnections signals a client that the maximum number of connections supported by the
	// server has been reached.
	ErrTooManyConnections = errors.New("maximum connections exceeded")

	// ErrTooManyAccountConnections signals that an account has reached its maximum number of active
	// connections.
	ErrTooManyAccountConnections = errors.New("maximum account active connections exceeded")

	// ErrTooManySubs signals a client that the maximum number of subscriptions per connection
	// has been reached.
	ErrTooManySubs = errors.New("maximum subscriptions exceeded")

	// ErrTooManySubTokens signals a client that the subject has too many tokens.
	ErrTooManySubTokens = errors.New("subject has exceeded number of tokens limit")

	// ErrClientConnectedToRoutePort represents an error condition when a client
	// attempted to connect to the route listen port.
	ErrClientConnectedToRoutePort = errors.New("attempted to connect to route port")

	// ErrClientConnectedToLeafNodePort represents an error condition when a client
	// attempted to connect to the leaf node listen port.
	ErrClientConnectedToLeafNodePort = errors.New("attempted to connect to leaf node port")

	// ErrLeafNodeHasSameClusterName represents an error condition when a leafnode is a cluster
	// and it has the same cluster name as the hub cluster.
	ErrLeafNodeHasSameClusterName = errors.New("remote leafnode has same cluster name")

	// ErrLeafNodeDisabled is when we disable leafnodes.
	ErrLeafNodeDisabled = errors.New("leafnodes disabled")

	// ErrConnectedToWrongPort represents an error condition when a connection is attempted
	// to the wrong listen port (for instance a LeafNode to a client port, etc...)
	ErrConnectedToWrongPort = errors.New("attempted to connect to wrong port")

	// ErrAccountExists is returned when an account is attempted to be registered
	// but already exists.
	ErrAccountExists = errors.New("account exists")

	// ErrBadAccount represents a malformed or incorrect account.
	ErrBadAccount = errors.New("bad account")

	// ErrReservedAccount represents a reserved account that can not be created.
	ErrReservedAccount = errors.New("reserved account")

	// ErrMissingAccount is returned when an account does not exist.
	ErrMissingAccount = errors.New("account missing")

	// ErrMissingService is returned when an account does not have an exported service.
	ErrMissingService = errors.New("service missing")

	// ErrBadServiceType is returned when latency tracking is being applied to non-singleton response types.
	ErrBadServiceType = errors.New("bad service response type")

	// ErrBadSampling is returned when the sampling for latency tracking is not 1 >= sample <= 100.
	ErrBadSampling = errors.New("bad sampling percentage, should be 1-100")

	// ErrAccountValidation is returned when an account has failed validation.
	ErrAccountValidation = errors.New("account validation failed")

	// ErrAccountExpired is returned when an account has expired.
	ErrAccountExpired = errors.New("account expired")

	// ErrNoAccountResolver is returned when we attempt an update but do not have an account resolver.
	ErrNoAccountResolver = errors.New("account resolver missing")

	// ErrAccountResolverUpdateTooSoon is returned when we attempt an update too soon to last request.
	ErrAccountResolverUpdateTooSoon = errors.New("account resolver update too soon")

	// ErrAccountResolverSameClaims is returned when same claims have been fetched.
	ErrAccountResolverSameClaims = errors.New("account resolver no new claims")

	// ErrStreamImportAuthorization is returned when a stream import is not authorized.
	ErrStreamImportAuthorization = errors.New("stream import not authorized")

	// ErrStreamImportBadPrefix is returned when a stream import prefix contains wildcards.
	ErrStreamImportBadPrefix = errors.New("stream import prefix can not contain wildcard tokens")

	// ErrStreamImportDuplicate is returned when a stream import is a duplicate of one that already exists.
	ErrStreamImportDuplicate = errors.New("stream import already exists")

	// ErrServiceImportAuthorization is returned when a service import is not authorized.
	ErrServiceImportAuthorization = errors.New("service import not authorized")

	// ErrImportFormsCycle is returned when an import would form a cycle.
	ErrImportFormsCycle = errors.New("import forms a cycle")

	// ErrCycleSearchDepth is returned when we have exceeded our maximum search depth..
	ErrCycleSearchDepth = errors.New("search cycle depth exhausted")

	// ErrClientOrRouteConnectedToGatewayPort represents an error condition when
	// a client or route attempted to connect to the Gateway port.
	ErrClientOrRouteConnectedToGatewayPort = errors.New("attempted to connect to gateway port")

	// ErrWrongGateway represents an error condition when a server receives a connect
	// request from a remote Gateway with a destination name that does not match the server's
	// Gateway's name.
	ErrWrongGateway = errors.New("wrong gateway")

	// ErrNoSysAccount is returned when an attempt to publish or subscribe is made
	// when there is no internal system account defined.
	ErrNoSysAccount = errors.New("system account not setup")

	// ErrRevocation is returned when a credential has been revoked.
	ErrRevocation = errors.New("credentials have been revoked")

	// ErrServerNotRunning is used to signal an error that a server is not running.
	ErrServerNotRunning = errors.New("server is not running")

	// ErrBadMsgHeader signals the parser detected a bad message header
	ErrBadMsgHeader = errors.New("bad message header detected")

	// ErrMsgHeadersNotSupported signals the parser detected a message header
	// but they are not supported on this server.
	ErrMsgHeadersNotSupported = errors.New("message headers not supported")

	// ErrNoRespondersRequiresHeaders signals that a client needs to have headers
	// on if they want no responders behavior.
	ErrNoRespondersRequiresHeaders = errors.New("no responders requires headers support")

	// ErrClusterNameConfigConflict signals that the options for cluster name in cluster and gateway are in conflict.
	ErrClusterNameConfigConflict = errors.New("cluster name conflicts between cluster and gateway definitions")

	// ErrClusterNameRemoteConflict signals that a remote server has a different cluster name.
	ErrClusterNameRemoteConflict = errors.New("cluster name from remote server conflicts")

	// ErrMalformedSubject is returned when a subscription is made with a subject that does not conform to subject rules.
	ErrMalformedSubject = errors.New("malformed subject")

	// ErrSubscribePermissionViolation is returned when processing of a subscription fails due to permissions.
	ErrSubscribePermissionViolation = errors.New("subscribe permission violation")

	// ErrNoTransforms signals no subject transforms are available to map this subject.
	ErrNoTransforms = errors.New("no matching transforms available")

	// ErrCertNotPinned is returned when pinned certs are set and the certificate is not in it
	ErrCertNotPinned = errors.New("certificate not pinned")

	// ErrDuplicateServerName is returned when processing a server remote connection and
	// the server reports that this server name is already used in the cluster.
	ErrDuplicateServerName = errors.New("duplicate server name")

	// ErrMinimumVersionRequired is returned when a connection is not at the minimum version required.
	ErrMinimumVersionRequired = errors.New("minimum version required")

	// ErrInvalidMappingDestination is used for all subject mapping destination errors
	ErrInvalidMappingDestination = errors.New("invalid mapping destination")

	// ErrInvalidMappingDestinationSubject is used to error on a bad transform destination mapping
	ErrInvalidMappingDestinationSubject = fmt.Errorf("%w: invalid subject", ErrInvalidMappingDestination)

	// ErrMappingDestinationNotUsingAllWildcards is used to error on a transform destination not using all of the token wildcards
	ErrMappingDestinationNotUsingAllWildcards = fmt.Errorf("%w: not using all of the token wildcard(s)", ErrInvalidMappingDestination)

	// ErrUnknownMappingDestinationFunction is returned when a subject mapping destination contains an unknown mustache-escaped mapping function.
	ErrUnknownMappingDestinationFunction = fmt.Errorf("%w: unknown function", ErrInvalidMappingDestination)

	// ErrorMappingDestinationFunctionWildcardIndexOutOfRange is returned when the mapping destination function is passed an out of range wildcard index value for one of it's arguments
	ErrorMappingDestinationFunctionWildcardIndexOutOfRange = fmt.Errorf("%w: wildcard index out of range", ErrInvalidMappingDestination)

	// ErrorMappingDestinationFunctionNotEnoughArguments is returned when the mapping destination function is not passed enough arguments
	ErrorMappingDestinationFunctionNotEnoughArguments = fmt.Errorf("%w: not enough arguments passed to the function", ErrInvalidMappingDestination)

	// ErrorMappingDestinationFunctionInvalidArgument is returned when the mapping destination function is passed and invalid argument
	ErrorMappingDestinationFunctionInvalidArgument = fmt.Errorf("%w: function argument is invalid or in the wrong format", ErrInvalidMappingDestination)

	// ErrorMappingDestinationFunctionTooManyArguments is returned when the mapping destination function is passed too many arguments
	ErrorMappingDestinationFunctionTooManyArguments = fmt.Errorf("%w: too many arguments passed to the function", ErrInvalidMappingDestination)
)

// mappingDestinationErr is a type of subject mapping destination error
type mappingDestinationErr struct {
	token string
	err   error
}

func (e *mappingDestinationErr) Error() string {
	return fmt.Sprintf("%s in %s", e.err, e.token)
}

func (e *mappingDestinationErr) Is(target error) bool {
	return target == ErrInvalidMappingDestination
}

// configErr is a configuration error.
type configErr struct {
	token  token
	reason string
}

// Source reports the location of a configuration error.
func (e *configErr) Source() string {
	return fmt.Sprintf("%s:%d:%d", e.token.SourceFile(), e.token.Line(), e.token.Position())
}

// Error reports the location and reason from a configuration error.
func (e *configErr) Error() string {
	if e.token != nil {
		return fmt.Sprintf("%s: %s", e.Source(), e.reason)
	}
	return e.reason
}

// unknownConfigFieldErr is an error reported in pedantic mode.
type unknownConfigFieldErr struct {
	configErr
	field string
}

// Error reports that an unknown field was in the configuration.
func (e *unknownConfigFieldErr) Error() string {
	return fmt.Sprintf("%s: unknown field %q", e.Source(), e.field)
}

// configWarningErr is an error reported in pedantic mode.
type configWarningErr struct {
	configErr
	field string
}

// Error reports a configuration warning.
func (e *configWarningErr) Error() string {
	return fmt.Sprintf("%s: invalid use of field %q: %s", e.Source(), e.field, e.reason)
}

// processConfigErr is the result of processing the configuration from the server.
type processConfigErr struct {
	errors   []error
	warnings []error
}

// Error returns the collection of errors separated by new lines,
// warnings appear first then hard errors.
func (e *processConfigErr) Error() string {
	var msg string
	for _, err := range e.Warnings() {
		msg += err.Error() + "\n"
	}
	for _, err := range e.Errors() {
		msg += err.Error() + "\n"
	}
	return msg
}

// Warnings returns the list of warnings.
func (e *processConfigErr) Warnings() []error {
	return e.warnings
}

// Errors returns the list of errors.
func (e *processConfigErr) Errors() []error {
	return e.errors
}

// errCtx wraps an error and stores additional ctx information for tracing.
// Does not print or return it unless explicitly requested.
type errCtx struct {
	error
	ctx string
}

func NewErrorCtx(err error, format string, args ...interface{}) error {
	return &errCtx{err, fmt.Sprintf(format, args...)}
}

// Unwrap implement to work with errors.Is and errors.As
func (e *errCtx) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.error
}

// Context for error
func (e *errCtx) Context() string {
	if e == nil {
		return ""
	}
	return e.ctx
}

// UnpackIfErrorCtx return Error or, if type is right error and context
func UnpackIfErrorCtx(err error) string {
	if e, ok := err.(*errCtx); ok {
		if _, ok := e.error.(*errCtx); ok {
			return fmt.Sprint(UnpackIfErrorCtx(e.error), ": ", e.Context())
		}
		return fmt.Sprint(e.Error(), ": ", e.Context())
	}
	return err.Error()
}

// implements: go 1.13 errors.Unwrap(err error) error
// TODO replace with native code once we no longer support go1.12
func errorsUnwrap(err error) error {
	u, ok := err.(interface {
		Unwrap() error
	})
	if !ok {
		return nil
	}
	return u.Unwrap()
}

// ErrorIs implements: go 1.13 errors.Is(err, target error) bool
// TODO replace with native code once we no longer support go1.12
func ErrorIs(err, target error) bool {
	// this is an outright copy of go 1.13 errors.Is(err, target error) bool
	// removed isComparable
	if err == nil || target == nil {
		return err == target
	}

	for {
		if err == target {
			return true
		}
		if x, ok := err.(interface{ Is(error) bool }); ok && x.Is(target) {
			return true
		}
		// TODO: consider supporing target.Is(err). This would allow
		// user-definable predicates, but also may allow for coping with sloppy
		// APIs, thereby making it easier to get away with them.
		if err = errorsUnwrap(err); err == nil {
			return false
		}
	}
}
