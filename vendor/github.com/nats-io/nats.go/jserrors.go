// Copyright 2020-2022 The NATS Authors
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

package nats

import (
	"errors"
	"fmt"
)

var (
	// API errors

	// ErrJetStreamNotEnabled is an error returned when JetStream is not enabled for an account.
	ErrJetStreamNotEnabled JetStreamError = &jsError{apiErr: &APIError{ErrorCode: JSErrCodeJetStreamNotEnabled, Description: "jetstream not enabled", Code: 503}}

	// ErrJetStreamNotEnabledForAccount is an error returned when JetStream is not enabled for an account.
	ErrJetStreamNotEnabledForAccount JetStreamError = &jsError{apiErr: &APIError{ErrorCode: JSErrCodeJetStreamNotEnabledForAccount, Description: "jetstream not enabled for account", Code: 503}}

	// ErrStreamNotFound is an error returned when stream with given name does not exist.
	ErrStreamNotFound JetStreamError = &jsError{apiErr: &APIError{ErrorCode: JSErrCodeStreamNotFound, Description: "stream not found", Code: 404}}

	// ErrStreamNameAlreadyInUse is returned when a stream with given name already exists and has a different configuration.
	ErrStreamNameAlreadyInUse JetStreamError = &jsError{apiErr: &APIError{ErrorCode: JSErrCodeStreamNameInUse, Description: "stream name already in use", Code: 400}}

	// ErrConsumerNotFound is an error returned when consumer with given name does not exist.
	ErrConsumerNotFound JetStreamError = &jsError{apiErr: &APIError{ErrorCode: JSErrCodeConsumerNotFound, Description: "consumer not found", Code: 404}}

	// ErrMsgNotFound is returned when message with provided sequence number does npt exist.
	ErrMsgNotFound JetStreamError = &jsError{apiErr: &APIError{ErrorCode: JSErrCodeMessageNotFound, Description: "message not found", Code: 404}}

	// ErrBadRequest is returned when invalid request is sent to JetStream API.
	ErrBadRequest JetStreamError = &jsError{apiErr: &APIError{ErrorCode: JSErrCodeBadRequest, Description: "bad request", Code: 400}}

	// Client errors

	// ErrConsumerNameAlreadyInUse is an error returned when consumer with given name already exists.
	ErrConsumerNameAlreadyInUse JetStreamError = &jsError{message: "consumer name already in use"}

	// ErrConsumerNotActive is an error returned when consumer is not active.
	ErrConsumerNotActive JetStreamError = &jsError{message: "consumer not active"}

	// ErrInvalidJSAck is returned when JetStream ack from message publish is invalid.
	ErrInvalidJSAck JetStreamError = &jsError{message: "invalid jetstream publish response"}

	// ErrStreamConfigRequired is returned when empty stream configuration is supplied to add/update stream.
	ErrStreamConfigRequired JetStreamError = &jsError{message: "stream configuration is required"}

	// ErrStreamNameRequired is returned when the provided stream name is empty.
	ErrStreamNameRequired JetStreamError = &jsError{message: "stream name is required"}

	// ErrConsumerNameRequired is returned when the provided consumer durable name is empty.
	ErrConsumerNameRequired JetStreamError = &jsError{message: "consumer name is required"}

	// ErrConsumerConfigRequired is returned when empty consumer consuguration is supplied to add/update consumer.
	ErrConsumerConfigRequired JetStreamError = &jsError{message: "consumer configuration is required"}

	// ErrPullSubscribeToPushConsumer is returned when attempting to use PullSubscribe on push consumer.
	ErrPullSubscribeToPushConsumer JetStreamError = &jsError{message: "cannot pull subscribe to push based consumer"}

	// ErrPullSubscribeRequired is returned when attempting to use subscribe methods not suitable for pull consumers for pull consumers.
	ErrPullSubscribeRequired JetStreamError = &jsError{message: "must use pull subscribe to bind to pull based consumer"}

	// ErrMsgAlreadyAckd is returned when attempting to acknowledge message more than once.
	ErrMsgAlreadyAckd JetStreamError = &jsError{message: "message was already acknowledged"}

	// ErrNoStreamResponse is returned when there is no response from stream (e.g. no responders error).
	ErrNoStreamResponse JetStreamError = &jsError{message: "no response from stream"}

	// ErrNotJSMessage is returned when attempting to get metadata from non JetStream message .
	ErrNotJSMessage JetStreamError = &jsError{message: "not a jetstream message"}

	// ErrInvalidStreamName is returned when the provided stream name is invalid (contains '.' or ' ').
	ErrInvalidStreamName JetStreamError = &jsError{message: "invalid stream name"}

	// ErrInvalidConsumerName is returned when the provided consumer name is invalid (contains '.' or ' ').
	ErrInvalidConsumerName JetStreamError = &jsError{message: "invalid consumer name"}

	// ErrNoMatchingStream is returned when stream lookup by subject is unsuccessful.
	ErrNoMatchingStream JetStreamError = &jsError{message: "no stream matches subject"}

	// ErrSubjectMismatch is returned when the provided subject does not match consumer's filter subject.
	ErrSubjectMismatch JetStreamError = &jsError{message: "subject does not match consumer"}

	// ErrContextAndTimeout is returned when attempting to use both context and timeout.
	ErrContextAndTimeout JetStreamError = &jsError{message: "context and timeout can not both be set"}

	// ErrCantAckIfConsumerAckNone is returned when attempting to ack a message for consumer with AckNone policy set.
	ErrCantAckIfConsumerAckNone JetStreamError = &jsError{message: "cannot acknowledge a message for a consumer with AckNone policy"}

	// ErrConsumerDeleted is returned when attempting to send pull request to a consumer which does not exist
	ErrConsumerDeleted JetStreamError = &jsError{message: "consumer deleted"}

	// ErrConsumerLeadershipChanged is returned when pending requests are no longer valid after leadership has changed
	ErrConsumerLeadershipChanged JetStreamError = &jsError{message: "Leadership Changed"}

	// DEPRECATED: ErrInvalidDurableName is no longer returned and will be removed in future releases.
	// Use ErrInvalidConsumerName instead.
	ErrInvalidDurableName = errors.New("nats: invalid durable name")
)

