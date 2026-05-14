// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package model

import (
	"strings"

	"github.com/pb33f/libopenapi/datamodel/low"
	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
)

// ExtensionChanges represents any changes to custom extensions defined for an OpenAPI object.
type ExtensionChanges struct {
	*PropertyChanges
}

// GetAllChanges returns a slice of all changes made between Extension objects
func (e *ExtensionChanges) GetAllChanges() []*Change {
	if e == nil {
		return nil
	}
	return e.Changes
}

// TotalChanges returns the total number of object extensions that were made.
func (e *ExtensionChanges) TotalChanges() int {
	if e == nil {
		return 0
	}
	return e.PropertyChanges.TotalChanges()
}

// TotalBreakingChanges returns the total number of breaking changes in Extension objects.
func (e *ExtensionChanges) TotalBreakingChanges() int {
	if e == nil {
		return 0
	}
	return e.PropertyChanges.TotalBreakingChanges()
}

// CompareExtensions will compare a left and right map of Tag/ValueReference models for any changes to
// anything. This function does not try and cast the value of an extension to perform checks, it
// will perform a basic value check.
//
// A current limitation relates to extensions being objects and a property of the object changes,
// there is currently no support for knowing anything changed - so it is ignored.
func CompareExtensions(l, r *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]]) *ExtensionChanges {
	// look at the original and then look through the new.
	seenLeft := make(map[string]*low.ValueReference[*yaml.Node])
	seenRight := make(map[string]*low.ValueReference[*yaml.Node])

	for k, h := range l.FromOldest() {
		seenLeft[strings.ToLower(k.Value)] = &h
	}
	for k, h := range r.FromOldest() {
		seenRight[strings.ToLower(k.Value)] = &h
	}

	var changes []*Change
	for i := range seenLeft {

		CheckForObjectAdditionOrRemovalWithEncoding[*yaml.Node](seenLeft, seenRight, i, &changes, false, true)

		if seenRight[i] != nil {
			var props []*PropertyCheck

			props = append(props, &PropertyCheck{
				LeftNode:  seenLeft[i].ValueNode,
				RightNode: seenRight[i].ValueNode,
				Label:     i,
				Changes:   &changes,
				Breaking:  false,
				Original:  seenLeft[i].Value,
				New:       seenRight[i].Value,
			})

			// check properties with encoding for extensions
			CheckPropertiesWithEncoding(props)
		}
	}
	for i := range seenRight {
		if seenLeft[i] == nil {
			CheckForObjectAdditionOrRemovalWithEncoding[*yaml.Node](seenLeft, seenRight, i, &changes, false, true)
		}
	}
	ex := new(ExtensionChanges)
	ex.PropertyChanges = NewPropertyChanges(changes)
	if ex.TotalChanges() <= 0 {
		return nil
	}
	return ex
}

// CheckExtensions is a helper method to un-pack a left and right model that contains extensions. Once unpacked
// the extensions are compared and returns a pointer to ExtensionChanges. If nothing changed, nil is returned.
func CheckExtensions[T low.HasExtensions[T]](l, r T) *ExtensionChanges {
	var lExt, rExt *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]]
	if orderedmap.Len(l.GetExtensions()) > 0 {
		lExt = l.GetExtensions()
	}
	if orderedmap.Len(r.GetExtensions()) > 0 {
		rExt = r.GetExtensions()
	}
	return CompareExtensions(lExt, rExt)
}
