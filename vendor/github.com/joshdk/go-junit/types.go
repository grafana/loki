// Copyright Josh Komoroske. All rights reserved.
// Use of this source code is governed by the MIT license,
// a copy of which can be found in the LICENSE.txt file.

package junit

import (
	"strings"
	"time"
)

// Status represents the result of a single a JUnit testcase. Indicates if a
// testcase was run, and if it was successful.
type Status string

const (
	// StatusPassed represents a JUnit testcase that was run, and did not
	// result in an error or a failure.
	StatusPassed Status = "passed"

	// StatusSkipped represents a JUnit testcase that was intentionally
	// skipped.
	StatusSkipped Status = "skipped"

	// StatusFailed represents a JUnit testcase that was run, but resulted in
	// a failure. Failures are violations of declared test expectations,
	// such as a failed assertion.
	StatusFailed Status = "failed"

	// StatusError represents a JUnit testcase that was run, but resulted in
	// an error. Errors are unexpected violations of the test itself, such as
	// an uncaught exception.
	StatusError Status = "error"
)

// Totals contains aggregated results across a set of test runs. Is usually
// calculated as a sum of all given test runs, and overrides whatever was given
// at the suite level.
//
// The following relation should hold true.
//   Tests == (Passed + Skipped + Failed + Error)
type Totals struct {
	// Tests is the total number of tests run.
	Tests int `json:"tests" yaml:"tests"`

	// Passed is the total number of tests that passed successfully.
	Passed int `json:"passed" yaml:"passed"`

	// Skipped is the total number of tests that were skipped.
	Skipped int `json:"skipped" yaml:"skipped"`

	// Failed is the total number of tests that resulted in a failure.
	Failed int `json:"failed" yaml:"failed"`

	// Error is the total number of tests that resulted in an error.
	Error int `json:"error" yaml:"error"`

	// Duration is the total time taken to run all tests.
	Duration time.Duration `json:"duration" yaml:"duration"`
}

// Suite represents a logical grouping (suite) of tests.
type Suite struct {
	// Name is a descriptor given to the suite.
	Name string `json:"name" yaml:"name"`

	// Package is an additional descriptor for the hierarchy of the suite.
	Package string `json:"package" yaml:"package"`

	// Properties is a mapping of key-value pairs that were available when the
	// tests were run.
	Properties map[string]string `json:"properties,omitempty" yaml:"properties,omitempty"`

	// Tests is an ordered collection of tests with associated results.
	Tests []Test `json:"tests,omitempty" yaml:"tests,omitempty"`

	// Suites is an ordered collection of suites with associated tests.
	Suites []Suite `json:"suites,omitempty" yaml:"suites,omitempty"`

	// SystemOut is textual test output for the suite. Usually output that is
	// written to stdout.
	SystemOut string `json:"stdout,omitempty" yaml:"stdout,omitempty"`

	// SystemErr is textual test error output for the suite. Usually output that is
	// written to stderr.
	SystemErr string `json:"stderr,omitempty" yaml:"stderr,omitempty"`

	// Totals is the aggregated results of all tests.
	Totals Totals `json:"totals" yaml:"totals"`
}

// Aggregate calculates result sums across all tests and nested suites.
func (s *Suite) Aggregate() {
	totals := Totals{Tests: len(s.Tests)}

	for _, test := range s.Tests {
		totals.Duration += test.Duration
		switch test.Status {
		case StatusPassed:
			totals.Passed++
		case StatusSkipped:
			totals.Skipped++
		case StatusFailed:
			totals.Failed++
		case StatusError:
			totals.Error++
		}
	}

	// just summing totals from nested suites
	for _, suite := range s.Suites {
		suite.Aggregate()
		totals.Tests += suite.Totals.Tests
		totals.Duration += suite.Totals.Duration
		totals.Passed += suite.Totals.Passed
		totals.Skipped += suite.Totals.Skipped
		totals.Failed += suite.Totals.Failed
		totals.Error += suite.Totals.Error
	}

	s.Totals = totals
}

// Test represents the results of a single test run.
type Test struct {
	// Name is a descriptor given to the test.
	Name string `json:"name" yaml:"name"`

	// Classname is an additional descriptor for the hierarchy of the test.
	Classname string `json:"classname" yaml:"classname"`

	// Duration is the total time taken to run the tests.
	Duration time.Duration `json:"duration" yaml:"duration"`

	// Status is the result of the test. Status values are passed, skipped,
	// failure, & error.
	Status Status `json:"status" yaml:"status"`

	// Message is an textual description optionally included with a skipped,
	// failure, or error test case.
	Message string `json:"message" yaml:"message"`

	// Error is a record of the failure or error of a test, if applicable.
	//
	// The following relations should hold true.
	//   Error == nil && (Status == Passed || Status == Skipped)
	//   Error != nil && (Status == Failed || Status == Error)
	Error error `json:"error" yaml:"error"`

	// Additional properties from XML node attributes.
	// Some tools use them to store additional information about test location.
	Properties map[string]string `json:"properties" yaml:"properties"`

	// SystemOut is textual output for the test case. Usually output that is
	// written to stdout.
	SystemOut string `json:"stdout,omitempty" yaml:"stdout,omitempty"`

	// SystemErr is textual error output for the test case. Usually output that is
	// written to stderr.
	SystemErr string `json:"stderr,omitempty" yaml:"stderr,omitempty"`
}

// Error represents an erroneous test result.
type Error struct {
	// Message is a descriptor given to the error. Purpose and values differ by
	// environment.
	Message string `json:"message,omitempty" yaml:"message,omitempty"`

	// Type is a descriptor given to the error. Purpose and values differ by
	// framework. Value is typically an exception class, such as an assertion.
	Type string `json:"type,omitempty" yaml:"type,omitempty"`

	// Body is extended text for the error. Purpose and values differ by
	// framework. Value is typically a stacktrace.
	Body string `json:"body,omitempty" yaml:"body,omitempty"`
}

// Error returns a textual description of the test error.
func (err Error) Error() string {
	switch {
	case strings.TrimSpace(err.Body) != "":
		return err.Body

	case strings.TrimSpace(err.Message) != "":
		return err.Message

	default:
		return err.Type
	}
}
