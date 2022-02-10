// Copyright The OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//       http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package config // import "go.opentelemetry.io/collector/config"

import (
	"errors"
	"fmt"
	"strings"
)

// typeAndNameSeparator is the separator that is used between type and name in type/name composite keys.
const typeAndNameSeparator = "/"

// identifiable is an interface that all components configurations MUST embed.
type identifiable interface {
	// ID returns the ID of the component that this configuration belongs to.
	ID() ComponentID
	// SetIDName updates the name part of the ID for the component that this configuration belongs to.
	SetIDName(idName string)
}

// ComponentID represents the identity for a component. It combines two values:
// * type - the Type of the component.
// * name - the name of that component.
// The component ComponentID (combination type + name) is unique for a given component.Kind.
type ComponentID struct {
	typeVal Type   `mapstructure:"-"`
	nameVal string `mapstructure:"-"`
}

// NewComponentID returns a new ComponentID with the given Type and empty name.
func NewComponentID(typeVal Type) ComponentID {
	return ComponentID{typeVal: typeVal}
}

// NewComponentIDWithName returns a new ComponentID with the given Type and name.
func NewComponentIDWithName(typeVal Type, nameVal string) ComponentID {
	return ComponentID{typeVal: typeVal, nameVal: nameVal}
}

// NewComponentIDFromString decodes a string in type[/name] format into ComponentID.
// The type and name components will have spaces trimmed, the "type" part must be present,
// the forward slash and "name" are optional.
// The returned ComponentID will be invalid if err is not-nil.
func NewComponentIDFromString(idStr string) (ComponentID, error) {
	id := ComponentID{}
	return id, id.UnmarshalText([]byte(idStr))
}

// Type returns the type of the component.
func (id ComponentID) Type() Type {
	return id.typeVal
}

// Name returns the custom name of the component.
func (id ComponentID) Name() string {
	return id.nameVal
}

// UnmarshalText implements the encoding.TextUnmarshaler interface.
func (id *ComponentID) UnmarshalText(text []byte) error {
	idStr := string(text)
	items := strings.SplitN(idStr, typeAndNameSeparator, 2)
	if len(items) >= 1 {
		id.typeVal = Type(strings.TrimSpace(items[0]))
	}

	if len(items) == 1 && id.typeVal == "" {
		return errors.New("id must not be empty")
	}

	if id.typeVal == "" {
		return fmt.Errorf("in %q id: the part before %s should not be empty", idStr, typeAndNameSeparator)
	}

	if len(items) > 1 {
		// "name" part is present.
		id.nameVal = strings.TrimSpace(items[1])
		if id.nameVal == "" {
			return fmt.Errorf("in %q id: the part after %s should not be empty", idStr, typeAndNameSeparator)
		}
	}

	return nil
}

// String returns the ComponentID string representation as "type[/name]" format.
func (id ComponentID) String() string {
	if id.nameVal == "" {
		return string(id.typeVal)
	}

	return string(id.typeVal) + typeAndNameSeparator + id.nameVal
}
