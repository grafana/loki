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

// problemSeverity simulates an enum by defining the only string values that are
// supported for the Severity field of an IBMProblem: "error" and "warning".
type problemSeverity string

const (
	ErrorSeverity   problemSeverity = "error"
	WarningSeverity problemSeverity = "warning"
)

// ProblemComponent is a structure that holds information about a given component.
type ProblemComponent struct {
	Name    string
	Version string
}

func NewProblemComponent(name, version string) *ProblemComponent {
	return &ProblemComponent{
		Name:    name,
		Version: version,
	}
}

// IBMProblem holds the base set of fields that all problem types
// should include. It is geared more towards embedding in other
// structs than towards use on its own.
type IBMProblem struct {

	// Summary is the informative, user-friendly message that describes
	// the problem and what caused it.
	Summary string

	// Component is a structure providing information about the actual
	// component that the problem occurred in: the name of the component
	// and the version of the component being used with the problem occurred.
	// Examples of components include cloud services, SDK clients, the IBM
	// Terraform Provider, etc. For programming libraries, the Component name
	// should match the module name for the library (i.e. the name a user
	// would use to install it).
	Component *ProblemComponent

	// Severity represents the severity level of the problem,
	// e.g. error, warning, or info.
	Severity problemSeverity

	// discriminator is a private property that is not ever meant to be
	// seen by the end user. It's sole purpose is to enforce uniqueness
	// for the computed ID of problems that would otherwise have the same
	// ID. For example, if two SDKProblem instances are created with the
	// same Component and Function values, they would end up with the same
	// ID. This property allows us to "discriminate" between such problems.
	discriminator string

	// causedBy allows for the storage of a problem from a previous component,
	// if there is one.
	causedBy Problem

	// nativeCausedBy allows for the storage of an error that is the cause of
	// the problem instance but is not a part of the official chain of problem
	// types. By including these errors in the "Unwrap" chain, the problem type
	// changes become compatible with downstream code that uses error checking
	// methods like "Is" and "As".
	nativeCausedBy error
}

// Error returns the problem's message and implements the native
// "error" interface.
func (e *IBMProblem) Error() string {
	return e.Summary
}

// GetBaseSignature provides a convenient way of
// retrieving the fields needed to compute the
// hash that are common to every kind of problem.
func (e *IBMProblem) GetBaseSignature() string {
	causedByID := ""
	if e.causedBy != nil {
		causedByID = e.causedBy.GetID()
	}
	return fmt.Sprintf("%s%s%s%s", e.Component.Name, e.Severity, e.discriminator, causedByID)
}

// GetCausedBy returns the underlying "causedBy" problem, if it exists.
func (e *IBMProblem) GetCausedBy() Problem {
	return e.causedBy
}

// Unwrap implements an interface the native Go "errors" package uses to
// check for embedded problems in a given problem instance. IBM problem types
// are not embedded in the traditional sense, but they chain previous
// problem instances together with the "causedBy" field. This allows error
// interface instances to be cast into any of the problem types in the chain
// using the native "errors.As" function. This can be useful for, as an
// example, extracting an HTTPProblem from the chain if it exists.
// Note that this Unwrap method returns only the chain of "caused by" problems;
// it does not include the error instance the method is called on - that is
// looked at separately by the "errors" package in functions like "As".
func (e *IBMProblem) Unwrap() []error {
	var errs []error

	// Include native (i.e. non-Problem) caused by errors in the
	// chain for compatibility with respect to downstream methods
	// like "errors.Is" or "errors.As".
	if e.nativeCausedBy != nil {
		errs = append(errs, e.nativeCausedBy)
	}

	causedBy := e.GetCausedBy()
	if causedBy == nil {
		return errs
	}

	errs = append(errs, causedBy)

	var toUnwrap interface{ Unwrap() []error }
	if errors.As(causedBy, &toUnwrap) {
		causedByChain := toUnwrap.Unwrap()
		if causedByChain != nil {
			errs = append(errs, causedByChain...)
		}
	}

	return errs
}

func ibmProblemf(err error, severity problemSeverity, component *ProblemComponent, summary, discriminator string) *IBMProblem {
	// Leaving summary blank is a convenient way to
	// use the message from the underlying problem.
	if summary == "" && err != nil {
		summary = err.Error()
	}

	newError := &IBMProblem{
		Summary:       summary,
		Component:     component,
		discriminator: discriminator,
		Severity:      severity,
	}

	var causedBy Problem
	if errors.As(err, &causedBy) {
		newError.causedBy = causedBy
	} else {
		newError.nativeCausedBy = err
	}

	return newError
}

// IBMErrorf creates and returns a new instance of an IBMProblem struct with "error"
// level severity. It is primarily meant for embedding IBMProblem structs in other types.
func IBMErrorf(err error, component *ProblemComponent, summary, discriminator string) *IBMProblem {
	return ibmProblemf(err, ErrorSeverity, component, summary, discriminator)
}
