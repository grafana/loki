// Copyright 2022-2025 Princess Beef Heavy Industries, LLC / Dave Shanley
// SPDX-License-Identifier: MIT

package model

import (
	"fmt"
	"slices"
	"sort"
	"sync"

	"github.com/pb33f/libopenapi/datamodel/low"
	"github.com/pb33f/libopenapi/datamodel/low/base"
	v3 "github.com/pb33f/libopenapi/datamodel/low/v3"
	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
)

// SchemaChanges represent all changes to a base.Schema OpenAPI object. These changes are represented
// by all versions of OpenAPI.
//
// Any additions or removals to slice based results will be recorded in the PropertyChanges of the parent
// changes, and not the child for example, adding a new schema to `anyOf` will create a new change result in
// PropertyChanges.Changes, and not in the AnyOfChanges property.
type SchemaChanges struct {
	*PropertyChanges
	DiscriminatorChanges        *DiscriminatorChanges     `json:"discriminator,omitempty" yaml:"discriminator,omitempty"`
	AllOfChanges                []*SchemaChanges          `json:"allOf,omitempty" yaml:"allOf,omitempty"`
	AnyOfChanges                []*SchemaChanges          `json:"anyOf,omitempty" yaml:"anyOf,omitempty"`
	OneOfChanges                []*SchemaChanges          `json:"oneOf,omitempty" yaml:"oneOf,omitempty"`
	PrefixItemsChanges          []*SchemaChanges          `json:"prefixItems,omitempty" yaml:"prefixItems,omitempty"`
	NotChanges                  *SchemaChanges            `json:"not,omitempty" yaml:"not,omitempty"`
	ItemsChanges                *SchemaChanges            `json:"items,omitempty" yaml:"items,omitempty"`
	SchemaPropertyChanges       map[string]*SchemaChanges `json:"properties,omitempty" yaml:"properties,omitempty"`
	ExternalDocChanges          *ExternalDocChanges       `json:"externalDoc,omitempty" yaml:"externalDoc,omitempty"`
	XMLChanges                  *XMLChanges               `json:"xml,omitempty" yaml:"xml,omitempty"`
	ExtensionChanges            *ExtensionChanges         `json:"extensions,omitempty" yaml:"extensions,omitempty"`
	AdditionalPropertiesChanges *SchemaChanges            `json:"additionalProperties,omitempty" yaml:"additionalProperties,omitempty"`

	// 3.1 specifics
	IfChanges                    *SchemaChanges            `json:"if,omitempty" yaml:"if,omitempty"`
	ElseChanges                  *SchemaChanges            `json:"else,omitempty" yaml:"else,omitempty"`
	ThenChanges                  *SchemaChanges            `json:"then,omitempty" yaml:"then,omitempty"`
	PropertyNamesChanges         *SchemaChanges            `json:"propertyNames,omitempty" yaml:"propertyNames,omitempty"`
	ContainsChanges              *SchemaChanges            `json:"contains,omitempty" yaml:"contains,omitempty"`
	UnevaluatedItemsChanges      *SchemaChanges            `json:"unevaluatedItems,omitempty" yaml:"unevaluatedItems,omitempty"`
	UnevaluatedPropertiesChanges *SchemaChanges            `json:"unevaluatedProperties,omitempty" yaml:"unevaluatedProperties,omitempty"`
	DependentSchemasChanges      map[string]*SchemaChanges `json:"dependentSchemas,omitempty" yaml:"dependentSchemas,omitempty"`
	DependentRequiredChanges     []*Change                 `json:"dependentRequired,omitempty" yaml:"dependentRequired,omitempty"`
	PatternPropertiesChanges     map[string]*SchemaChanges `json:"patternProperties,omitempty" yaml:"patternProperties,omitempty"`
	ContentSchemaChanges         *SchemaChanges            `json:"contentSchema,omitempty" yaml:"contentSchema,omitempty"`
	VocabularyChanges            []*Change                 `json:"$vocabulary,omitempty" yaml:"$vocabulary,omitempty"`
}

func (s *SchemaChanges) GetPropertyChanges() []*Change {
	if s == nil {
		return nil
	}
	changes := s.Changes
	if s.SchemaPropertyChanges != nil {
		for n := range s.SchemaPropertyChanges {
			if s.SchemaPropertyChanges[n] != nil {
				changes = append(changes, s.SchemaPropertyChanges[n].GetAllChanges()...)
			}
		}
	}
	if s.DependentSchemasChanges != nil {
		for n := range s.DependentSchemasChanges {
			if s.DependentSchemasChanges[n] != nil {
				changes = append(changes, s.DependentSchemasChanges[n].GetAllChanges()...)
			}
		}
	}
	if len(s.DependentRequiredChanges) > 0 {
		changes = append(changes, s.DependentRequiredChanges...)
	}
	if s.PatternPropertiesChanges != nil {
		for n := range s.PatternPropertiesChanges {
			if s.PatternPropertiesChanges[n] != nil {
				changes = append(changes, s.PatternPropertiesChanges[n].GetAllChanges()...)
			}
		}
	}
	if s.XMLChanges != nil {
		changes = append(changes, s.XMLChanges.GetAllChanges()...)
	}
	return changes
}

// GetAllChanges returns a slice of all changes made between Responses objects
func (s *SchemaChanges) GetAllChanges() []*Change {
	if s == nil {
		return nil
	}
	var changes []*Change
	changes = append(changes, s.Changes...)
	if s.DiscriminatorChanges != nil {
		changes = append(changes, s.DiscriminatorChanges.GetAllChanges()...)
	}
	if len(s.AllOfChanges) > 0 {
		for n := range s.AllOfChanges {
			if s.AllOfChanges[n] != nil {
				changes = append(changes, s.AllOfChanges[n].GetAllChanges()...)
			}
		}
	}
	if len(s.AnyOfChanges) > 0 {
		for n := range s.AnyOfChanges {
			if s.AnyOfChanges[n] != nil {
				changes = append(changes, s.AnyOfChanges[n].GetAllChanges()...)
			}
		}
	}
	if len(s.OneOfChanges) > 0 {
		for n := range s.OneOfChanges {
			if s.OneOfChanges[n] != nil {
				changes = append(changes, s.OneOfChanges[n].GetAllChanges()...)
			}
		}
	}
	if len(s.PrefixItemsChanges) > 0 {
		for n := range s.PrefixItemsChanges {
			if s.PrefixItemsChanges[n] != nil {
				changes = append(changes, s.PrefixItemsChanges[n].GetAllChanges()...)
			}
		}
	}
	if s.NotChanges != nil {
		changes = append(changes, s.NotChanges.GetAllChanges()...)
	}
	if s.ItemsChanges != nil {
		changes = append(changes, s.ItemsChanges.GetAllChanges()...)
	}
	if s.IfChanges != nil {
		changes = append(changes, s.IfChanges.GetAllChanges()...)
	}
	if s.ElseChanges != nil {
		changes = append(changes, s.ElseChanges.GetAllChanges()...)
	}
	if s.ThenChanges != nil {
		changes = append(changes, s.ThenChanges.GetAllChanges()...)
	}
	if s.PropertyNamesChanges != nil {
		changes = append(changes, s.PropertyNamesChanges.GetAllChanges()...)
	}
	if s.ContainsChanges != nil {
		changes = append(changes, s.ContainsChanges.GetAllChanges()...)
	}
	if s.UnevaluatedItemsChanges != nil {
		changes = append(changes, s.UnevaluatedItemsChanges.GetAllChanges()...)
	}
	if s.UnevaluatedPropertiesChanges != nil {
		changes = append(changes, s.UnevaluatedPropertiesChanges.GetAllChanges()...)
	}
	if s.AdditionalPropertiesChanges != nil {
		changes = append(changes, s.AdditionalPropertiesChanges.GetAllChanges()...)
	}
	if s.SchemaPropertyChanges != nil {
		for n := range s.SchemaPropertyChanges {
			if s.SchemaPropertyChanges[n] != nil {
				changes = append(changes, s.SchemaPropertyChanges[n].GetAllChanges()...)
			}
		}
	}
	if s.DependentSchemasChanges != nil {
		for n := range s.DependentSchemasChanges {
			if s.DependentSchemasChanges[n] != nil {
				changes = append(changes, s.DependentSchemasChanges[n].GetAllChanges()...)
			}
		}
	}
	if len(s.DependentRequiredChanges) > 0 {
		changes = append(changes, s.DependentRequiredChanges...)
	}
	if s.PatternPropertiesChanges != nil {
		for n := range s.PatternPropertiesChanges {
			if s.PatternPropertiesChanges[n] != nil {
				changes = append(changes, s.PatternPropertiesChanges[n].GetAllChanges()...)
			}
		}
	}
	if s.ExternalDocChanges != nil {
		changes = append(changes, s.ExternalDocChanges.GetAllChanges()...)
	}
	if s.XMLChanges != nil {
		changes = append(changes, s.XMLChanges.GetAllChanges()...)
	}
	if s.ExtensionChanges != nil {
		changes = append(changes, s.ExtensionChanges.GetAllChanges()...)
	}
	return changes
}

