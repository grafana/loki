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

// SDKProblem provides a type suited to problems that
// occur in SDK projects. It extends the base
// "IBMProblem" type with a field to store the
// function being called when the problem occurs.
type SDKProblem struct {
	*IBMProblem

	// Function provides the name of the in-code
	// function or method in which the problem
	// occurred.
	Function string

	// A computed stack trace including the relevant
	// function names, files, and line numbers invoked
	// leading up to the origination of the problem.
	stack []sdkStackFrame

	// If the problem instance originated in the core, we
	// want to keep track of the information for debugging
	// purposes (even though we aren't using the problem
	// as a "caused by" problem).
	coreProblem *sparseSDKProblem

	// If the problem instance originated in the core and
	// was caused by an HTTP request, we don't use the
	// HTTPProblem instance as a "caused by" but we need
	// to store the instance to pass it to a downstream
	// SDKProblem instance.
	httpProblem *HTTPProblem
}

// GetConsoleMessage returns all public fields of
// the problem, formatted in YAML.
func (e *SDKProblem) GetConsoleMessage() string {
	return ComputeConsoleMessage(e)
}

// GetDebugMessage returns all information
// about the problem, formatted in YAML.
func (e *SDKProblem) GetDebugMessage() string {
	return ComputeDebugMessage(e)
}

// GetID returns the computed identifier, computed from the
// "Component", "discriminator", and "Function" fields, as well as the
// identifier of the "causedBy" problem, if it exists.
func (e *SDKProblem) GetID() string {
	return CreateIDHash("sdk", e.GetBaseSignature(), e.Function)
}

// Is allows an SDKProblem instance to be compared against another error for equality.
// An SDKProblem is considered equal to another error if 1) the error is also a Problem and
// 2) it has the same ID (i.e. it is the same problem scenario).
func (e *SDKProblem) Is(target error) bool {
	return is(target, e.GetID())
}

// GetConsoleOrderedMaps returns an ordered-map representation
// of an SDKProblem instance suited for a console message.
func (e *SDKProblem) GetConsoleOrderedMaps() *OrderedMaps {
	orderedMaps := NewOrderedMaps()

	orderedMaps.Add("id", e.GetID())
	orderedMaps.Add("summary", e.Summary)
	orderedMaps.Add("severity", e.Severity)
	orderedMaps.Add("function", e.Function)
	orderedMaps.Add("component", e.Component)

	return orderedMaps
}

// GetDebugOrderedMaps returns an ordered-map representation
// of an SDKProblem instance, with additional information
// suited for a debug message.
func (e *SDKProblem) GetDebugOrderedMaps() *OrderedMaps {
	orderedMaps := e.GetConsoleOrderedMaps()

	orderedMaps.Add("stack", e.stack)

	if e.coreProblem != nil {
		orderedMaps.Add("core_problem", e.coreProblem)
	}

	var orderableCausedBy OrderableProblem
	if errors.As(e.GetCausedBy(), &orderableCausedBy) {
		orderedMaps.Add("caused_by", orderableCausedBy.GetDebugOrderedMaps().GetMaps())
	}

	return orderedMaps
}

// SDKErrorf creates and returns a new instance of "SDKProblem" with "error" level severity.
func SDKErrorf(err error, summary, discriminator string, component *ProblemComponent) *SDKProblem {
	function := computeFunctionName(component.Name)
	stack := getStackInfo(component.Name)

	ibmProb := IBMErrorf(err, component, summary, discriminator)
	newSDKProb := &SDKProblem{
		IBMProblem: ibmProb,
		Function:   function,
		stack:      stack,
	}

	// Flatten chains of SDKProblem instances in order to present a single,
	// unique error scenario for the SDK context. Multiple Golang components
	// can invoke each other, but we only want to track "caused by" problems
	// through context boundaries (like API, SDK, Terraform, etc.). This
	// eliminates unnecessary granularity of problem scenarios for the SDK
	// context. If the problem originated in this library (the Go SDK Core),
	// we still want to track that info for debugging purposes.
	var sdkCausedBy *SDKProblem
	if errors.As(err, &sdkCausedBy) {
		// Not a "native" caused by but allows us to maintain compatibility through "Unwrap".
		newSDKProb.nativeCausedBy = sdkCausedBy
		newSDKProb.causedBy = nil

		if isCoreProblem(sdkCausedBy) {
			newSDKProb.coreProblem = newSparseSDKProblem(sdkCausedBy)

			// If we stored an HTTPProblem instance in the core, we'll want to use
			// it as the actual "caused by" problem for the new SDK problem.
			if sdkCausedBy.httpProblem != nil {
				newSDKProb.causedBy = sdkCausedBy.httpProblem
			}
		}
	}

	// We can't use HTTPProblem instances as "caused by" problems for Go SDK Core
	// problems because 1) it prevents us from enumerating hashes in the core and
	// 2) core problems will almost never be the instances that users interact with
	// and the HTTPProblem will need to be used as the "caused by" of the problems
	// coming from actual service SDK libraries.
	var httpCausedBy *HTTPProblem
	if errors.As(err, &httpCausedBy) && isCoreProblem(newSDKProb) {
		newSDKProb.httpProblem = httpCausedBy
		newSDKProb.causedBy = nil
	}

	return newSDKProb
}

// RepurposeSDKProblem provides a convenient way to take a problem from
// another function in the same component and contextualize it to the current
// function. Should only be used in public (exported) functions.
func RepurposeSDKProblem(err error, discriminator string) error {
	if err == nil {
		return nil
	}

	// It only makes sense to carry out this logic with SDK Errors.
	var sdkErr *SDKProblem
	if !errors.As(err, &sdkErr) {
		return err
	}

	// Special behavior to allow SDK problems coming from a method that wraps a
	// "*WithContext" method to maintain the discriminator of the originating
	// problem. Otherwise, we would lose all of that data in the wrap.
	if discriminator != "" {
		sdkErr.discriminator = discriminator
	}

	// Recompute the function to reflect this public boundary (but let the stack
	// remain as it is - it is the path to the original problem origination point).
	sdkErr.Function = computeFunctionName(sdkErr.Component.Name)

	return sdkErr
}
