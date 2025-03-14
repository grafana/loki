// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package globalsignal // import "go.opentelemetry.io/collector/pipeline/internal/globalsignal"

import (
	"errors"
	"fmt"
	"regexp"
)

// Signal represents the signals supported by the collector.
type Signal struct {
	name string
}

// String returns the string representation of the signal.
func (s Signal) String() string {
	return s.name
}

// MarshalText marshals the Signal.
func (s Signal) MarshalText() (text []byte, err error) {
	return []byte(s.name), nil
}

// signalRegex is used to validate the signal.
// A signal must consist of 1 to 62 lowercase ASCII alphabetic characters.
var signalRegex = regexp.MustCompile(`^[a-z]{1,62}$`)

// NewSignal creates a Signal. It returns an error if the Signal is invalid.
// A Signal must consist of 1 to 62 lowercase ASCII alphabetic characters.
func NewSignal(signal string) (Signal, error) {
	if len(signal) == 0 {
		return Signal{}, errors.New("signal must not be empty")
	}
	if !signalRegex.MatchString(signal) {
		return Signal{}, fmt.Errorf("invalid character(s) in type %q", signal)
	}
	return Signal{name: signal}, nil
}

// MustNewSignal creates a Signal. It panics if the Signal is invalid.
// A signal must consist of 1 to 62 lowercase ASCII alphabetic characters.
func MustNewSignal(signal string) Signal {
	s, err := NewSignal(signal)
	if err != nil {
		panic(err)
	}
	return s
}
