// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package pipeline // import "go.opentelemetry.io/collector/pipeline"
import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// typeAndNameSeparator is the separator that is used between type and name in type/name composite keys.
const typeAndNameSeparator = "/"

// ID represents the identity for a pipeline. It combines two values:
// * signal - the Signal of the pipeline.
// * name - the name of that pipeline.
type ID struct {
	signal Signal `mapstructure:"-"`
	name   string `mapstructure:"-"`
}

// NewID returns a new ID with the given Signal and empty name.
func NewID(signal Signal) ID {
	return NewIDWithName(signal, "")
}

// NewIDWithName returns a new ID with the given Signal and name.
func NewIDWithName(signal Signal, name string) ID {
	return ID{signal: signal, name: name}
}

// Signal returns the Signal of the ID.
func (i ID) Signal() Signal {
	return i.signal
}

// Name returns the name of the ID.
func (i ID) Name() string {
	return i.name
}

// MarshalText implements the encoding.TextMarshaler interface.
// This marshals the Signal and name as one string in the config.
func (i ID) MarshalText() (text []byte, err error) {
	return []byte(i.String()), nil
}

// UnmarshalText implements the encoding.TextUnmarshaler interface.
func (i *ID) UnmarshalText(text []byte) error {
	idStr := string(text)
	signalStr, nameStr, hasName := strings.Cut(idStr, typeAndNameSeparator)
	signalStr = strings.TrimSpace(signalStr)

	if signalStr == "" {
		if hasName {
			return fmt.Errorf("in %q id: the part before %s should not be empty", idStr, typeAndNameSeparator)
		}
		return errors.New("id must not be empty")
	}

	if hasName {
		// "name" part is present.
		nameStr = strings.TrimSpace(nameStr)
		if nameStr == "" {
			return fmt.Errorf("in %q id: the part after %s should not be empty", idStr, typeAndNameSeparator)
		}
		if err := validateName(nameStr); err != nil {
			return fmt.Errorf("in %q id: %w", nameStr, err)
		}
	}

	if err := i.signal.UnmarshalText([]byte(signalStr)); err != nil {
		return fmt.Errorf("in %q id: %w", idStr, err)
	}
	i.name = nameStr

	return nil
}

// String returns the ID string representation as "signal[/name]" format.
func (i ID) String() string {
	if i.name == "" {
		return i.signal.String()
	}

	return i.signal.String() + typeAndNameSeparator + i.name
}

// nameRegexp is used to validate the name of an ID. A name can consist of
// 1 to 1024 unicode characters excluding whitespace, control characters, and
// symbols.
var nameRegexp = regexp.MustCompile(`^[^\pZ\pC\pS]+$`)

func validateName(nameStr string) error {
	if len(nameStr) > 1024 {
		return fmt.Errorf("name %q is longer than 1024 characters (%d characters)", nameStr, len(nameStr))
	}
	if !nameRegexp.MatchString(nameStr) {
		return fmt.Errorf("invalid character(s) in name %q", nameStr)
	}
	return nil
}