// TotalChanges returns a count of the total number of changes made to this schema and all sub-schemas
func (s *SchemaChanges) TotalChanges() int {
	if s == nil {
		return 0
	}
	t := s.PropertyChanges.TotalChanges()
	if s.DiscriminatorChanges != nil {
		t += s.DiscriminatorChanges.TotalChanges()
	}
	if len(s.AllOfChanges) > 0 {
		for n := range s.AllOfChanges {
			t += s.AllOfChanges[n].TotalChanges()
		}
	}
	if len(s.AnyOfChanges) > 0 {
		for n := range s.AnyOfChanges {
			if s.AnyOfChanges[n] != nil {
				t += s.AnyOfChanges[n].TotalChanges()
			}
		}
	}
	if len(s.OneOfChanges) > 0 {
		for n := range s.OneOfChanges {
			t += s.OneOfChanges[n].TotalChanges()
		}
	}
	if len(s.PrefixItemsChanges) > 0 {
		for n := range s.PrefixItemsChanges {
			t += s.PrefixItemsChanges[n].TotalChanges()
		}
	}

	if s.NotChanges != nil {
		t += s.NotChanges.TotalChanges()
	}
	if s.ItemsChanges != nil {
		t += s.ItemsChanges.TotalChanges()
	}
	if s.IfChanges != nil {
		t += s.IfChanges.TotalChanges()
	}
	if s.ElseChanges != nil {
		t += s.ElseChanges.TotalChanges()
	}
	if s.ThenChanges != nil {
		t += s.ThenChanges.TotalChanges()
	}
	if s.PropertyNamesChanges != nil {
		t += s.PropertyNamesChanges.TotalChanges()
	}
	if s.ContainsChanges != nil {
		t += s.ContainsChanges.TotalChanges()
	}
	if s.UnevaluatedItemsChanges != nil {
		t += s.UnevaluatedItemsChanges.TotalChanges()
	}
	if s.UnevaluatedPropertiesChanges != nil {
		t += s.UnevaluatedPropertiesChanges.TotalChanges()
	}
	if s.AdditionalPropertiesChanges != nil {
		t += s.AdditionalPropertiesChanges.TotalChanges()
	}
	if s.SchemaPropertyChanges != nil {
		for n := range s.SchemaPropertyChanges {
			if s.SchemaPropertyChanges[n] != nil {
				t += s.SchemaPropertyChanges[n].TotalChanges()
			}
		}
	}
	if s.DependentSchemasChanges != nil {
		for n := range s.DependentSchemasChanges {
			t += s.DependentSchemasChanges[n].TotalChanges()
		}
	}
	if len(s.DependentRequiredChanges) > 0 {
		t += len(s.DependentRequiredChanges)
	}
	if s.PatternPropertiesChanges != nil {
		for n := range s.PatternPropertiesChanges {
			t += s.PatternPropertiesChanges[n].TotalChanges()
		}
	}
	if s.ContentSchemaChanges != nil {
		t += s.ContentSchemaChanges.TotalChanges()
	}
	if len(s.VocabularyChanges) > 0 {
		t += len(s.VocabularyChanges)
	}
	if s.ExternalDocChanges != nil {
		t += s.ExternalDocChanges.TotalChanges()
	}
	if s.XMLChanges != nil {
		t += s.XMLChanges.TotalChanges()
	}
	if s.ExtensionChanges != nil {
		t += s.ExtensionChanges.TotalChanges()
	}
	return t
}

// TotalBreakingChanges returns the total number of breaking changes made to this schema and all sub-schemas.
func (s *SchemaChanges) TotalBreakingChanges() int {
	if s == nil {
		return 0
	}
	t := s.PropertyChanges.TotalBreakingChanges()
	if s.DiscriminatorChanges != nil {
		t += s.DiscriminatorChanges.TotalBreakingChanges()
	}
	if len(s.AllOfChanges) > 0 {
		for n := range s.AllOfChanges {
			t += s.AllOfChanges[n].TotalBreakingChanges()
		}
	}
	if len(s.AllOfChanges) > 0 {
		for n := range s.AllOfChanges {
			t += s.AllOfChanges[n].TotalBreakingChanges()
		}
	}
	if len(s.AnyOfChanges) > 0 {
		for n := range s.AnyOfChanges {
			t += s.AnyOfChanges[n].TotalBreakingChanges()
		}
	}
	if len(s.OneOfChanges) > 0 {
		for n := range s.OneOfChanges {
			t += s.OneOfChanges[n].TotalBreakingChanges()
		}
	}
	if len(s.PrefixItemsChanges) > 0 {
		for n := range s.PrefixItemsChanges {
			t += s.PrefixItemsChanges[n].TotalBreakingChanges()
		}
	}
	if s.NotChanges != nil {
		t += s.NotChanges.TotalBreakingChanges()
	}
	if s.ItemsChanges != nil {
		t += s.ItemsChanges.TotalBreakingChanges()
	}
	if s.IfChanges != nil {
		t += s.IfChanges.TotalBreakingChanges()
	}
	if s.ElseChanges != nil {
		t += s.ElseChanges.TotalBreakingChanges()
	}
	if s.ThenChanges != nil {
		t += s.ThenChanges.TotalBreakingChanges()
	}
	if s.PropertyNamesChanges != nil {
		t += s.PropertyNamesChanges.TotalBreakingChanges()
	}
	if s.ContainsChanges != nil {
		t += s.ContainsChanges.TotalBreakingChanges()
	}
	if s.UnevaluatedItemsChanges != nil {
		t += s.UnevaluatedItemsChanges.TotalBreakingChanges()
	}
	if s.UnevaluatedPropertiesChanges != nil {
		t += s.UnevaluatedPropertiesChanges.TotalBreakingChanges()
	}
	if s.AdditionalPropertiesChanges != nil {
		t += s.AdditionalPropertiesChanges.TotalBreakingChanges()
	}
	if s.DependentSchemasChanges != nil {
		for n := range s.DependentSchemasChanges {
			t += s.DependentSchemasChanges[n].TotalBreakingChanges()
		}
	}
	if len(s.DependentRequiredChanges) > 0 {
		// Count breaking changes in dependent required changes
		for _, change := range s.DependentRequiredChanges {
			if change.Breaking {
				t++
			}
		}
	}
	if s.PatternPropertiesChanges != nil {
		for n := range s.PatternPropertiesChanges {
			t += s.PatternPropertiesChanges[n].TotalBreakingChanges()
		}
	}
	if s.ContentSchemaChanges != nil {
		t += s.ContentSchemaChanges.TotalBreakingChanges()
	}
	if len(s.VocabularyChanges) > 0 {
		for _, change := range s.VocabularyChanges {
			if change.Breaking {
				t++
			}
		}
	}
	if s.XMLChanges != nil {
		t += s.XMLChanges.TotalBreakingChanges()
	}
	if s.SchemaPropertyChanges != nil {
		for n := range s.SchemaPropertyChanges {
			t += s.SchemaPropertyChanges[n].TotalBreakingChanges()
		}
	}
	return t
}

// CompareSchemas accepts a left and right SchemaProxy and checks for changes. If anything is found, returns
// a pointer to SchemaChanges, otherwise returns nil
func CompareSchemas(l, r *base.SchemaProxy) *SchemaChanges {
	sc := new(SchemaChanges)
	var changes []*Change

	// Added
	if l == nil && r != nil {
		CreateChange(&changes, ObjectAdded, v3.SchemaLabel,
			nil, nil, BreakingAdded(CompSchemas, ""), nil, r)
		sc.PropertyChanges = NewPropertyChanges(changes)
	}

	// Removed
	if l != nil && r == nil {
		CreateChange(&changes, ObjectRemoved, v3.SchemaLabel,
			nil, nil, BreakingRemoved(CompSchemas, ""), l, nil)
		sc.PropertyChanges = NewPropertyChanges(changes)
	}

	if l != nil && r != nil {

		// if left proxy is a reference and right is a reference (we won't recurse into circular references here)
		if l.IsReference() && r.IsReference() {

			// points to the same schema
			if l.GetReference() == r.GetReference() {

				// check if this is a circular ref.
				if base.CheckSchemaProxyForCircularRefs(l) || base.CheckSchemaProxyForCircularRefs(r) {
					// if we have a circular reference, we can't do any more work here.
					return nil
				}

				if r.GetIndex() != nil && r.GetIndex().GetSpecAbsolutePath() == "" ||
					r.GetIndex().GetSpecAbsolutePath() == "root.yaml" {
					// local reference doesn't need following
					return nil
				}

				// continue on because the external references are the same and we need to check things going forward.

			} else {
				// references are different, that's all we care to know.
				CreateChange(&changes, Modified, v3.RefLabel,
					l.GetValueNode().Content[1], r.GetValueNode().Content[1], BreakingModified(CompSchema, PropRef), l.GetReference(),
					r.GetReference())
				sc.PropertyChanges = NewPropertyChanges(changes)

				// check if this is a circular ref.
				if base.CheckSchemaProxyForCircularRefs(l) || base.CheckSchemaProxyForCircularRefs(r) {
					// if we have a circular reference, we can't do any more work here.
					return nil
				}
				return sc
			}
		}

		// changed from inline to ref
		if !l.IsReference() && r.IsReference() {
			// check if the referenced schema matches or not
			// https://github.com/pb33f/libopenapi/issues/218
			lHash := l.Schema().Hash()
			rHash := r.Schema().Hash()
			if lHash != rHash {
				CreateChange(&changes, Modified, v3.RefLabel,
					l.GetValueNode(), r.GetValueNode().Content[1], BreakingModified(CompSchema, PropRef), l, r.GetReference())
				sc.PropertyChanges = NewPropertyChanges(changes)

				// check if this is a circular ref.
				if base.CheckSchemaProxyForCircularRefs(r) {
					// if we have a circular reference, we can't do any more work here.
					return nil
				}
				return sc
			}
		}

		// changed from ref to inline
		if l.IsReference() && !r.IsReference() {
			// check if the referenced schema matches or not
			// https://github.com/pb33f/libopenapi/issues/218
			lHash := l.Schema().Hash()
			rHash := r.Schema().Hash()
			if lHash != rHash {
				CreateChange(&changes, Modified, v3.RefLabel,
					l.GetValueNode().Content[1], r.GetValueNode(), BreakingModified(CompSchema, PropRef), l.GetReference(), r)
				sc.PropertyChanges = NewPropertyChanges(changes)

				// check if this is a circular ref.
				if base.CheckSchemaProxyForCircularRefs(l) {
					// if we have a circular reference, we can't do any more work here.
					return nil
				}
				return sc
			}
		}

		lSchema := l.Schema()
		rSchema := r.Schema()

		if low.AreEqual(lSchema, rSchema) {
			// there is no point going on, we know nothing changed!
			return nil
		}

		// check XML
		checkSchemaXML(lSchema, rSchema, &changes, sc)

		// check examples
		checkExamples(lSchema, rSchema, &changes)

		// check schema core properties for changes.
		checkSchemaPropertyChanges(lSchema, rSchema, l, r, &changes, sc)

		// now for the confusing part, there is also a schema's 'properties' property to parse.
		// inception, eat your heart out.
		var lProperties, rProperties, lDepSchemas, rDepSchemas, lPattProp, rPattProp *orderedmap.Map[low.KeyReference[string], low.ValueReference[*base.SchemaProxy]]
		var loneOf, lallOf, lanyOf, roneOf, rallOf, ranyOf, lprefix, rprefix []low.ValueReference[*base.SchemaProxy]
		if lSchema != nil {
			lProperties = lSchema.Properties.Value
			lDepSchemas = lSchema.DependentSchemas.Value
			lPattProp = lSchema.PatternProperties.Value
			loneOf = lSchema.OneOf.Value
			lallOf = lSchema.AllOf.Value
			lanyOf = lSchema.AnyOf.Value
			lprefix = lSchema.PrefixItems.Value
		}
		if rSchema != nil {
			rProperties = rSchema.Properties.Value
			rDepSchemas = rSchema.DependentSchemas.Value
			rPattProp = rSchema.PatternProperties.Value
			roneOf = rSchema.OneOf.Value
			rallOf = rSchema.AllOf.Value
			ranyOf = rSchema.AnyOf.Value
			rprefix = rSchema.PrefixItems.Value
		}

		props := checkMappedSchemaOfASchema(lProperties, rProperties, &changes)
		sc.SchemaPropertyChanges = props

		deps := checkMappedSchemaOfASchema(lDepSchemas, rDepSchemas, &changes)
		sc.DependentSchemasChanges = deps

		// Check dependent required changes
		var lDepRequired, rDepRequired *orderedmap.Map[low.KeyReference[string], low.ValueReference[[]string]]
		if lSchema != nil {
			lDepRequired = lSchema.DependentRequired.Value
		}
		if rSchema != nil {
			rDepRequired = rSchema.DependentRequired.Value
		}

		depRequiredChanges := checkDependentRequiredChanges(lDepRequired, rDepRequired)
		if len(depRequiredChanges) > 0 {
			sc.DependentRequiredChanges = depRequiredChanges
		}

		patterns := checkMappedSchemaOfASchema(lPattProp, rPattProp, &changes)
		sc.PatternPropertiesChanges = patterns

		var wg sync.WaitGroup
		wg.Add(4)
		go func() {
			extractSchemaChanges(loneOf, roneOf, v3.OneOfLabel,
				&sc.OneOfChanges, &changes)
			wg.Done()
		}()
		go func() {
			extractSchemaChanges(lallOf, rallOf, v3.AllOfLabel,
				&sc.AllOfChanges, &changes)
			wg.Done()
		}()
		go func() {
			extractSchemaChanges(lanyOf, ranyOf, v3.AnyOfLabel,
				&sc.AnyOfChanges, &changes)
			wg.Done()
		}()
		go func() {
			extractSchemaChanges(lprefix, rprefix, v3.PrefixItemsLabel,
				&sc.PrefixItemsChanges, &changes)
			wg.Done()
		}()
		wg.Wait()

	}
	// done
	if changes != nil {
		sc.PropertyChanges = NewPropertyChanges(changes)
	} else {
		sc.PropertyChanges = NewPropertyChanges(nil)
	}
	if sc.TotalChanges() > 0 {
		return sc
	}
	return nil
}

