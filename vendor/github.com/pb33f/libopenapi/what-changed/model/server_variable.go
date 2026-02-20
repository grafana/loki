// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package model

import (
	"github.com/pb33f/libopenapi/datamodel/low"
	"github.com/pb33f/libopenapi/datamodel/low/v3"
)

// ServerVariableChanges represents changes found between two OpenAPI ServerVariable Objects
type ServerVariableChanges struct {
	*PropertyChanges
}

// GetAllChanges returns a slice of all changes made between SecurityRequirement objects
func (s *ServerVariableChanges) GetAllChanges() []*Change {
	if s == nil {
		return nil
	}
	return s.Changes
}

// CompareServerVariables compares a left and right OpenAPI ServerVariable object for changes.
// If anything is found, returns a pointer to a ServerVariableChanges instance, otherwise returns nil.
func CompareServerVariables(l, r *v3.ServerVariable) *ServerVariableChanges {
	if low.AreEqual(l, r) {
		return nil
	}

	var changes []*Change

	lValues := make(map[string]low.NodeReference[string])
	rValues := make(map[string]low.NodeReference[string])
	for i := range l.Enum {
		lValues[l.Enum[i].Value] = l.Enum[i]
	}
	for i := range r.Enum {
		rValues[r.Enum[i].Value] = r.Enum[i]
	}
	for k := range lValues {
		if _, ok := rValues[k]; !ok {
			CreateChange(&changes, ObjectRemoved, v3.EnumLabel,
				lValues[k].ValueNode, nil, BreakingRemoved(CompServerVariable, PropEnum),
				lValues[k].Value, nil)
			continue
		}
	}
	for k := range rValues {
		if _, ok := lValues[k]; !ok {
			CreateChange(&changes, ObjectAdded, v3.EnumLabel,
				lValues[k].ValueNode, rValues[k].ValueNode, BreakingAdded(CompServerVariable, PropEnum),
				lValues[k].Value, rValues[k].Value)
		}
	}

	props := make([]*PropertyCheck, 0, 2)
	props = append(props,
		NewPropertyCheck(CompServerVariable, PropDefault,
			l.Default.ValueNode, r.Default.ValueNode,
			v3.DefaultLabel, &changes, l, r),
		NewPropertyCheck(CompServerVariable, PropDescription,
			l.Description.ValueNode, r.Description.ValueNode,
			v3.DescriptionLabel, &changes, l, r),
	)

	CheckProperties(props)
	sc := new(ServerVariableChanges)
	sc.PropertyChanges = NewPropertyChanges(changes)
	return sc
}
