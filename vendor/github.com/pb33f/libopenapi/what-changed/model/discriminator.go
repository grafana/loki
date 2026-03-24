// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package model

import (
	"github.com/pb33f/libopenapi/datamodel/low/base"
)

// DiscriminatorChanges represents changes made to a Discriminator OpenAPI object
type DiscriminatorChanges struct {
	*PropertyChanges
	MappingChanges []*Change `json:"mappings,omitempty" yaml:"mappings,omitempty"`
}

// TotalChanges returns a count of everything changed within the Discriminator object
func (d *DiscriminatorChanges) TotalChanges() int {
	if d == nil {
		return 0
	}
	l := 0
	if k := d.PropertyChanges.TotalChanges(); k > 0 {
		l += k
	}
	if k := len(d.MappingChanges); k > 0 {
		l += k
	}
	return l
}

// GetAllChanges returns a slice of all changes made between Callback objects
func (c *DiscriminatorChanges) GetAllChanges() []*Change {
	if c == nil {
		return nil
	}
	var changes []*Change
	changes = append(changes, c.Changes...)
	if c.MappingChanges != nil {
		changes = append(changes, c.MappingChanges...)
	}
	return changes
}

// TotalBreakingChanges returns the number of breaking changes made by the Discriminator
func (d *DiscriminatorChanges) TotalBreakingChanges() int {
	return d.PropertyChanges.TotalBreakingChanges() + CountBreakingChanges(d.MappingChanges)
}

// CompareDiscriminator will check a left (original) and right (new) Discriminator object for changes
// and will return a pointer to DiscriminatorChanges
func CompareDiscriminator(l, r *base.Discriminator) *DiscriminatorChanges {
	dc := new(DiscriminatorChanges)
	var changes []*Change
	props := make([]*PropertyCheck, 0, 2)
	var mappingChanges []*Change

	props = append(props,
		NewPropertyCheck(CompDiscriminator, PropPropertyName,
			l.PropertyName.ValueNode, r.PropertyName.ValueNode,
			base.PropertyNameLabel, &changes, l, r),
		NewPropertyCheck(CompDiscriminator, PropDefaultMapping,
			l.DefaultMapping.ValueNode, r.DefaultMapping.ValueNode,
			base.DefaultMappingLabel, &changes, l, r),
	)

	CheckProperties(props)

	// flatten maps
	lMap := FlattenLowLevelOrderedMap[string](l.Mapping.Value)
	rMap := FlattenLowLevelOrderedMap[string](r.Mapping.Value)

	// check for removals, modifications and moves
	for i := range lMap {
		CheckForObjectAdditionOrRemoval[string](lMap, rMap, i, &mappingChanges, BreakingAdded(CompDiscriminator, PropMapping), BreakingRemoved(CompDiscriminator, PropMapping))
		// if the existing tag exists, let's check it.
		if rMap[i] != nil {
			if lMap[i].Value != rMap[i].Value {
				CreateChange(&mappingChanges, Modified, i, lMap[i].GetValueNode(),
					rMap[i].GetValueNode(), BreakingModified(CompDiscriminator, PropMapping), lMap[i].GetValue(), rMap[i].GetValue())
			}
		}
	}

	for i := range rMap {
		if lMap[i] == nil {
			CreateChange(&mappingChanges, ObjectAdded, i, nil,
				rMap[i].GetValueNode(), BreakingAdded(CompDiscriminator, PropMapping), nil, rMap[i].GetValue())
		}
	}

	dc.PropertyChanges = NewPropertyChanges(changes)
	dc.MappingChanges = mappingChanges
	if dc.TotalChanges() <= 0 {
		return nil
	}
	return dc
}