func checkSchemaXML(lSchema *base.Schema, rSchema *base.Schema, changes *[]*Change, sc *SchemaChanges) {
	// XML removed
	if lSchema == nil || rSchema == nil {
		return
	}
	if lSchema.XML.Value != nil && rSchema.XML.Value == nil {
		CreateChange(changes, ObjectRemoved, v3.XMLLabel,
			lSchema.XML.GetValueNode(), nil, BreakingRemoved(CompSchema, PropXML), lSchema.XML.GetValue(), nil)
	}
	// XML added
	if lSchema.XML.Value == nil && rSchema.XML.Value != nil {
		CreateChange(changes, ObjectAdded, v3.XMLLabel,
			nil, rSchema.XML.GetValueNode(), BreakingAdded(CompSchema, PropXML), nil, rSchema.XML.GetValue())
	}

	// compare XML
	if lSchema.XML.Value != nil && rSchema.XML.Value != nil {
		if !low.AreEqual(lSchema.XML.Value, rSchema.XML.Value) {
			sc.XMLChanges = CompareXML(lSchema.XML.Value, rSchema.XML.Value)
		}
	}
}

func checkMappedSchemaOfASchema(
	lSchema,
	rSchema *orderedmap.Map[low.KeyReference[string], low.ValueReference[*base.SchemaProxy]],
	changes *[]*Change,
) map[string]*SchemaChanges {
	var syncPropChanges sync.Map // concurrent-safe map
	var lProps []string
	lEntities := make(map[string]*base.SchemaProxy)
	lKeyNodes := make(map[string]*yaml.Node)
	var rProps []string
	rEntities := make(map[string]*base.SchemaProxy)
	rKeyNodes := make(map[string]*yaml.Node)

	for k, v := range lSchema.FromOldest() {
		lProps = append(lProps, k.Value)
		lEntities[k.Value] = v.Value
		lKeyNodes[k.Value] = k.KeyNode
	}
	for k, v := range rSchema.FromOldest() {
		rProps = append(rProps, k.Value)
		rEntities[k.Value] = v.Value
		rKeyNodes[k.Value] = k.KeyNode
	}
	sort.Strings(lProps)
	sort.Strings(rProps)
	buildProperty(lProps, rProps, lEntities, rEntities, &syncPropChanges, changes, rKeyNodes, lKeyNodes)

	// Convert the sync.Map into a regular map[string]*SchemaChanges.
	propChanges := make(map[string]*SchemaChanges)
	syncPropChanges.Range(func(key, value interface{}) bool {
		propChanges[key.(string)] = value.(*SchemaChanges)
		return true
	})
	return propChanges
}

func buildProperty(lProps, rProps []string, lEntities, rEntities map[string]*base.SchemaProxy,
	propChanges *sync.Map, changes *[]*Change, rKeyNodes, lKeyNodes map[string]*yaml.Node,
) {
	var wg sync.WaitGroup
	checkProperty := func(key string, lp, rp *base.SchemaProxy) {
		defer wg.Done()
		if low.AreEqual(lp, rp) {
			return
		}
		s := CompareSchemas(lp, rp)
		propChanges.Store(key, s)
	}

	// left and right equal.
	if len(lProps) == len(rProps) {
		for w := range lProps {
			lp := lEntities[lProps[w]]
			rp := rEntities[rProps[w]]
			if lProps[w] == rProps[w] && lp != nil && rp != nil {
				wg.Add(1)
				go checkProperty(lProps[w], lp, rp)
			}
			// Handle keys that do not match.
			if lProps[w] != rProps[w] {
				if !slices.Contains(lProps, rProps[w]) {
					// new property added.
					CreateChange(changes, ObjectAdded, v3.PropertiesLabel,
						nil, rKeyNodes[rProps[w]], BreakingAdded(CompSchema, PropProperties), nil, rEntities[rProps[w]])
				}
				if !slices.Contains(rProps, lProps[w]) {
					CreateChange(changes, ObjectRemoved, v3.PropertiesLabel,
						lKeyNodes[lProps[w]], nil, BreakingRemoved(CompSchema, PropProperties), lEntities[lProps[w]], nil)
				}
				if slices.Contains(lProps, rProps[w]) {
					h := slices.Index(lProps, rProps[w])
					lp = lEntities[lProps[h]]
					rp = rEntities[rProps[w]]
					wg.Add(1)
					go checkProperty(lProps[h], lp, rp)
				}
			}
		}
	}

	// things removed
	if len(lProps) > len(rProps) {
		for w := range lProps {
			if rEntities[lProps[w]] != nil {
				wg.Add(1)
				go checkProperty(lProps[w], lEntities[lProps[w]], rEntities[lProps[w]])
			} else {
				CreateChange(changes, ObjectRemoved, v3.PropertiesLabel,
					lKeyNodes[lProps[w]], nil, BreakingRemoved(CompSchema, PropProperties), lEntities[lProps[w]], nil)
			}
		}
		for w := range rProps {
			if lEntities[rProps[w]] != nil {
				wg.Add(1)
				go checkProperty(rProps[w], lEntities[rProps[w]], rEntities[rProps[w]])
			} else {
				CreateChange(changes, ObjectAdded, v3.PropertiesLabel,
					nil, rKeyNodes[rProps[w]], BreakingAdded(CompSchema, PropProperties), nil, rEntities[rProps[w]])
			}
		}
	}

	// stuff added
	if len(rProps) > len(lProps) {
		for _, propName := range rProps {
			if lEntities[propName] != nil {
				wg.Add(1)
				go checkProperty(propName, lEntities[propName], rEntities[propName])
			} else {
				CreateChange(changes, ObjectAdded, v3.PropertiesLabel,
					nil, rKeyNodes[propName], BreakingAdded(CompSchema, PropProperties), nil, rEntities[propName])
			}
		}
		for _, propName := range lProps {
			if rEntities[propName] != nil {
				wg.Add(1)
				go checkProperty(propName, lEntities[propName], rEntities[propName])
			} else {
				CreateChange(changes, ObjectRemoved, v3.PropertiesLabel,
					nil, lKeyNodes[propName], BreakingRemoved(CompSchema, PropProperties), lEntities[propName], nil)
			}
		}
	}

	// Wait for all property comparisons to finish.
	wg.Wait()
}

