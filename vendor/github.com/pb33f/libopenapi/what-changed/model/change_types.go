// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package model

import (
	"encoding/json"

	"go.yaml.in/yaml/v4"
)

// Definitions of the possible changes between two items
const (

	// Modified means that was a modification of a value was made
	Modified = iota + 1

	// PropertyAdded means that a new property to an object was added
	PropertyAdded

	// ObjectAdded means that a new object was added to a parent object
	ObjectAdded

	// ObjectRemoved means that an object was removed from a parent object
	ObjectRemoved

	// PropertyRemoved means that a property of an object was removed
	PropertyRemoved
)

// WhatChanged is a summary object that contains a high level summary of everything changed.
type WhatChanged struct {
	Added        int `json:"added,omitempty" yaml:"added,omitempty"`
	Removed      int `json:"removed,omitempty" yaml:"removed,omitempty"`
	Modified     int `json:"modified,omitempty" yaml:"modified,omitempty"`
	TotalChanges int `json:"total,omitempty" yaml:"total,omitempty"`
}

// ChangeContext holds a reference to the line and column positions of original and new change.
type ChangeContext struct {
	DocumentLocation string `json:"document,omitempty" yaml:"document,omitempty"`
	OriginalLine     *int   `json:"originalLine,omitempty" yaml:"originalLine,omitempty"`
	OriginalColumn   *int   `json:"originalColumn,omitempty" yaml:"originalColumn,omitempty"`
	NewLine          *int   `json:"newLine,omitempty" yaml:"newLine,omitempty"`
	NewColumn        *int   `json:"newColumn,omitempty" yaml:"newColumn,omitempty"`
}

type ChangeIsReferenced interface {
	GetChangeReference() string
	SetChangeReference(ref string)
}

// HasChanged determines if the line and column numbers of the original and new values have changed.
//
// It's worth noting that there is no guarantee to the positions of anything in either left or right, so
// considering these values as 'changes' is going to add a considerable amount of noise to results.
func (c *ChangeContext) HasChanged() bool {
	if c.NewLine != nil && c.OriginalLine != nil && *c.NewLine != *c.OriginalLine {
		return true
	}
	//if c.NewColumn != nil && c.OriginalColumn != nil && *c.NewColumn != *c.OriginalColumn {
	//    return true
	//}
	if (c.NewLine == nil && c.OriginalLine != nil) || (c.NewLine != nil && c.OriginalLine == nil) {
		return true
	}
	//if (c.NewColumn == nil && c.OriginalColumn != nil) || (c.NewColumn != nil && c.OriginalColumn == nil) {
	//    return true
	//}
	return false
}

// Change represents a change between two different elements inside an OpenAPI specification.
type Change struct {
	// Context represents the lines and column numbers of the original and new values
	// It's worth noting that these values may frequently be different and are not used to calculate
	// a change. If the positions change, but values do not, then no change is recorded.
	Context *ChangeContext `json:"context,omitempty" yaml:"context,omitempty"`

	// ChangeType represents the type of change that occurred. stored as an integer, defined by constants above.
	ChangeType int `json:"change,omitempty" yaml:"change,omitempty"`

	// Property is the property name key being changed.
	Property string `json:"property,omitempty" yaml:"property,omitempty"`

	// Original is the original value represented as a string.
	Original string `json:"original,omitempty" yaml:"original,omitempty"`

	// New is the new value represented as a string.
	New string `json:"new,omitempty" yaml:"new,omitempty"`

	// OriginalEncoded is the original value serialized to YAML (for complex types like extensions).
	// Only populated for specific use cases (e.g., extension values that are objects/arrays).
	OriginalEncoded string `json:"originalEncoded,omitempty" yaml:"originalEncoded,omitempty"`

	// NewEncoded is the new value serialized to YAML (for complex types like extensions).
	// Only populated for specific use cases (e.g., extension values that are objects/arrays).
	NewEncoded string `json:"newEncoded,omitempty" yaml:"newEncoded,omitempty"`

	// Breaking determines if the change is a breaking one or not.
	Breaking bool `json:"breaking" yaml:"breaking"`

	// OriginalObject represents the original object that was changed.
	OriginalObject any `json:"-" yaml:"-"`

	// NewObject represents the new object that has been modified.
	NewObject any `json:"-" yaml:"-"`

	// Type represents the type of object that was changed. (not used in the current implementation).
	Type string `json:"type,omitempty"`

	// Path represents the path to the object that was changed (not used in the current implementation).
	Path string `json:"path,omitempty"`

	// Reference is populated when the change is related to a $ref change.
	Reference string `json:"reference,omitempty"`
}

