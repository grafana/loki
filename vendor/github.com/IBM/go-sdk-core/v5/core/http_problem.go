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
	"fmt"
)

// HTTPProblem provides a type suited to problems that
// occur as the result of an HTTP request. It extends
// the base "IBMProblem" type with fields to store
// information about the HTTP request/response.
type HTTPProblem struct {
	*IBMProblem

	// OperationID identifies the operation of an API
	// that the failed request was made to.
	OperationID string

	// Response contains the full HTTP error response
	// returned as a result of the failed request,
	// including the body and all headers.
	Response *DetailedResponse
}

// GetConsoleMessage returns all public fields of
// the problem, formatted in YAML.
func (e *HTTPProblem) GetConsoleMessage() string {
	return ComputeConsoleMessage(e)
}

// GetDebugMessage returns all information about
// the problem, formatted in YAML.
func (e *HTTPProblem) GetDebugMessage() string {
	return ComputeDebugMessage(e)
}

// GetID returns the computed identifier, computed from the
// "Component", "discriminator", and "OperationID" fields,
// as well as the status code of the stored response and the
// identifier of the "causedBy" problem, if it exists.
func (e *HTTPProblem) GetID() string {
	// TODO: add the error code to the hash once we have the ability to enumerate error codes in an API.
	return CreateIDHash("http", e.GetBaseSignature(), e.OperationID, fmt.Sprint(e.Response.GetStatusCode()))
}

// Is allows an HTTPProblem instance to be compared against another error for equality.
// An HTTPProblem is considered equal to another error if 1) the error is also a Problem and
// 2) it has the same ID (i.e. it is the same problem scenario).
func (e *HTTPProblem) Is(target error) bool {
	return is(target, e.GetID())
}

func (e *HTTPProblem) getErrorCode() string {
	// If the error response was a standard JSON body, the result will
	// be a map and we can do a decent job of guessing the code.
	if e.Response.Result != nil {
		if resultMap, ok := e.Response.Result.(map[string]interface{}); ok {
			return getErrorCode(resultMap)
		}
	}

	return ""
}

func (e *HTTPProblem) getHeader(key string) (string, bool) {
	value := e.Response.Headers.Get(key)
	return value, value != ""
}

// GetConsoleOrderedMaps returns an ordered-map representation
// of an HTTPProblem instance suited for a console message.
func (e *HTTPProblem) GetConsoleOrderedMaps() *OrderedMaps {
	orderedMaps := NewOrderedMaps()

	orderedMaps.Add("id", e.GetID())
	orderedMaps.Add("summary", e.Summary)
	orderedMaps.Add("severity", e.Severity)
	orderedMaps.Add("operation_id", e.OperationID)
	orderedMaps.Add("status_code", e.Response.GetStatusCode())
	errorCode := e.getErrorCode()
	if errorCode != "" {
		orderedMaps.Add("error_code", errorCode)
	}
	orderedMaps.Add("component", e.Component)

	// Conditionally add the request ID and correlation ID header values.

	if header, ok := e.getHeader("x-request-id"); ok {
		orderedMaps.Add("request_id", header)
	}

	if header, ok := e.getHeader("x-correlation-id"); ok {
		orderedMaps.Add("correlation_id", header)
	}

	return orderedMaps
}

// GetDebugOrderedMaps returns an ordered-map representation
// of an HTTPProblem instance, with additional information
// suited for a debug message.
func (e *HTTPProblem) GetDebugOrderedMaps() *OrderedMaps {
	orderedMaps := e.GetConsoleOrderedMaps()

	// The RawResult is never helpful in the printed message. Create a hard copy
	// (de-referenced pointer) to remove the raw result from so we don't alter
	// the response stored in the problem object.
	printableResponse := *e.Response
	if printableResponse.Result == nil {
		printableResponse.Result = string(printableResponse.RawResult)
	}
	printableResponse.RawResult = nil
	orderedMaps.Add("response", printableResponse)

	var orderableCausedBy OrderableProblem
	if errors.As(e.GetCausedBy(), &orderableCausedBy) {
		orderedMaps.Add("caused_by", orderableCausedBy.GetDebugOrderedMaps().GetMaps())
	}

	return orderedMaps
}

// httpErrorf creates and returns a new instance of "HTTPProblem" with "error" level severity.
func httpErrorf(summary string, response *DetailedResponse) *HTTPProblem {
	httpProb := &HTTPProblem{
		IBMProblem: IBMErrorf(nil, NewProblemComponent("", ""), summary, ""),
		Response:   response,
	}

	return httpProb
}

// EnrichHTTPProblem takes an problem and, if it originated as an HTTPProblem, populates
// the fields of the underlying HTTP problem with the given service/operation information.
func EnrichHTTPProblem(err error, operationID string, component *ProblemComponent) {
	// If the problem originated from an HTTP error response, populate the
	// HTTPProblem instance with details from the SDK that weren't available
	// in the core at problem creation time.
	var httpProb *HTTPProblem

	// In the case of an SDKProblem instance originating in the core,
	// it will not track an HTTPProblem instance in its "caused by"
	// chain, but we still want to be able to enrich it. It will be
	// stored in the private "httpProblem" field.
	var sdkProb *SDKProblem

	if errors.As(err, &httpProb) {
		enrichHTTPProblem(httpProb, operationID, component)
	} else if errors.As(err, &sdkProb) && sdkProb.httpProblem != nil {
		enrichHTTPProblem(sdkProb.httpProblem, operationID, component)
	}
}

// enrichHTTPProblem takes an HTTPProblem instance alongside information about the request
// and adds the extra info to the instance. It also loosely deserializes the response
// in order to set additional information, like the error code.
func enrichHTTPProblem(httpProb *HTTPProblem, operationID string, component *ProblemComponent) {
	// If this problem is already populated with service-level information,
	// we should not enrich it any further. Most likely, this is an authentication
	// error passed from the core to the SDK.
	if httpProb.Component.Name != "" {
		return
	}

	httpProb.Component = component
	httpProb.OperationID = operationID
}