func checkSchemaPropertyChanges(
	lSchema *base.Schema,
	rSchema *base.Schema,
	lProxy *base.SchemaProxy,
	rProxy *base.SchemaProxy,
	changes *[]*Change, sc *SchemaChanges,
) {
	var props []*PropertyCheck

	// $schema (breaking change)
	var lnv, rnv *yaml.Node
	if lSchema != nil && lSchema.SchemaTypeRef.ValueNode != nil {
		lnv = lSchema.SchemaTypeRef.ValueNode
	}
	if rSchema != nil && rSchema.SchemaTypeRef.ValueNode != nil {
		rnv = rSchema.SchemaTypeRef.ValueNode
	}

	props = append(props, &PropertyCheck{
		LeftNode:  lnv,
		RightNode: rnv,
		Label:     v3.SchemaDialectLabel,
		Changes:   changes,
		Breaking:  BreakingModified(CompSchema, PropSchemaDialect),
		Component: CompSchema,
		Property:  PropSchemaDialect,
		Original:  lSchema,
		New:       rSchema,
	})
	lnv = nil
	rnv = nil

	if lSchema != nil && lSchema.ExclusiveMaximum.ValueNode != nil {
		lnv = lSchema.ExclusiveMaximum.ValueNode
	}
	if rSchema != nil && rSchema.ExclusiveMaximum.ValueNode != nil {
		rnv = rSchema.ExclusiveMaximum.ValueNode
	}
	// ExclusiveMaximum
	props = append(props, &PropertyCheck{
		LeftNode:  lnv,
		RightNode: rnv,
		Label:     v3.ExclusiveMaximumLabel,
		Changes:   changes,
		Breaking:  BreakingModified(CompSchema, PropExclusiveMaximum),
		Component: CompSchema,
		Property:  PropExclusiveMaximum,
		Original:  lSchema,
		New:       rSchema,
	})
	lnv = nil
	rnv = nil

	if lSchema != nil && lSchema.ExclusiveMinimum.ValueNode != nil {
		lnv = lSchema.ExclusiveMinimum.ValueNode
	}
	if rSchema != nil && rSchema.ExclusiveMinimum.ValueNode != nil {
		rnv = rSchema.ExclusiveMinimum.ValueNode
	}

	// ExclusiveMinimum
	props = append(props, &PropertyCheck{
		LeftNode:  lnv,
		RightNode: rnv,
		Label:     v3.ExclusiveMinimumLabel,
		Changes:   changes,
		Breaking:  BreakingModified(CompSchema, PropExclusiveMinimum),
		Component: CompSchema,
		Property:  PropExclusiveMinimum,
		Original:  lSchema,
		New:       rSchema,
	})
	lnv = nil
	rnv = nil

	if lSchema != nil && lSchema.Type.ValueNode != nil {
		lnv = lSchema.Type.ValueNode
	}
	if rSchema != nil && rSchema.Type.ValueNode != nil {
		rnv = rSchema.Type.ValueNode
	}
	// Type
	props = append(props, &PropertyCheck{
		LeftNode:  lnv,
		RightNode: rnv,
		Label:     v3.TypeLabel,
		Changes:   changes,
		Breaking:  BreakingModified(CompSchema, PropType),
		Component: CompSchema,
		Property:  PropType,
		Original:  lSchema,
		New:       rSchema,
	})
	lnv = nil
	rnv = nil

	if lSchema != nil && lSchema.Title.ValueNode != nil {
		lnv = lSchema.Title.ValueNode
	}
	if rSchema != nil && rSchema.Title.ValueNode != nil {
		rnv = rSchema.Title.ValueNode
	}
	// Title
	props = append(props, &PropertyCheck{
		LeftNode:  lnv,
		RightNode: rnv,
		Label:     v3.TitleLabel,
		Changes:   changes,
		Breaking:  BreakingModified(CompSchema, PropTitle),
		Component: CompSchema,
		Property:  PropTitle,
		Original:  lSchema,
		New:       rSchema,
	})
	lnv = nil
	rnv = nil

	if lSchema != nil && lSchema.MultipleOf.ValueNode != nil {
		lnv = lSchema.MultipleOf.ValueNode
	}
	if rSchema != nil && rSchema.MultipleOf.ValueNode != nil {
		rnv = rSchema.MultipleOf.ValueNode
	}

	// MultipleOf
	props = append(props, &PropertyCheck{
		LeftNode:  lnv,
		RightNode: rnv,
		Label:     v3.MultipleOfLabel,
		Changes:   changes,
		Breaking:  BreakingModified(CompSchema, PropMultipleOf),
		Component: CompSchema,
		Property:  PropMultipleOf,
		Original:  lSchema,
		New:       rSchema,
	})
	lnv = nil
	rnv = nil

	if lSchema != nil && lSchema.Maximum.ValueNode != nil {
		lnv = lSchema.Maximum.ValueNode
	}
	if rSchema != nil && rSchema.Maximum.ValueNode != nil {
		rnv = rSchema.Maximum.ValueNode
	}
	// Maximum
	props = append(props, &PropertyCheck{
		LeftNode:  lnv,
		RightNode: rnv,
		Label:     v3.MaximumLabel,
		Changes:   changes,
		Breaking:  BreakingModified(CompSchema, PropMaximum),
		Component: CompSchema,
		Property:  PropMaximum,
		Original:  lSchema,
		New:       rSchema,
	})
	lnv = nil
	rnv = nil

	if lSchema != nil && lSchema.Minimum.ValueNode != nil {
		lnv = lSchema.Minimum.ValueNode
	}
	if rSchema != nil && rSchema.Minimum.ValueNode != nil {
		rnv = rSchema.Minimum.ValueNode
	}
	// Minimum
	props = append(props, &PropertyCheck{
		LeftNode:  lnv,
		RightNode: rnv,
		Label:     v3.MinimumLabel,
		Changes:   changes,
		Breaking:  BreakingModified(CompSchema, PropMinimum),
		Component: CompSchema,
		Property:  PropMinimum,
		Original:  lSchema,
		New:       rSchema,
	})
	lnv = nil
	rnv = nil

	if lSchema != nil && lSchema.MaxLength.ValueNode != nil {
		lnv = lSchema.MaxLength.ValueNode
	}
	if rSchema != nil && rSchema.MaxLength.ValueNode != nil {
		rnv = rSchema.MaxLength.ValueNode
	}
	// MaxLength
	props = append(props, &PropertyCheck{
		LeftNode:  lnv,
		RightNode: rnv,
		Label:     v3.MaxLengthLabel,
		Changes:   changes,
		Breaking:  BreakingModified(CompSchema, PropMaxLength),
		Component: CompSchema,
		Property:  PropMaxLength,
		Original:  lSchema,
		New:       rSchema,
	})
	lnv = nil
	rnv = nil

	if lSchema != nil && lSchema.MinLength.ValueNode != nil {
		lnv = lSchema.MinLength.ValueNode
	}
	if rSchema != nil && rSchema.MinLength.ValueNode != nil {
		rnv = rSchema.MinLength.ValueNode
	}
	// MinLength
	props = append(props, &PropertyCheck{
		LeftNode:  lnv,
		RightNode: rnv,
		Label:     v3.MinLengthLabel,
		Changes:   changes,
		Breaking:  BreakingModified(CompSchema, PropMinLength),
		Component: CompSchema,
		Property:  PropMinLength,
		Original:  lSchema,
		New:       rSchema,
	})
	lnv = nil
	rnv = nil

	if lSchema != nil && lSchema.Pattern.ValueNode != nil {
		lnv = lSchema.Pattern.ValueNode
	}
	if rSchema != nil && rSchema.Pattern.ValueNode != nil {
		rnv = rSchema.Pattern.ValueNode
	}
	// Pattern
	props = append(props, &PropertyCheck{
		LeftNode:  lnv,
		RightNode: rnv,
		Label:     v3.PatternLabel,
		Changes:   changes,
		Breaking:  BreakingModified(CompSchema, PropPattern),
		Component: CompSchema,
		Property:  PropPattern,
		Original:  lSchema,
		New:       rSchema,
	})
	lnv = nil
	rnv = nil

	if lSchema != nil && lSchema.Format.ValueNode != nil {
		lnv = lSchema.Format.ValueNode
	}
	if rSchema != nil && rSchema.Format.ValueNode != nil {
		rnv = rSchema.Format.ValueNode
	}
	// Format
	props = append(props, &PropertyCheck{
		LeftNode:  lnv,
		RightNode: rnv,
		Label:     v3.FormatLabel,
		Changes:   changes,
		Breaking:  BreakingModified(CompSchema, PropFormat),
		Component: CompSchema,
		Property:  PropFormat,
		Original:  lSchema,
		New:       rSchema,
	})
	lnv = nil
	rnv = nil

	if lSchema != nil && lSchema.MaxItems.ValueNode != nil {
		lnv = lSchema.MaxItems.ValueNode
	}
	if rSchema != nil && rSchema.MaxItems.ValueNode != nil {
		rnv = rSchema.MaxItems.ValueNode
	}
	// MaxItems
	props = append(props, &PropertyCheck{
		LeftNode:  lnv,
		RightNode: rnv,
		Label:     v3.MaxItemsLabel,
		Changes:   changes,
		Breaking:  BreakingModified(CompSchema, PropMaxItems),
		Component: CompSchema,
		Property:  PropMaxItems,
		Original:  lSchema,
		New:       rSchema,
	})
	lnv = nil
	rnv = nil

	if lSchema != nil && lSchema.MinItems.ValueNode != nil {
		lnv = lSchema.MinItems.ValueNode
	}
	if rSchema != nil && rSchema.MinItems.ValueNode != nil {
		rnv = rSchema.MinItems.ValueNode
	}
	// MinItems
	props = append(props, &PropertyCheck{
		LeftNode:  lnv,
		RightNode: rnv,
		Label:     v3.MinItemsLabel,
		Changes:   changes,
		Breaking:  BreakingModified(CompSchema, PropMinItems),
		Component: CompSchema,
		Property:  PropMinItems,
		Original:  lSchema,
		New:       rSchema,
	})
	lnv = nil
	rnv = nil

	if lSchema != nil && lSchema.MaxProperties.ValueNode != nil {
		lnv = lSchema.MaxProperties.ValueNode
	}
	if rSchema != nil && rSchema.MaxProperties.ValueNode != nil {
		rnv = rSchema.MaxProperties.ValueNode
	}
	// MaxProperties
	props = append(props, &PropertyCheck{
		LeftNode:  lnv,
		RightNode: rnv,
		Label:     v3.MaxPropertiesLabel,
		Changes:   changes,
		Breaking:  BreakingModified(CompSchema, PropMaxProperties),
		Component: CompSchema,
		Property:  PropMaxProperties,
		Original:  lSchema,
		New:       rSchema,
	})
	lnv = nil
	rnv = nil

	if lSchema != nil && lSchema.MinProperties.ValueNode != nil {
		lnv = lSchema.MinProperties.ValueNode
	}
	if rSchema != nil && rSchema.MinProperties.ValueNode != nil {
		rnv = rSchema.MinProperties.ValueNode
	}

	// MinProperties
	props = append(props, &PropertyCheck{
		LeftNode:  lnv,
		RightNode: rnv,
		Label:     v3.MinPropertiesLabel,
		Changes:   changes,
		Breaking:  BreakingModified(CompSchema, PropMinProperties),
		Component: CompSchema,
		Property:  PropMinProperties,
		Original:  lSchema,
		New:       rSchema,
	})
	lnv = nil
	rnv = nil

	if lSchema != nil && lSchema.UniqueItems.ValueNode != nil {
		lnv = lSchema.UniqueItems.ValueNode
	}
	if rSchema != nil && rSchema.UniqueItems.ValueNode != nil {
		rnv = rSchema.UniqueItems.ValueNode
	}
	// UniqueItems
	props = append(props, &PropertyCheck{
		LeftNode:  lnv,
		RightNode: rnv,
		Label:     v3.UniqueItemsLabel,
		Changes:   changes,
		Breaking:  BreakingModified(CompSchema, PropUniqueItems),
		Component: CompSchema,
		Property:  PropUniqueItems,
		Original:  lSchema,
		New:       rSchema,
	})

	lnv = nil
	rnv = nil

	// AdditionalProperties
	if lSchema != nil && lSchema.AdditionalProperties.Value != nil && rSchema != nil && rSchema.AdditionalProperties.Value != nil {
		if lSchema.AdditionalProperties.Value.IsA() && rSchema.AdditionalProperties.Value.IsA() {
			if !low.AreEqual(lSchema.AdditionalProperties.Value.A, rSchema.AdditionalProperties.Value.A) {
				sc.AdditionalPropertiesChanges = CompareSchemas(lSchema.AdditionalProperties.Value.A, rSchema.AdditionalProperties.Value.A)
			}
		} else {
			if lSchema.AdditionalProperties.Value.IsB() && rSchema.AdditionalProperties.Value.IsB() {
				if lSchema.AdditionalProperties.Value.B != rSchema.AdditionalProperties.Value.B {
					CreateChange(changes, Modified, v3.AdditionalPropertiesLabel,
						lSchema.AdditionalProperties.ValueNode, rSchema.AdditionalProperties.ValueNode, BreakingModified(CompSchema, PropAdditionalProperties),
						lSchema.AdditionalProperties.Value.B, rSchema.AdditionalProperties.Value.B)
				}
			} else {
				CreateChange(changes, Modified, v3.AdditionalPropertiesLabel,
					lSchema.AdditionalProperties.ValueNode, rSchema.AdditionalProperties.ValueNode, BreakingModified(CompSchema, PropAdditionalProperties),
					lSchema.AdditionalProperties.Value.B, rSchema.AdditionalProperties.Value.B)
			}
		}
	}

	// added AdditionalProperties
	if (lSchema == nil || lSchema.AdditionalProperties.Value == nil) && (rSchema != nil && rSchema.AdditionalProperties.Value != nil) {
		CreateChange(changes, ObjectAdded, v3.AdditionalPropertiesLabel,
			nil, rSchema.AdditionalProperties.ValueNode, BreakingAdded(CompSchema, PropAdditionalProperties), nil, rSchema.AdditionalProperties.Value)
	}
	// removed AdditionalProperties
	if (lSchema != nil && lSchema.AdditionalProperties.Value != nil) && (rSchema == nil || rSchema.AdditionalProperties.Value == nil) {
		CreateChange(changes, ObjectRemoved, v3.AdditionalPropertiesLabel,
			lSchema.AdditionalProperties.ValueNode, nil, BreakingRemoved(CompSchema, PropAdditionalProperties), lSchema.AdditionalProperties.Value, nil)
	}

	if lSchema != nil && lSchema.Description.ValueNode != nil {
		lnv = lSchema.Description.ValueNode
	}
	if rSchema != nil && rSchema.Description.ValueNode != nil {
		rnv = rSchema.Description.ValueNode
	}
	// Description
	props = append(props, &PropertyCheck{
		LeftNode:  lnv,
		RightNode: rnv,
		Label:     v3.DescriptionLabel,
		Changes:   changes,
		Breaking:  BreakingModified(CompSchema, PropDescription),
		Component: CompSchema,
		Property:  PropDescription,
		Original:  lSchema,
		New:       rSchema,
	})
	lnv = nil
	rnv = nil

	if lSchema != nil && lSchema.ContentEncoding.ValueNode != nil {
		lnv = lSchema.ContentEncoding.ValueNode
	}
	if rSchema != nil && rSchema.ContentEncoding.ValueNode != nil {
		rnv = rSchema.ContentEncoding.ValueNode
	}
	// ContentEncoding
	props = append(props, &PropertyCheck{
		LeftNode:  lnv,
		RightNode: rnv,
		Label:     v3.ContentEncodingLabel,
		Changes:   changes,
		Breaking:  BreakingModified(CompSchema, PropContentEncoding),
		Component: CompSchema,
		Property:  PropContentEncoding,
		Original:  lSchema,
		New:       rSchema,
	})
	lnv = nil
	rnv = nil

	if lSchema != nil && lSchema.ContentMediaType.ValueNode != nil {
		lnv = lSchema.ContentMediaType.ValueNode
	}
	if rSchema != nil && rSchema.ContentMediaType.ValueNode != nil {
		rnv = rSchema.ContentMediaType.ValueNode
	}
	// ContentMediaType
	props = append(props, &PropertyCheck{
		LeftNode:  lnv,
		RightNode: rnv,
		Label:     v3.ContentMediaType,
		Changes:   changes,
		Breaking:  BreakingModified(CompSchema, PropContentMediaType),
		Component: CompSchema,
		Property:  PropContentMediaType,
		Original:  lSchema,
		New:       rSchema,
	})
	lnv = nil
	rnv = nil

	if lSchema != nil && lSchema.Default.ValueNode != nil {
		lnv = lSchema.Default.ValueNode
	}
	if rSchema != nil && rSchema.Default.ValueNode != nil {
		rnv = rSchema.Default.ValueNode
	}
	// Default
	props = append(props, &PropertyCheck{
		LeftNode:  lnv,
		RightNode: rnv,
		Label:     v3.DefaultLabel,
		Changes:   changes,
		Breaking:  BreakingModified(CompSchema, PropDefault),
		Component: CompSchema,
		Property:  PropDefault,
		Original:  lSchema,
		New:       rSchema,
	})
	lnv = nil
	rnv = nil

	if lSchema != nil && lSchema.Const.ValueNode != nil {
		lnv = lSchema.Const.ValueNode
	}
	if rSchema != nil && rSchema.Const.ValueNode != nil {
		rnv = rSchema.Const.ValueNode
	}
	// Const
	props = append(props, &PropertyCheck{
		LeftNode:  lnv,
		RightNode: rnv,
		Label:     v3.ConstLabel,
		Changes:   changes,
		Breaking:  BreakingModified(CompSchema, PropConst),
		Component: CompSchema,
		Property:  PropConst,
		Original:  lSchema,
		New:       rSchema,
	})
	lnv = nil
	rnv = nil

	if lSchema != nil && lSchema.Nullable.ValueNode != nil {
		lnv = lSchema.Nullable.ValueNode
	}
	if rSchema != nil && rSchema.Nullable.ValueNode != nil {
		rnv = rSchema.Nullable.ValueNode
	}
	// Nullable
	props = append(props, &PropertyCheck{
		LeftNode:  lnv,
		RightNode: rnv,
		Label:     v3.NullableLabel,
		Changes:   changes,
		Breaking:  BreakingModified(CompSchema, PropNullable),
		Component: CompSchema,
		Property:  PropNullable,
		Original:  lSchema,
		New:       rSchema,
	})
	lnv = nil
	rnv = nil

	if lSchema != nil && lSchema.ReadOnly.ValueNode != nil {
		lnv = lSchema.ReadOnly.ValueNode
	}
	if rSchema != nil && rSchema.ReadOnly.ValueNode != nil {
		rnv = rSchema.ReadOnly.ValueNode
	}
	// ReadOnly
	props = append(props, &PropertyCheck{
		LeftNode:  lnv,
		RightNode: rnv,
		Label:     v3.ReadOnlyLabel,
		Changes:   changes,
		Breaking:  BreakingModified(CompSchema, PropReadOnly),
		Component: CompSchema,
		Property:  PropReadOnly,
		Original:  lSchema,
		New:       rSchema,
	})
	lnv = nil
	rnv = nil

	if lSchema != nil && lSchema.WriteOnly.ValueNode != nil {
		lnv = lSchema.WriteOnly.ValueNode
	}
	if rSchema != nil && rSchema.WriteOnly.ValueNode != nil {
		rnv = rSchema.WriteOnly.ValueNode
	}
	// WriteOnly
	props = append(props, &PropertyCheck{
		LeftNode:  lnv,
		RightNode: rnv,
		Label:     v3.WriteOnlyLabel,
		Changes:   changes,
		Breaking:  BreakingModified(CompSchema, PropWriteOnly),
		Component: CompSchema,
		Property:  PropWriteOnly,
		Original:  lSchema,
		New:       rSchema,
	})
	lnv = nil
	rnv = nil

	if lSchema != nil && lSchema.Example.ValueNode != nil {
		lnv = lSchema.Example.ValueNode
	}
	if rSchema != nil && rSchema.Example.ValueNode != nil {
		rnv = rSchema.Example.ValueNode
	}
	// Example
	CheckPropertyAdditionOrRemovalWithEncoding(lnv, rnv,
		v3.ExampleLabel, changes, false, lSchema, rSchema)
	CheckForModificationWithEncoding(lnv, rnv,
		v3.ExampleLabel, changes, false, lSchema, rSchema)
	lnv = nil
	rnv = nil

	if lSchema != nil && lSchema.Deprecated.ValueNode != nil {
		lnv = lSchema.Deprecated.ValueNode
	}
	if rSchema != nil && rSchema.Deprecated.ValueNode != nil {
		rnv = rSchema.Deprecated.ValueNode
	}
	// Deprecated
	props = append(props, &PropertyCheck{
		LeftNode:  lnv,
		RightNode: rnv,
		Label:     v3.DeprecatedLabel,
		Changes:   changes,
		Breaking:  BreakingModified(CompSchema, PropDeprecated),
		Component: CompSchema,
		Property:  PropDeprecated,
		Original:  lSchema,
		New:       rSchema,
	})

	// Required
	j := make(map[string]int)
	k := make(map[string]int)
	if lSchema != nil {
		for i := range lSchema.Required.Value {
			j[lSchema.Required.Value[i].Value] = i
		}
	}
	if rSchema != nil {
		for i := range rSchema.Required.Value {
			k[rSchema.Required.Value[i].Value] = i
		}
	}
	for g := range k {
		if _, ok := j[g]; !ok {
			CreateChange(changes, PropertyAdded, v3.RequiredLabel,
				nil, rSchema.Required.Value[k[g]].GetValueNode(), BreakingAdded(CompSchema, PropRequired), nil,
				rSchema.Required.Value[k[g]].GetValue)
		}
	}
	for g := range j {
		if _, ok := k[g]; !ok {
			CreateChange(changes, PropertyRemoved, v3.RequiredLabel,
				lSchema.Required.Value[j[g]].GetValueNode(), nil, BreakingRemoved(CompSchema, PropRequired), lSchema.Required.Value[j[g]].GetValue,
				nil)
		}
	}

	// Enums
	j = make(map[string]int)
	k = make(map[string]int)
	if lSchema != nil {
		for i := range lSchema.Enum.Value {
			j[toString(lSchema.Enum.Value[i].Value.Value)] = i
		}
	}
	if rSchema != nil {
		for i := range rSchema.Enum.Value {
			k[toString(rSchema.Enum.Value[i].Value.Value)] = i
		}
	}
	for g := range k {
		if _, ok := j[g]; !ok {
			CreateChange(changes, PropertyAdded, v3.EnumLabel,
				nil, rSchema.Enum.Value[k[g]].GetValueNode(), BreakingAdded(CompSchema, PropEnum), nil,
				rSchema.Enum.Value[k[g]].GetValue)
		}
	}
	for g := range j {
		if _, ok := k[g]; !ok {
			CreateChange(changes, PropertyRemoved, v3.EnumLabel,
				lSchema.Enum.Value[j[g]].GetValueNode(), nil, BreakingRemoved(CompSchema, PropEnum), lSchema.Enum.Value[j[g]].GetValue,
				nil)
		}
	}

	// Discriminator
	if (lSchema != nil && lSchema.Discriminator.Value != nil) && (rSchema != nil && rSchema.Discriminator.Value != nil) {
		// check if hash matches, if not then compare.
		if lSchema.Discriminator.Value.Hash() != rSchema.Discriminator.Value.Hash() {
			sc.DiscriminatorChanges = CompareDiscriminator(lSchema.Discriminator.Value, rSchema.Discriminator.Value)
		}
	}
	// added Discriminator
	if (lSchema == nil || lSchema.Discriminator.Value == nil) && (rSchema != nil && rSchema.Discriminator.Value != nil) {
		CreateChange(changes, ObjectAdded, v3.DiscriminatorLabel,
			nil, rSchema.Discriminator.ValueNode, BreakingAdded(CompSchema, PropDiscriminator), nil, rSchema.Discriminator.Value)
	}
	// removed Discriminator
	if (lSchema != nil && lSchema.Discriminator.Value != nil) && (rSchema == nil || rSchema.Discriminator.Value == nil) {
		CreateChange(changes, ObjectRemoved, v3.DiscriminatorLabel,
			lSchema.Discriminator.ValueNode, nil, BreakingRemoved(CompSchema, PropDiscriminator), lSchema.Discriminator.Value, nil)
	}

	// ExternalDocs
	if (lSchema != nil && lSchema.ExternalDocs.Value != nil) && (rSchema != nil && rSchema.ExternalDocs.Value != nil) {
		// check if hash matches, if not then compare.
		if lSchema.ExternalDocs.Value.Hash() != rSchema.ExternalDocs.Value.Hash() {
			sc.ExternalDocChanges = CompareExternalDocs(lSchema.ExternalDocs.Value, rSchema.ExternalDocs.Value)
		}
	}
	// added ExternalDocs
	if (lSchema == nil || lSchema.ExternalDocs.Value == nil) && (rSchema != nil && rSchema.ExternalDocs.Value != nil) {
		CreateChange(changes, ObjectAdded, v3.ExternalDocsLabel,
			nil, rSchema.ExternalDocs.ValueNode, BreakingAdded(CompSchema, PropExternalDocs), nil, rSchema.ExternalDocs.Value)
	}
	// removed ExternalDocs
	if (lSchema != nil && lSchema.ExternalDocs.Value != nil) && (rSchema == nil || rSchema.ExternalDocs.Value == nil) {
		CreateChange(changes, ObjectRemoved, v3.ExternalDocsLabel,
			lSchema.ExternalDocs.ValueNode, nil, BreakingRemoved(CompSchema, PropExternalDocs), lSchema.ExternalDocs.Value, nil)
	}

	// 3.1 properties
	// If
	if (lSchema != nil && lSchema.If.Value != nil) && (rSchema != nil && rSchema.If.Value != nil) {
		if !low.AreEqual(lSchema.If.Value, rSchema.If.Value) {
			sc.IfChanges = CompareSchemas(lSchema.If.Value, rSchema.If.Value)
		}
	}
	// added If
	if (lSchema == nil || lSchema.If.Value == nil) && (rSchema != nil && rSchema.If.Value != nil) {
		CreateChange(changes, ObjectAdded, v3.IfLabel,
			nil, rSchema.If.ValueNode, BreakingAdded(CompSchema, PropIf), nil, rSchema.If.Value)
	}
	// removed If
	if (lSchema != nil && lSchema.If.Value != nil) && (rSchema == nil || rSchema.If.Value == nil) {
		CreateChange(changes, ObjectRemoved, v3.IfLabel,
			lSchema.If.ValueNode, nil, BreakingRemoved(CompSchema, PropIf), lSchema.If.Value, nil)
	}
	// Else
	if (lSchema != nil && lSchema.Else.Value != nil) && (rSchema == nil || rSchema.Else.Value != nil) {
		if !low.AreEqual(lSchema.Else.Value, rSchema.Else.Value) {
			sc.ElseChanges = CompareSchemas(lSchema.Else.Value, rSchema.Else.Value)
		}
	}
	// added Else
	if (lSchema == nil || lSchema.Else.Value == nil) && (rSchema != nil && rSchema.Else.Value != nil) {
		CreateChange(changes, ObjectAdded, v3.ElseLabel,
			nil, rSchema.Else.ValueNode, BreakingAdded(CompSchema, PropElse), nil, rSchema.Else.Value)
	}
	// removed Else
	if (lSchema != nil && lSchema.Else.Value != nil) && (rSchema == nil || rSchema.Else.Value == nil) {
		CreateChange(changes, ObjectRemoved, v3.ElseLabel,
			lSchema.Else.ValueNode, nil, BreakingRemoved(CompSchema, PropElse), lSchema.Else.Value, nil)
	}
	// Then
	if (lSchema != nil && lSchema.Then.Value != nil) && (rSchema != nil && rSchema.Then.Value != nil) {
		if !low.AreEqual(lSchema.Then.Value, rSchema.Then.Value) {
			sc.ThenChanges = CompareSchemas(lSchema.Then.Value, rSchema.Then.Value)
		}
	}
	// added Then
	if (lSchema == nil || lSchema.Then.Value == nil) && (rSchema != nil && rSchema.Then.Value != nil) {
		CreateChange(changes, ObjectAdded, v3.ThenLabel,
			nil, rSchema.Then.ValueNode, BreakingAdded(CompSchema, PropThen), nil, rSchema.Then.Value)
	}
	// removed Then
	if (lSchema != nil && lSchema.Then.Value != nil) && (rSchema == nil || rSchema.Then.Value == nil) {
		CreateChange(changes, ObjectRemoved, v3.ThenLabel,
			lSchema.Then.ValueNode, nil, BreakingRemoved(CompSchema, PropThen), lSchema.Then.Value, nil)
	}
	// PropertyNames
	if (lSchema != nil && lSchema.PropertyNames.Value != nil) && (rSchema != nil && rSchema.PropertyNames.Value != nil) {
		if !low.AreEqual(lSchema.PropertyNames.Value, rSchema.PropertyNames.Value) {
			sc.PropertyNamesChanges = CompareSchemas(lSchema.PropertyNames.Value, rSchema.PropertyNames.Value)
		}
	}
	// added PropertyNames
	if (lSchema == nil || lSchema.PropertyNames.Value == nil) && (rSchema != nil && rSchema.PropertyNames.Value != nil) {
		CreateChange(changes, ObjectAdded, v3.PropertyNamesLabel,
			nil, rSchema.PropertyNames.ValueNode, BreakingAdded(CompSchema, PropPropertyNames), nil, rSchema.PropertyNames.Value)
	}
	// removed PropertyNames
	if (lSchema != nil && lSchema.PropertyNames.Value != nil) && (rSchema == nil || rSchema.PropertyNames.Value == nil) {
		CreateChange(changes, ObjectRemoved, v3.PropertyNamesLabel,
			lSchema.PropertyNames.ValueNode, nil, BreakingRemoved(CompSchema, PropPropertyNames), lSchema.PropertyNames.Value, nil)
	}
	// Contains
	if (lSchema != nil && lSchema.Contains.Value != nil) && (rSchema != nil && rSchema.Contains.Value != nil) {
		if !low.AreEqual(lSchema.Contains.Value, rSchema.Contains.Value) {
			sc.ContainsChanges = CompareSchemas(lSchema.Contains.Value, rSchema.Contains.Value)
		}
	}
	// added Contains
	if (lSchema == nil || lSchema.Contains.Value == nil) && (rSchema != nil && rSchema.Contains.Value != nil) {
		CreateChange(changes, ObjectAdded, v3.ContainsLabel,
			nil, rSchema.Contains.ValueNode, BreakingAdded(CompSchema, PropContains), nil, rSchema.Contains.Value)
	}
	// removed Contains
	if (lSchema != nil && lSchema.Contains.Value != nil) && (rSchema == nil || rSchema.Contains.Value == nil) {
		CreateChange(changes, ObjectRemoved, v3.ContainsLabel,
			lSchema.Contains.ValueNode, nil, BreakingRemoved(CompSchema, PropContains), lSchema.Contains.Value, nil)
	}
	// UnevaluatedItems
	if (lSchema != nil && lSchema.UnevaluatedItems.Value != nil) && (rSchema != nil && rSchema.UnevaluatedItems.Value != nil) {
		if !low.AreEqual(lSchema.UnevaluatedItems.Value, rSchema.UnevaluatedItems.Value) {
			sc.UnevaluatedItemsChanges = CompareSchemas(lSchema.UnevaluatedItems.Value, rSchema.UnevaluatedItems.Value)
		}
	}
	// added UnevaluatedItems
	if (lSchema == nil || lSchema.UnevaluatedItems.Value == nil) && (rSchema != nil && rSchema.UnevaluatedItems.Value != nil) {
		CreateChange(changes, ObjectAdded, v3.UnevaluatedItemsLabel,
			nil, rSchema.UnevaluatedItems.ValueNode, BreakingAdded(CompSchema, PropUnevaluatedItems), nil, rSchema.UnevaluatedItems.Value)
	}
	// removed UnevaluatedItems
	if (lSchema != nil && lSchema.UnevaluatedItems.Value != nil) && (rSchema == nil || rSchema.UnevaluatedItems.Value == nil) {
		CreateChange(changes, ObjectRemoved, v3.UnevaluatedItemsLabel,
			lSchema.UnevaluatedItems.ValueNode, nil, BreakingRemoved(CompSchema, PropUnevaluatedItems), lSchema.UnevaluatedItems.Value, nil)
	}

	// UnevaluatedProperties
	if (lSchema != nil && lSchema.UnevaluatedProperties.Value != nil) && (rSchema != nil && rSchema.UnevaluatedProperties.Value != nil) {
		if lSchema.UnevaluatedProperties.Value.IsA() && rSchema.UnevaluatedProperties.Value.IsA() {
			if !low.AreEqual(lSchema.UnevaluatedProperties.Value.A, rSchema.UnevaluatedProperties.Value.A) {
				sc.UnevaluatedPropertiesChanges = CompareSchemas(lSchema.UnevaluatedProperties.Value.A, rSchema.UnevaluatedProperties.Value.A)
			}
		} else {
			if lSchema.UnevaluatedProperties.Value.IsB() && rSchema.UnevaluatedProperties.Value.IsB() {
				if lSchema.UnevaluatedProperties.Value.B != rSchema.UnevaluatedProperties.Value.B {
					CreateChange(changes, Modified, v3.UnevaluatedPropertiesLabel,
						lSchema.UnevaluatedProperties.ValueNode, rSchema.UnevaluatedProperties.ValueNode, BreakingModified(CompSchema, PropUnevaluatedProps),
						lSchema.UnevaluatedProperties.Value.B, rSchema.UnevaluatedProperties.Value.B)
				}
			} else {
				CreateChange(changes, Modified, v3.UnevaluatedPropertiesLabel,
					lSchema.UnevaluatedProperties.ValueNode, rSchema.UnevaluatedProperties.ValueNode, BreakingModified(CompSchema, PropUnevaluatedProps),
					lSchema.UnevaluatedProperties.Value.B, rSchema.UnevaluatedProperties.Value.B)
			}
		}
	}

	// added UnevaluatedProperties
	if (lSchema == nil || lSchema.UnevaluatedProperties.Value == nil) && (rSchema != nil && rSchema.UnevaluatedProperties.Value != nil) {
		CreateChange(changes, ObjectAdded, v3.UnevaluatedPropertiesLabel,
			nil, rSchema.UnevaluatedProperties.ValueNode, BreakingAdded(CompSchema, PropUnevaluatedProps), nil, rSchema.UnevaluatedProperties.Value)
	}
	// removed UnevaluatedProperties
	if (lSchema != nil && lSchema.UnevaluatedProperties.Value != nil) && (rSchema == nil || rSchema.UnevaluatedProperties.Value == nil) {
		CreateChange(changes, ObjectRemoved, v3.UnevaluatedPropertiesLabel,
			lSchema.UnevaluatedProperties.ValueNode, nil, BreakingRemoved(CompSchema, PropUnevaluatedProps), lSchema.UnevaluatedProperties.Value, nil)
	}

	// Not
	if (lSchema != nil && lSchema.Not.Value != nil) && (rSchema != nil && rSchema.Not.Value != nil) {
		if !low.AreEqual(lSchema.Not.Value, rSchema.Not.Value) {
			sc.NotChanges = CompareSchemas(lSchema.Not.Value, rSchema.Not.Value)
		}
	}
	// added Not
	if (lSchema == nil || lSchema.Not.Value == nil) && (rSchema != nil && rSchema.Not.Value != nil) {
		CreateChange(changes, ObjectAdded, v3.NotLabel,
			nil, rSchema.Not.ValueNode, BreakingAdded(CompSchema, PropNot), nil, rSchema.Not.Value)
	}
	// removed not
	if (lSchema != nil && lSchema.Not.Value != nil) && (rSchema == nil || rSchema.Not.Value == nil) {
		CreateChange(changes, ObjectRemoved, v3.NotLabel,
			lSchema.Not.ValueNode, nil, BreakingRemoved(CompSchema, PropNot), lSchema.Not.Value, nil)
	}

	// items
	if (lSchema != nil && lSchema.Items.Value != nil) && (rSchema != nil && rSchema.Items.Value != nil) {
		if lSchema.Items.Value.IsA() && rSchema.Items.Value.IsA() {
			if !low.AreEqual(lSchema.Items.Value.A, rSchema.Items.Value.A) {
				sc.ItemsChanges = CompareSchemas(lSchema.Items.Value.A, rSchema.Items.Value.A)
			}
		} else {
			CreateChange(changes, Modified, v3.ItemsLabel,
				lSchema.Items.ValueNode, rSchema.Items.ValueNode, BreakingModified(CompSchema, PropItems), lSchema.Items.Value.B, rSchema.Items.Value.B)
		}
	}
	// added Items
	if (lSchema == nil || lSchema.Items.Value == nil) && (rSchema != nil && rSchema.Items.Value != nil) {
		CreateChange(changes, ObjectAdded, v3.ItemsLabel,
			nil, rSchema.Items.ValueNode, BreakingAdded(CompSchema, PropItems), nil, rSchema.Items.Value)
	}
	// removed Items
	if (lSchema != nil && lSchema.Items.Value != nil) && (rSchema == nil || rSchema.Items.Value == nil) {
		CreateChange(changes, ObjectRemoved, v3.ItemsLabel,
			lSchema.Items.ValueNode, nil, BreakingRemoved(CompSchema, PropItems), lSchema.Items.Value, nil)
	}

	// $dynamicAnchor (JSON Schema 2020-12)
	lnv = nil
	rnv = nil
	if lSchema != nil && lSchema.DynamicAnchor.ValueNode != nil {
		lnv = lSchema.DynamicAnchor.ValueNode
	}
	if rSchema != nil && rSchema.DynamicAnchor.ValueNode != nil {
		rnv = rSchema.DynamicAnchor.ValueNode
	}
	props = append(props, &PropertyCheck{
		LeftNode:  lnv,
		RightNode: rnv,
		Label:     v3.DynamicAnchorLabel,
		Changes:   changes,
		Breaking:  BreakingModified(CompSchema, PropDynamicAnchor),
		Component: CompSchema,
		Property:  PropDynamicAnchor,
		Original:  lSchema,
		New:       rSchema,
	})
	lnv = nil
	rnv = nil

	// $dynamicRef (JSON Schema 2020-12)
	if lSchema != nil && lSchema.DynamicRef.ValueNode != nil {
		lnv = lSchema.DynamicRef.ValueNode
	}
	if rSchema != nil && rSchema.DynamicRef.ValueNode != nil {
		rnv = rSchema.DynamicRef.ValueNode
	}
	props = append(props, &PropertyCheck{
		LeftNode:  lnv,
		RightNode: rnv,
		Label:     v3.DynamicRefLabel,
		Changes:   changes,
		Breaking:  BreakingModified(CompSchema, PropDynamicRef),
		Component: CompSchema,
		Property:  PropDynamicRef,
		Original:  lSchema,
		New:       rSchema,
	})
	lnv = nil
	rnv = nil

	// $id (JSON Schema 2020-12)
	if lSchema != nil && lSchema.Id.ValueNode != nil {
		lnv = lSchema.Id.ValueNode
	}
	if rSchema != nil && rSchema.Id.ValueNode != nil {
		rnv = rSchema.Id.ValueNode
	}
	props = append(props, &PropertyCheck{
		LeftNode:  lnv,
		RightNode: rnv,
		Label:     base.IdLabel,
		Changes:   changes,
		Breaking:  BreakingModified(CompSchema, PropId),
		Component: CompSchema,
		Property:  PropId,
		Original:  lSchema,
		New:       rSchema,
	})
	lnv = nil
	rnv = nil

	// $comment (JSON Schema 2020-12)
	if lSchema != nil && lSchema.Comment.ValueNode != nil {
		lnv = lSchema.Comment.ValueNode
	}
	if rSchema != nil && rSchema.Comment.ValueNode != nil {
		rnv = rSchema.Comment.ValueNode
	}
	props = append(props, &PropertyCheck{
		LeftNode:  lnv,
		RightNode: rnv,
		Label:     base.CommentLabel,
		Changes:   changes,
		Breaking:  BreakingModified(CompSchema, PropComment),
		Component: CompSchema,
		Property:  PropComment,
		Original:  lSchema,
		New:       rSchema,
	})
	lnv = nil
	rnv = nil

	// contentSchema (JSON Schema 2020-12) - recursive schema comparison
	if lSchema != nil && !lSchema.ContentSchema.IsEmpty() && rSchema != nil && !rSchema.ContentSchema.IsEmpty() {
		sc.ContentSchemaChanges = CompareSchemas(lSchema.ContentSchema.Value, rSchema.ContentSchema.Value)
	}
	if lSchema != nil && !lSchema.ContentSchema.IsEmpty() && (rSchema == nil || rSchema.ContentSchema.IsEmpty()) {
		CreateChange(changes, PropertyRemoved, base.ContentSchemaLabel,
			lSchema.ContentSchema.ValueNode, nil,
			BreakingRemoved(CompSchema, PropContentSchema),
			lSchema.ContentSchema.Value, nil)
	}
	if (lSchema == nil || lSchema.ContentSchema.IsEmpty()) && rSchema != nil && !rSchema.ContentSchema.IsEmpty() {
		CreateChange(changes, PropertyAdded, base.ContentSchemaLabel,
			nil, rSchema.ContentSchema.ValueNode,
			BreakingAdded(CompSchema, PropContentSchema),
			nil, rSchema.ContentSchema.Value)
	}

	// $vocabulary (JSON Schema 2020-12) - map comparison
	// note: vocabulary changes are stored in VocabularyChanges and counted separately
	// in TotalChanges(), so they should NOT be appended to the main changes slice
	var lVocab, rVocab *orderedmap.Map[low.KeyReference[string], low.ValueReference[bool]]
	if lSchema != nil {
		lVocab = lSchema.Vocabulary.Value
	}
	if rSchema != nil {
		rVocab = rSchema.Vocabulary.Value
	}
	if lVocab != nil || rVocab != nil {
		sc.VocabularyChanges = checkVocabularyChanges(lVocab, rVocab)
	}

	// check extensions
	var lext *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]]
	var rext *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]]
	if lSchema != nil {
		lext = lSchema.Extensions
	}
	if rSchema != nil {
		rext = rSchema.Extensions
	}
	if lext != nil && rext != nil {
		sc.ExtensionChanges = CompareExtensions(lext, rext)
	}

	// check core properties
	CheckProperties(props)

	// Post-process: Update context line numbers for Type changes to use schema KeyNode for better context
	// This provides line where "schema:" is defined, not "type: value"
	if changes != nil && len(*changes) > 0 {
		for _, change := range *changes {
			if change.Property == v3.TypeLabel && change.Context != nil {
				if lProxy != nil && lProxy.GetKeyNode() != nil {
					line := lProxy.GetKeyNode().Line
					col := lProxy.GetKeyNode().Column
					change.Context.OriginalLine = &line
					change.Context.OriginalColumn = &col
				}
				if rProxy != nil && rProxy.GetKeyNode() != nil {
					line := rProxy.GetKeyNode().Line
					col := rProxy.GetKeyNode().Column
					change.Context.NewLine = &line
					change.Context.NewColumn = &col
				}
				break // found the type change, no need to continue
			}
		}
	}
}

