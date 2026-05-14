// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package model

import (
	"github.com/pb33f/libopenapi/datamodel/low"
	"github.com/pb33f/libopenapi/datamodel/low/base"
	"github.com/pb33f/libopenapi/datamodel/low/v3"
)

// TagChanges represents changes made to the Tags object of an OpenAPI document.
type TagChanges struct {
	*PropertyChanges
	ExternalDocs     *ExternalDocChanges `json:"externalDocs,omitempty" yaml:"externalDocs,omitempty"`
	ExtensionChanges *ExtensionChanges   `json:"extensions,omitempty" yaml:"extensions,omitempty"`
}

// GetAllChanges returns a slice of all changes made between Tag objects
func (t *TagChanges) GetAllChanges() []*Change {
	if t == nil {
		return nil
	}
	var changes []*Change
	changes = append(changes, t.Changes...)
	if t.ExternalDocs != nil {
		changes = append(changes, t.ExternalDocs.GetAllChanges()...)
	}
	if t.ExtensionChanges != nil {
		changes = append(changes, t.ExtensionChanges.GetAllChanges()...)
	}
	return changes
}

// TotalChanges returns a count of everything that changed within tags.
func (t *TagChanges) TotalChanges() int {
	if t == nil {
		return 0
	}
	c := t.PropertyChanges.TotalChanges()
	if t.ExternalDocs != nil {
		c += t.ExternalDocs.TotalChanges()
	}
	if t.ExtensionChanges != nil {
		c += t.ExtensionChanges.TotalChanges()
	}
	return c
}

// TotalBreakingChanges returns the number of breaking changes made by Tags
func (t *TagChanges) TotalBreakingChanges() int {
	return t.PropertyChanges.TotalBreakingChanges()
}

// CompareTags will compare a left (original) and a right (new) slice of ValueReference nodes for
// any changes between them. If there are changes, a pointer to TagChanges is returned, if not then
// nil is returned instead.
func CompareTags(l, r []low.ValueReference[*base.Tag]) []*TagChanges {
	var tagResults []*TagChanges

	// look at the original and then look through the new.
	seenLeft := make(map[string]*low.ValueReference[*base.Tag])
	seenRight := make(map[string]*low.ValueReference[*base.Tag])
	for i := range l {
		h := l[i]
		seenLeft[l[i].Value.Name.Value] = &h
	}
	for i := range r {
		h := r[i]
		seenRight[r[i].Value.Name.Value] = &h
	}

	// var changes []*Change

	// check for removals, modifications and moves
	for i := range seenLeft {
		tc := new(TagChanges)
		var changes []*Change

		CheckForObjectAdditionOrRemoval[*base.Tag](seenLeft, seenRight, i, &changes, BreakingAdded(CompTags, ""), BreakingRemoved(CompTags, ""))

		// if the existing tag exists, let's check it.
		if seenRight[i] != nil {
			lTag := seenLeft[i].Value
			rTag := seenRight[i].Value
			props := make([]*PropertyCheck, 0, 5)

			props = append(props,
				NewPropertyCheck(CompTag, PropName,
					lTag.Name.ValueNode, rTag.Name.ValueNode,
					v3.NameLabel, &changes, lTag, rTag),
				NewPropertyCheck(CompTag, PropSummary,
					lTag.Summary.ValueNode, rTag.Summary.ValueNode,
					v3.SummaryLabel, &changes, lTag, rTag),
				NewPropertyCheck(CompTag, PropDescription,
					lTag.Description.ValueNode, rTag.Description.ValueNode,
					v3.DescriptionLabel, &changes, lTag, rTag),
				NewPropertyCheck(CompTag, PropParent,
					lTag.Parent.ValueNode, rTag.Parent.ValueNode,
					v3.ParentLabel, &changes, lTag, rTag),
				NewPropertyCheck(CompTag, PropKind,
					lTag.Kind.ValueNode, rTag.Kind.ValueNode,
					v3.KindLabel, &changes, lTag, rTag),
			)

			// check properties
			CheckProperties(props)

			// compare external docs
			if !seenLeft[i].Value.ExternalDocs.IsEmpty() && !seenRight[i].Value.ExternalDocs.IsEmpty() {
				tc.ExternalDocs = CompareExternalDocs(seenLeft[i].Value.ExternalDocs.Value,
					seenRight[i].Value.ExternalDocs.Value)
			}
			if seenLeft[i].Value.ExternalDocs.IsEmpty() && !seenRight[i].Value.ExternalDocs.IsEmpty() {
				CreateChange(&changes, ObjectAdded, v3.ExternalDocsLabel, nil, seenRight[i].GetValueNode(),
					BreakingAdded(CompTag, PropExternalDocs), nil, seenRight[i].Value.ExternalDocs.Value)
			}
			if !seenLeft[i].Value.ExternalDocs.IsEmpty() && seenRight[i].Value.ExternalDocs.IsEmpty() {
				CreateChange(&changes, ObjectRemoved, v3.ExternalDocsLabel, seenLeft[i].GetValueNode(), nil,
					BreakingRemoved(CompTag, PropExternalDocs), seenLeft[i].Value.ExternalDocs.Value, nil)
			}

			// check extensions
			tc.ExtensionChanges = CompareExtensions(seenLeft[i].Value.Extensions, seenRight[i].Value.Extensions)
			tc.PropertyChanges = NewPropertyChanges(changes)
			if tc.TotalChanges() > 0 {
				tagResults = append(tagResults, tc)
			}
			continue
		}

		if len(changes) > 0 {
			tc.PropertyChanges = NewPropertyChanges(changes)
			tagResults = append(tagResults, tc)
		}

	}
	for i := range seenRight {
		if seenLeft[i] == nil {
			tc := new(TagChanges)
			var changes []*Change

			CreateChange(&changes, ObjectAdded, i, nil, seenRight[i].GetValueNode(),
				BreakingAdded(CompTags, ""), nil, seenRight[i].GetValue())

			tc.PropertyChanges = NewPropertyChanges(changes)
			tagResults = append(tagResults, tc)

		}
	}
	return tagResults
}