// MarshalJSON is a custom JSON marshaller for the Change object.
func (c *Change) MarshalJSON() ([]byte, error) {
	changeType := ""
	switch c.ChangeType {
	case Modified:
		changeType = "modified"
	case PropertyAdded:
		changeType = "property_added"
	case ObjectAdded:
		changeType = "object_added"
	case ObjectRemoved:
		changeType = "object_removed"
	case PropertyRemoved:
		changeType = "property_removed"
	}
	data := map[string]interface{}{
		"change":     c.ChangeType,
		"changeText": changeType,
		"property":   c.Property,
		"breaking":   c.Breaking,
	}

	if c.Original != "" {
		data["original"] = c.Original
	}

	if c.New != "" {
		data["new"] = c.New
	}

	if c.OriginalEncoded != "" {
		data["originalEncoded"] = c.OriginalEncoded
	}

	if c.NewEncoded != "" {
		data["newEncoded"] = c.NewEncoded
	}

	if c.Context != nil {
		data["context"] = c.Context
	}
	if c.Type != "" {
		data["type"] = c.Type
	}
	if c.Path != "" {
		data["path"] = c.Path
	}
	return json.Marshal(data)
}

// PropertyChanges holds a slice of Change pointers
type PropertyChanges struct {
	RenderPropertiesOnly bool      `json:"-" yaml:"-"`
	ChangeReference      string    `json:"changeReference,omitempty""`
	Changes              []*Change `json:"changes,omitempty" yaml:"changes,omitempty"`
}

func (p *PropertyChanges) SetChangeReference(ref string) {
	p.ChangeReference = ref
}

func (p *PropertyChanges) GetChangeReference() string {
	return p.ChangeReference
}

// TotalChanges returns the total number of property changes made.
func (p *PropertyChanges) TotalChanges() int {
	if p == nil {
		return 0
	}
	return len(p.Changes)
}

// TotalBreakingChanges returns the total number of property breaking changes made.
func (p *PropertyChanges) TotalBreakingChanges() int {
	return CountBreakingChanges(p.Changes)
}

// PropertiesOnly will set the change object to only render properties, not the timeline.
func (p *PropertyChanges) PropertiesOnly() {
	p.RenderPropertiesOnly = true
}

// GetPropertyChanges will return just the property changes
func (p *PropertyChanges) GetPropertyChanges() []*Change {
	return p.Changes
}

func NewPropertyChanges(changes []*Change) *PropertyChanges {
	return &PropertyChanges{Changes: changes}
}

// PropertyCheck is used by functions to check the state of left and right values.
type PropertyCheck struct {
	// Original is the property we're checking on the left
	Original any

	// New is s the property we're checking on the right
	New any

	// Label is the identifier we're looking for on the left and right hand sides
	Label string

	// LeftNode is the yaml.Node pointer that holds the original node structure of the value
	LeftNode *yaml.Node

	// RightNode is the yaml.Node pointer that holds the new node structure of the value
	RightNode *yaml.Node

	// Breaking determines if the check is a breaking change (modifications or removals etc.)
	//
	// Deprecated: Use Component and Property fields for configurable breaking rules.
	// This field is used as a fallback when Component is not set.
	//
	// TODO: Migration to Component/Property-based breaking rules
	//
	// Current state: CreateChange() takes a `breaking bool` parameter that is computed via
	// BreakingAdded/Modified/Removed(component, property) and stored directly on this field.
	// The breaking status is fixed at Change creation time.
	//
	// Target state: CreateChange() should accept `component, property string` parameters instead,
	// store them on the Change, and breaking status should be computed at runtime via IsBreaking().
	// This allows users to apply different breaking rule configs to the same set of changes.
	//
	// Migration steps:
	//   1. Extend CreateChange signature to accept component, property string
	//   2. Update ~198 CreateChange call sites across 23 files
	//   3. Add IsBreaking() method on Change that computes from Component/Property
	//   4. Keep Breaking field populated for backward compatibility
	//
	// Files with most CreateChange calls: schema.go (51), path_item.go (42), operation.go (22)
	Breaking bool

	// Component is the OpenAPI component type (e.g., CompTag, CompSchema) for breaking rules lookup.
	// When set along with Property, the configurable breaking rules system is used instead of Breaking.
	// TODO: Currently not populated - see Breaking field TODO for migration plan.
	Component string

	// Property is the property name within the component (e.g., PropParent, PropName) for breaking rules lookup.
	// Used together with Component to look up the correct breaking rule for each change type.
	// TODO: Currently not populated - see Breaking field TODO for migration plan.
	Property string

	// Changes represents a pointer to the slice to contain all changes found.
	Changes *[]*Change
}

// NewPropertyCheck creates a PropertyCheck with the Component and Property fields set for
// configurable breaking rules. This is the preferred way to create PropertyCheck instances.
func NewPropertyCheck(
	component, property string,
	leftNode, rightNode *yaml.Node,
	label string,
	changes *[]*Change,
	original, new any,
) *PropertyCheck {
	return &PropertyCheck{
		LeftNode:  leftNode,
		RightNode: rightNode,
		Label:     label,
		Changes:   changes,
		Breaking:  BreakingModified(component, property), // fallback for legacy code paths
		Component: component,
		Property:  property,
		Original:  original,
		New:       new,
	}
}