func checkExamples(lSchema *base.Schema, rSchema *base.Schema, changes *[]*Change) {
	if lSchema == nil && rSchema == nil {
		return
	}

	// check examples (3.1+)
	var lExampKey, rExampKey []string
	lExampN := make(map[string]*yaml.Node)
	rExampN := make(map[string]*yaml.Node)
	lExampVal := make(map[string]any)
	rExampVal := make(map[string]any)

	// create keys by hashing values
	if lSchema != nil && lSchema.Examples.ValueNode != nil {
		for i := range lSchema.Examples.ValueNode.Content {
			key := low.GenerateHashString(lSchema.Examples.ValueNode.Content[i].Value)
			lExampKey = append(lExampKey, key)
			lExampVal[key] = lSchema.Examples.ValueNode.Content[i].Value
			lExampN[key] = lSchema.Examples.ValueNode.Content[i]

		}
	}
	if rSchema != nil && rSchema.Examples.ValueNode != nil {
		for i := range rSchema.Examples.ValueNode.Content {
			key := low.GenerateHashString(rSchema.Examples.ValueNode.Content[i].Value)
			rExampKey = append(rExampKey, key)
			rExampVal[key] = rSchema.Examples.ValueNode.Content[i].Value
			rExampN[key] = rSchema.Examples.ValueNode.Content[i]
		}
	}

	// if examples equal lengths, check for equality
	if len(lExampKey) == len(rExampKey) {
		for i := range lExampKey {
			if lExampKey[i] != rExampKey[i] {
				CreateChangeWithEncoding(changes, Modified, v3.ExamplesLabel,
					lExampN[lExampKey[i]], rExampN[rExampKey[i]], BreakingModified(CompSchema, PropExamples),
					lExampVal[lExampKey[i]], rExampVal[rExampKey[i]])
			}
		}
	}
	// examples were removed.
	if len(lExampKey) > len(rExampKey) {
		for i := range lExampKey {
			if i < len(rExampKey) && lExampKey[i] != rExampKey[i] {
				CreateChangeWithEncoding(changes, Modified, v3.ExamplesLabel,
					lExampN[lExampKey[i]], rExampN[rExampKey[i]], BreakingModified(CompSchema, PropExamples),
					lExampVal[lExampKey[i]], rExampVal[rExampKey[i]])
			}
			if i >= len(rExampKey) {
				CreateChangeWithEncoding(changes, ObjectRemoved, v3.ExamplesLabel,
					lExampN[lExampKey[i]], nil, BreakingRemoved(CompSchema, PropExamples),
					lExampVal[lExampKey[i]], nil)
			}
		}
	}

	// examples were added
	if len(lExampKey) < len(rExampKey) {
		for i := range rExampKey {
			if i < len(lExampKey) && lExampKey[i] != rExampKey[i] {
				CreateChangeWithEncoding(changes, Modified, v3.ExamplesLabel,
					lExampN[lExampKey[i]], rExampN[rExampKey[i]], BreakingModified(CompSchema, PropExamples),
					lExampVal[lExampKey[i]], rExampVal[rExampKey[i]])
			}
			if i >= len(lExampKey) {
				CreateChangeWithEncoding(changes, ObjectAdded, v3.ExamplesLabel,
					nil, rExampN[rExampKey[i]], BreakingAdded(CompSchema, PropExamples),
					nil, rExampVal[rExampKey[i]])
			}
		}
	}
}