// Error code represents JetStream error codes returned by the API
type ErrorCode uint16

const (
	JSErrCodeJetStreamNotEnabledForAccount ErrorCode = 10039
	JSErrCodeJetStreamNotEnabled           ErrorCode = 10076
	JSErrCodeInsufficientResourcesErr      ErrorCode = 10023

	JSErrCodeStreamNotFound  ErrorCode = 10059
	JSErrCodeStreamNameInUse ErrorCode = 10058

	JSErrCodeConsumerNotFound      ErrorCode = 10014
	JSErrCodeConsumerNameExists    ErrorCode = 10013
	JSErrCodeConsumerAlreadyExists ErrorCode = 10105

	JSErrCodeMessageNotFound ErrorCode = 10037

	JSErrCodeBadRequest ErrorCode = 10003

	JSErrCodeStreamWrongLastSequence ErrorCode = 10071
)

// APIError is included in all API responses if there was an error.
type APIError struct {
	Code        int       `json:"code"`
	ErrorCode   ErrorCode `json:"err_code"`
	Description string    `json:"description,omitempty"`
}

// Error prints the JetStream API error code and description
func (e *APIError) Error() string {
	return fmt.Sprintf("nats: %s", e.Description)
}

// APIError implements the JetStreamError interface.
func (e *APIError) APIError() *APIError {
	return e
}

// Is matches against an APIError.
func (e *APIError) Is(err error) bool {
	if e == nil {
		return false
	}
	// Extract internal APIError to match against.
	var aerr *APIError
	ok := errors.As(err, &aerr)
	if !ok {
		return ok
	}
	return e.ErrorCode == aerr.ErrorCode
}

// JetStreamError is an error result that happens when using JetStream.
// In case of client-side error, `APIError()` returns nil
type JetStreamError interface {
	APIError() *APIError
	error
}

type jsError struct {
	apiErr  *APIError
	message string
}

func (err *jsError) APIError() *APIError {
	return err.apiErr
}

func (err *jsError) Error() string {
	if err.apiErr != nil && err.apiErr.Description != "" {
		return err.apiErr.Error()
	}
	return fmt.Sprintf("nats: %s", err.message)
}

func (err *jsError) Unwrap() error {
	// Allow matching to embedded APIError in case there is one.
	if err.apiErr == nil {
		return nil
	}
	return err.apiErr
}
