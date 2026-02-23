// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package model

import (
	"github.com/pb33f/libopenapi/datamodel/low/base"
	v3 "github.com/pb33f/libopenapi/datamodel/low/v3"
)

// XMLChanges represents changes made to the XML object of an OpenAPI document.
type XMLChanges struct {
	*PropertyChanges
	ExtensionChanges *ExtensionChanges `json:"extensions,omitempty" yaml:"extensions,omitempty"`
}

// GetAllChanges returns a slice of all changes made between XML objects
func (x *XMLChanges) GetAllChanges() []*Change {
	if x == nil {
		return nil
	}
	var changes []*Change
	changes = append(changes, x.Changes...)
	if x.ExtensionChanges != nil {
		changes = append(changes, x.ExtensionChanges.GetAllChanges()...)
	}
	return changes
}

// TotalChanges returns a count of everything that was changed within an XML object.
func (x *XMLChanges) TotalChanges() int {
	if x == nil {
		return 0
	}
	c := x.PropertyChanges.TotalChanges()
	if x.ExtensionChanges != nil {
		c += x.ExtensionChanges.TotalChanges()
	}
	return c
}

// TotalBreakingChanges returns the number of breaking changes made by the XML object.
func (x *XMLChanges) TotalBreakingChanges() int {
	return x.PropertyChanges.TotalBreakingChanges()
}

// CompareXML will compare a left (original) and a right (new) XML instance, and check for
// any changes between them. If changes are found, the function returns a pointer to XMLChanges,
// otherwise, if nothing changed - it will return nil
func CompareXML(l, r *base.XML) *XMLChanges {
	xc := new(XMLChanges)
	var changes []*Change
	props := make([]*PropertyCheck, 0, 6)

	props = append(props,
		NewPropertyCheck(CompXML, PropName,
			l.Name.ValueNode, r.Name.ValueNode,
			v3.NameLabel, &changes, l, r),
		NewPropertyCheck(CompXML, PropNamespace,
			l.Namespace.ValueNode, r.Namespace.ValueNode,
			v3.NamespaceLabel, &changes, l, r),
		NewPropertyCheck(CompXML, PropPrefix,
			l.Prefix.ValueNode, r.Prefix.ValueNode,
			v3.PrefixLabel, &changes, l, r),
		NewPropertyCheck(CompXML, PropAttribute,
			l.Attribute.ValueNode, r.Attribute.ValueNode,
			v3.AttributeLabel, &changes, l, r),
		NewPropertyCheck(CompXML, PropNodeType,
			l.NodeType.ValueNode, r.NodeType.ValueNode,
			base.NodeTypeLabel, &changes, l, r),
		NewPropertyCheck(CompXML, PropWrapped,
			l.Wrapped.ValueNode, r.Wrapped.ValueNode,
			v3.WrappedLabel, &changes, l, r),
	)

	CheckProperties(props)

	// check extensions
	xc.ExtensionChanges = CheckExtensions(l, r)
	xc.PropertyChanges = NewPropertyChanges(changes)
	if xc.TotalChanges() <= 0 {
		return nil
	}
	return xc
}