func extractSchemaChanges(
	lSchema []low.ValueReference[*base.SchemaProxy],
	rSchema []low.ValueReference[*base.SchemaProxy],
	label string,
	sc *[]*SchemaChanges,
	changes *[]*Change,
) {
	// if there is nothing here, there is nothing to do.
	if lSchema == nil && rSchema == nil {
		return
	}

	x := "%x"
	// create hash key maps to check equality
	lKeys := make([]string, 0, len(lSchema))
	rKeys := make([]string, 0, len(rSchema))
	lEntities := make(map[string]*base.SchemaProxy)
	rEntities := make(map[string]*base.SchemaProxy)

	for h := range lSchema {
		q := lSchema[h].Value
		z := fmt.Sprintf(x, q.Hash())
		lKeys = append(lKeys, z)
		lEntities[z] = q
	}
	for h := range rSchema {
		q := rSchema[h].Value
		z := fmt.Sprintf(x, q.Hash())
		rKeys = append(rKeys, z)
		rEntities[z] = q
	}

	// check for identical lengths
	if len(lKeys) == len(rKeys) {
		for w := range lKeys {
			// keys are different, which means there are changes.
			if lKeys[w] != rKeys[w] {
				*sc = append(*sc, CompareSchemas(lEntities[lKeys[w]], rEntities[rKeys[w]]))
			}
		}
	}

	// things were removed
	if len(lKeys) > len(rKeys) {
		for w := range lKeys {
			if w < len(rKeys) && lKeys[w] != rKeys[w] {
				*sc = append(*sc, CompareSchemas(lEntities[lKeys[w]], rEntities[rKeys[w]]))
			}
			if w >= len(rKeys) {
				// determine breaking status based on label
				breaking := true
				switch label {
				case v3.AllOfLabel:
					breaking = BreakingRemoved(CompSchema, PropAllOf)
				case v3.AnyOfLabel:
					breaking = BreakingRemoved(CompSchema, PropAnyOf)
				case v3.OneOfLabel:
					breaking = BreakingRemoved(CompSchema, PropOneOf)
				case v3.PrefixItemsLabel:
					breaking = BreakingRemoved(CompSchema, PropPrefixItems)
				}
				CreateChange(changes, ObjectRemoved, label,
					lEntities[lKeys[w]].GetValueNode(), nil, breaking, lEntities[lKeys[w]], nil)
			}
		}
	}

	// things were added
	if len(rKeys) > len(lKeys) {
		for w := range rKeys {
			if w < len(lKeys) && rKeys[w] != lKeys[w] {
				*sc = append(*sc, CompareSchemas(lEntities[lKeys[w]], rEntities[rKeys[w]]))
			}
			if w >= len(lKeys) {
				// determine breaking status based on label
				breaking := false
				switch label {
				case v3.AllOfLabel:
					breaking = BreakingAdded(CompSchema, PropAllOf)
				case v3.AnyOfLabel:
					breaking = BreakingAdded(CompSchema, PropAnyOf)
				case v3.OneOfLabel:
					breaking = BreakingAdded(CompSchema, PropOneOf)
				case v3.PrefixItemsLabel:
					breaking = BreakingAdded(CompSchema, PropPrefixItems)
				}
				CreateChange(changes, ObjectAdded, label,
					nil, rEntities[rKeys[w]].GetValueNode(), breaking, nil, rEntities[rKeys[w]])
			}
		}
	}
}

