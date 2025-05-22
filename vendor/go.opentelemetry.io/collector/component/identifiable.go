// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package component // import "go.opentelemetry.io/collector/component"

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// typeAndNameSeparator is the separator that is used between type and name in type/name composite keys.
const typeAndNameSeparator = "/"

var (
	// typeRegexp is used to validate the type of component.
	// A type must start with an ASCII alphabetic character and
	// can only contain ASCII alphanumeric characters and '_'.
	// This must be kept in sync with the regex in cmd/mdatagen/validate.go.
	typeRegexp = regexp.MustCompile(`^[a-zA-Z][0-9a-zA-Z_]{0,62}$`)

	// nameRegexp is used to validate the name of a component. A name can consist of
	// 1 to 1024 Unicode characters excluding whitespace, control characters, and
	// symbols.
	nameRegexp = regexp.MustCompile(`^[^\pZ\pC\pS]+$`)
)

var _ fmt.Stringer = Type{}

// Type is the component type as it is used in the config.
type Type struct {
	name string
}

// String returns the string representation of the type.
func (t Type) String() string {
	return t.name
}

// MarshalText marshals returns the Type name.
func (t Type) MarshalText() ([]byte, error) {
	return []byte(t.name), nil
}

// NewType creates a type. It returns an error if the type is invalid.
// A type must
// - have at least one character,
// - start with an ASCII alphabetic character and
// - can only contain ASCII alphanumeric characters and '_'.
func NewType(ty string) (Type, error) {
	if len(ty) == 0 {
		return Type{}, errors.New("id must not be empty")
	}
	if !typeRegexp.MatchString(ty) {
		return Type{}, fmt.Errorf("invalid character(s) in type %q", ty)
	}
	return Type{name: ty}, nil
}

// MustNewType creates a type. It panics if the type is invalid.
// A type must
// - have at least one character,
// - start with an ASCII alphabetic character and
// - can only contain ASCII alphanumeric characters and '_'.
func MustNewType(strType string) Type {
	ty, err := NewType(strType)
	if err != nil {
		panic(err)
	}
	return ty
}

// ID represents the identity for a component. It combines two values:
// * type - the Type of the component.
// * name - the name of that component.
// The component ID (combination type + name) is unique for a given component.Kind.
type ID struct {
	typeVal Type   `mapstructure:"-"`
	nameVal string `mapstructure:"-"`
}

// NewID returns a new ID with the given Type and empty name.
func NewID(typeVal Type) ID {
	return ID{typeVal: typeVal}
}

// MustNewID builds a Type and returns a new ID with the given Type and empty name.
// This is equivalent to NewID(MustNewType(typeVal)).
// See MustNewType to check the valid values of typeVal.
func MustNewID(typeVal string) ID {
	return NewID(MustNewType(typeVal))
}

// NewIDWithName returns a new ID with the given Type and name.
func NewIDWithName(typeVal Type, nameVal string) ID {
	return ID{typeVal: typeVal, nameVal: nameVal}
}

// MustNewIDWithName builds a Type and returns a new ID with the given Type and name.
// This is equivalent to NewIDWithName(MustNewType(typeVal), nameVal).
// See MustNewType to check the valid values of typeVal.
func MustNewIDWithName(typeVal string, nameVal string) ID {
	return NewIDWithName(MustNewType(typeVal), nameVal)
}

// Type returns the type of the component.
func (id ID) Type() Type {
	return id.typeVal
}

// Name returns the custom name of the component.
func (id ID) Name() string {
	return id.nameVal
}

// MarshalText implements the encoding.TextMarshaler interface.
// This marshals the type and name as one string in the config.
func (id ID) MarshalText() (text []byte, err error) {
	return []byte(id.String()), nil
}

// UnmarshalText implements the encoding.TextUnmarshaler interface.
func (id *ID) UnmarshalText(text []byte) error {
	idStr := string(text)
	items := strings.SplitN(idStr, typeAndNameSeparator, 2)
	var typeStr, nameStr string
	if len(items) >= 1 {
		typeStr = strings.TrimSpace(items[0])
	}

	if len(items) == 1 && typeStr == "" {
		return errors.New("id must not be empty")
	}

	if typeStr == "" {
		return fmt.Errorf("in %q id: the part before %s should not be empty", idStr, typeAndNameSeparator)
	}

	if len(items) > 1 {
		// "name" part is present.
		nameStr = strings.TrimSpace(items[1])
		if nameStr == "" {
			return fmt.Errorf("in %q id: the part after %s should not be empty", idStr, typeAndNameSeparator)
		}
		if err := validateName(nameStr); err != nil {
			return fmt.Errorf("in %q id: %w", nameStr, err)
		}
	}

	var err error
	if id.typeVal, err = NewType(typeStr); err != nil {
		return fmt.Errorf("in %q id: %w", idStr, err)
	}
	id.nameVal = nameStr

	return nil
}

// String returns the ID string representation as "type[/name]" format.
func (id ID) String() string {
	if id.nameVal == "" {
		return id.typeVal.String()
	}

	return id.typeVal.String() + typeAndNameSeparator + id.nameVal
}

func validateName(nameStr string) error {
	if len(nameStr) > 1024 {
		return fmt.Errorf("name %q is longer than 1024 characters (%d characters)", nameStr, len(nameStr))
	}
	if !nameRegexp.MatchString(nameStr) {
		return fmt.Errorf("invalid character(s) in name %q", nameStr)
	}
	return nil
}
