// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package componentalias // import "go.opentelemetry.io/collector/internal/componentalias"

import (
	"errors"
	"fmt"

	"go.opentelemetry.io/collector/component"
)

type TypeAliasHolder interface {
	DeprecatedAlias() component.Type
	SetDeprecatedAlias(component.Type)
}

func NewTypeAliasHolder() TypeAliasHolder {
	ta := typeAlias(component.Type{})
	return &ta
}

type typeAlias component.Type

// DeprecatedAlias returns the deprecated type typeAlias for this component, if any.
// Returns an empty component.Type if no typeAlias is configured.
func (ta *typeAlias) DeprecatedAlias() component.Type {
	return component.Type(*ta)
}

// SetDeprecatedAlias sets the deprecated type typeAlias.
func (ta *typeAlias) SetDeprecatedAlias(newAlias component.Type) {
	*ta = typeAlias(newAlias)
}

// ValidateComponentType returns an error if the provided factory does not match the provided component ID.
// It checks both the current type and any deprecated alias type.
func ValidateComponentType(f component.Factory, id component.ID) error {
	if id.Type() == f.Type() {
		return nil
	}
	errMsg := fmt.Sprintf("component type mismatch: component ID %q does not have type %q", id, f.Type())
	if aliasHolder, ok := f.(TypeAliasHolder); ok && aliasHolder.DeprecatedAlias().String() != "" {
		if id.Type() == aliasHolder.DeprecatedAlias() {
			return nil
		}
		errMsg += fmt.Sprintf(" or deprecated alias type %q", aliasHolder.DeprecatedAlias())
	}
	return errors.New(errMsg)
}