// checkDependentRequiredChanges compares two DependentRequired maps and returns any changes found
func checkDependentRequiredChanges(
	left, right *orderedmap.Map[low.KeyReference[string], low.ValueReference[[]string]],
) []*Change {
	// If both are nil, no changes
	if left == nil && right == nil {
		return nil
	}

	var changes []*Change

	leftMap := make(map[string][]string)
	rightMap := make(map[string][]string)

	// Build left map
	if left != nil {
		for prop, reqArray := range left.FromOldest() {
			leftMap[prop.Value] = reqArray.Value
		}
	}

	// Build right map
	if right != nil {
		for prop, reqArray := range right.FromOldest() {
			rightMap[prop.Value] = reqArray.Value
		}
	}

	// Check for property additions and modifications
	for prop, rightReqs := range rightMap {
		if leftReqs, exists := leftMap[prop]; exists {
			// Property exists in both, check if requirements changed
			if !slicesEqual(leftReqs, rightReqs) {
				CreateChange(&changes, Modified, prop,
					getNodeForProperty(left, prop), getNodeForProperty(right, prop),
					BreakingModified(CompSchema, PropDependentRequired), leftReqs, rightReqs)
			}
		} else {
			// Property added
			CreateChange(&changes, PropertyAdded, prop,
				nil, getNodeForProperty(right, prop),
				BreakingAdded(CompSchema, PropDependentRequired), nil, rightReqs)
		}
	}

	// Check for property removals
	for prop, leftReqs := range leftMap {
		if _, exists := rightMap[prop]; !exists {
			CreateChange(&changes, PropertyRemoved, prop,
				getNodeForProperty(left, prop), nil,
				BreakingRemoved(CompSchema, PropDependentRequired), leftReqs, nil)
		}
	}

	return changes
}

// slicesEqual compares two string slices for equality (order matters)
func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i, v := range a {
		if b[i] != v {
			return false
		}
	}
	return true
}

// getNodeForProperty gets the YAML node for a specific property in a DependentRequired map
func getNodeForProperty(depMap *orderedmap.Map[low.KeyReference[string], low.ValueReference[[]string]], prop string) *yaml.Node {
	if depMap == nil {
		return nil
	}
	for key, value := range depMap.FromOldest() {
		if key.Value == prop {
			return value.ValueNode
		}
	}
	return nil
}

// checkVocabularyChanges compares $vocabulary maps and returns a list of changes.
// the caller is responsible for appending the returned changes to their main changes slice.
func checkVocabularyChanges(lVocab, rVocab *orderedmap.Map[low.KeyReference[string], low.ValueReference[bool]]) []*Change {
	if lVocab == nil && rVocab == nil {
		return nil
	}

	// pre-allocate maps with size hints for better memory efficiency
	lSize := orderedmap.Len(lVocab)
	rSize := orderedmap.Len(rVocab)

	lVocabMap := make(map[string]bool, lSize)
	lVocabNodes := make(map[string]*yaml.Node, lSize)
	rVocabMap := make(map[string]bool, rSize)
	rVocabNodes := make(map[string]*yaml.Node, rSize)

	if lVocab != nil {
		for k, v := range lVocab.FromOldest() {
			lVocabMap[k.Value] = v.Value
			lVocabNodes[k.Value] = v.ValueNode
		}
	}
	if rVocab != nil {
		for k, v := range rVocab.FromOldest() {
			rVocabMap[k.Value] = v.Value
			rVocabNodes[k.Value] = v.ValueNode
		}
	}

	// pre-allocate result slice with reasonable capacity
	var vocabChanges []*Change

	// check for removed or modified vocabularies
	for uri, lVal := range lVocabMap {
		if rVal, ok := rVocabMap[uri]; ok {
			// vocabulary exists in both - check if value changed
			if lVal != rVal {
				c := &Change{
					Property:       base.VocabularyLabel,
					ChangeType:     Modified,
					Original:       fmt.Sprintf("%s=%v", uri, lVal),
					New:            fmt.Sprintf("%s=%v", uri, rVal),
					Breaking:       BreakingModified(CompSchema, PropVocabulary),
					OriginalObject: lVocabMap,
					NewObject:      rVocabMap,
				}
				if lVocabNodes[uri] != nil {
					c.Context = CreateContext(lVocabNodes[uri], rVocabNodes[uri])
				}
				vocabChanges = append(vocabChanges, c)
			}
		} else {
			// vocabulary was removed
			c := &Change{
				Property:       base.VocabularyLabel,
				ChangeType:     PropertyRemoved,
				Original:       uri,
				Breaking:       BreakingRemoved(CompSchema, PropVocabulary),
				OriginalObject: lVocabMap,
			}
			if lVocabNodes[uri] != nil {
				c.Context = CreateContext(lVocabNodes[uri], nil)
			}
			vocabChanges = append(vocabChanges, c)
		}
	}

	// check for added vocabularies
	for uri := range rVocabMap {
		if _, ok := lVocabMap[uri]; !ok {
			// vocabulary was added
			c := &Change{
				Property:   base.VocabularyLabel,
				ChangeType: PropertyAdded,
				New:        uri,
				Breaking:   BreakingAdded(CompSchema, PropVocabulary),
				NewObject:  rVocabMap,
			}
			if rVocabNodes[uri] != nil {
				c.Context = CreateContext(nil, rVocabNodes[uri])
			}
			vocabChanges = append(vocabChanges, c)
		}
	}

	return vocabChanges
}
