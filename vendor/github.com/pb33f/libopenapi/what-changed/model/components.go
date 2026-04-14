// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package model

import (
	"reflect"

	"github.com/pb33f/libopenapi/datamodel/low"
	"github.com/pb33f/libopenapi/datamodel/low/base"
	v2 "github.com/pb33f/libopenapi/datamodel/low/v2"
	v3 "github.com/pb33f/libopenapi/datamodel/low/v3"
	"github.com/pb33f/libopenapi/orderedmap"
)

// ComponentsChanges represents changes made to both OpenAPI and Swagger documents. This model is based on OpenAPI 3
// components, however it's also used to contain Swagger definitions changes. Swagger for some reason decided to not
// contain definitions inside a single parent like Components, and instead scattered them across the root of the
// Swagger document, giving everything a `Definitions` postfix. This design attempts to unify those models into
// a single entity that contains all changes.
//
// Schemas are treated differently from every other component / definition in this library. Schemas can be highly
// recursive, and are not resolved by the model, every ref is recorded, but it's not looked at essentially. This means
// that when what-changed performs a check, everything that is *not* a schema is checked *inline*, Those references are
// resolved in place and a change is recorded in place. Schemas however are *not* resolved. which means no change
// will be recorded in place for any object referencing it.
//
// That is why there is a separate SchemaChanges object in ComponentsChanges. Schemas are checked at the source, and
// not inline when referenced. A schema change will only be found once, however a change to ANY other definition or
// component, will be found inline (and will duplicate for every use).
//
// The other oddity here is SecuritySchemes. For some reason OpenAPI does not use a $ref for these entities, it
// uses a name lookup, which means there are no direct links between any model and a security scheme reference.
// So like Schemas, SecuritySchemes are treated differently and handled individually.
//
// An important note: Everything EXCEPT Schemas and SecuritySchemes is ONLY checked for additions or removals.
// modifications are not checked, these checks occur in-place by implementing objects as they are autp-resolved
// when the model is built.
type ComponentsChanges struct {
	*PropertyChanges
	SchemaChanges         map[string]*SchemaChanges         `json:"schemas,omitempty" yaml:"schemas,omitempty"`
	SecuritySchemeChanges map[string]*SecuritySchemeChanges `json:"securitySchemes,omitempty" yaml:"securitySchemes,omitempty"`
	ExtensionChanges      *ExtensionChanges                 `json:"extensions,omitempty" yaml:"extensions,omitempty"`
}

// CompareComponents will compare OpenAPI components for any changes. Accepts Swagger Definition objects
// like ParameterDefinitions or Definitions etc.
func CompareComponents(l, r any) *ComponentsChanges {
	var changes []*Change

	cc := new(ComponentsChanges)

	// Swagger Parameters
	if reflect.TypeOf(&v2.ParameterDefinitions{}) == reflect.TypeOf(l) &&
		reflect.TypeOf(&v2.ParameterDefinitions{}) == reflect.TypeOf(r) {
		lDef := l.(*v2.ParameterDefinitions)
		rDef := r.(*v2.ParameterDefinitions)
		var a, b *orderedmap.Map[low.KeyReference[string], low.ValueReference[*v2.Parameter]]
		if lDef != nil {
			a = lDef.Definitions
		}
		if rDef != nil {
			b = rDef.Definitions
		}
		CheckMapForAdditionRemoval(a, b, &changes, v3.ParametersLabel)
	}

	// Swagger Responses
	if reflect.TypeOf(&v2.ResponsesDefinitions{}) == reflect.TypeOf(l) &&
		reflect.TypeOf(&v2.ResponsesDefinitions{}) == reflect.TypeOf(r) {
		lDef := l.(*v2.ResponsesDefinitions)
		rDef := r.(*v2.ResponsesDefinitions)
		var a, b *orderedmap.Map[low.KeyReference[string], low.ValueReference[*v2.Response]]
		if lDef != nil {
			a = lDef.Definitions
		}
		if rDef != nil {
			b = rDef.Definitions
		}
		CheckMapForAdditionRemoval(a, b, &changes, v3.ResponsesLabel)
	}

	// Swagger Schemas
	if reflect.TypeOf(&v2.Definitions{}) == reflect.TypeOf(l) &&
		reflect.TypeOf(&v2.Definitions{}) == reflect.TypeOf(r) {
		lDef := l.(*v2.Definitions)
		rDef := r.(*v2.Definitions)
		var a, b *orderedmap.Map[low.KeyReference[string], low.ValueReference[*base.SchemaProxy]]
		if lDef != nil {
			a = lDef.Schemas
		}
		if rDef != nil {
			b = rDef.Schemas
		}
		cc.SchemaChanges = CheckMapForChanges(a, b, &changes, v2.DefinitionsLabel, CompareSchemas)
	}

	// Swagger Security Definitions
	if reflect.TypeOf(&v2.SecurityDefinitions{}) == reflect.TypeOf(l) &&
		reflect.TypeOf(&v2.SecurityDefinitions{}) == reflect.TypeOf(r) {
		lDef := l.(*v2.SecurityDefinitions)
		rDef := r.(*v2.SecurityDefinitions)
		var a, b *orderedmap.Map[low.KeyReference[string], low.ValueReference[*v2.SecurityScheme]]
		if lDef != nil {
			a = lDef.Definitions
		}
		if rDef != nil {
			b = rDef.Definitions
		}
		cc.SecuritySchemeChanges = CheckMapForChanges(a, b, &changes,
			v3.SecurityDefinitionLabel, CompareSecuritySchemesV2)
	}

	// OpenAPI Components
	if reflect.TypeOf(&v3.Components{}) == reflect.TypeOf(l) &&
		reflect.TypeOf(&v3.Components{}) == reflect.TypeOf(r) {

		lComponents := l.(*v3.Components)
		rComponents := r.(*v3.Components)

		//if low.AreEqual(lComponents, rComponents) {
		//	return nil
		//}

		doneChan := make(chan componentComparison)
		comparisons := 0

		// run as fast as we can, thread all the things.
		if !lComponents.Schemas.IsEmpty() || !rComponents.Schemas.IsEmpty() {
			comparisons++
			go runComparison(lComponents.Schemas.Value, rComponents.Schemas.Value,
				&changes, v3.SchemasLabel, CompareSchemas, doneChan)
		}

		if !lComponents.Responses.IsEmpty() || !rComponents.Responses.IsEmpty() {
			comparisons++
			go runComparison(lComponents.Responses.Value, rComponents.Responses.Value,
				&changes, v3.ResponsesLabel, CompareResponseV3, doneChan)
		}

		if !lComponents.Parameters.IsEmpty() || !rComponents.Parameters.IsEmpty() {
			comparisons++
			go runComparison(lComponents.Parameters.Value, rComponents.Parameters.Value,
				&changes, v3.ParametersLabel, CompareParametersV3, doneChan)
		}

		if !lComponents.Examples.IsEmpty() || !rComponents.Examples.IsEmpty() {
			comparisons++
			go runComparison(lComponents.Examples.Value, rComponents.Examples.Value,
				&changes, v3.ExamplesLabel, CompareExamples, doneChan)
		}

		if !lComponents.RequestBodies.IsEmpty() || !rComponents.RequestBodies.IsEmpty() {
			comparisons++
			go runComparison(lComponents.RequestBodies.Value, rComponents.RequestBodies.Value,
				&changes, v3.RequestBodiesLabel, CompareRequestBodies, doneChan)
		}

		if !lComponents.Headers.IsEmpty() || !rComponents.Headers.IsEmpty() {
			comparisons++
			go runComparison(lComponents.Headers.Value, rComponents.Headers.Value,
				&changes, v3.HeadersLabel, CompareHeadersV3, doneChan)
		}

		if !lComponents.SecuritySchemes.IsEmpty() || !rComponents.SecuritySchemes.IsEmpty() {
			comparisons++
			go runComparison(lComponents.SecuritySchemes.Value, rComponents.SecuritySchemes.Value,
				&changes, v3.SecuritySchemesLabel, CompareSecuritySchemesV3, doneChan)
		}

		if !lComponents.Links.IsEmpty() || !rComponents.Links.IsEmpty() {
			comparisons++
			go runComparison(lComponents.Links.Value, rComponents.Links.Value,
				&changes, v3.LinksLabel, CompareLinks, doneChan)
		}

		if !lComponents.Callbacks.IsEmpty() || !rComponents.Callbacks.IsEmpty() {
			comparisons++
			go runComparison(lComponents.Callbacks.Value, rComponents.Callbacks.Value,
				&changes, v3.CallbacksLabel, CompareCallback, doneChan)
		}

		if !lComponents.MediaTypes.IsEmpty() || !rComponents.MediaTypes.IsEmpty() {
			comparisons++
			go runComparison(lComponents.MediaTypes.Value, rComponents.MediaTypes.Value,
				&changes, v3.MediaTypesLabel, CompareMediaTypes, doneChan)
		}

		cc.ExtensionChanges = CompareExtensions(lComponents.Extensions, rComponents.Extensions)

		completedComponents := 0
		for completedComponents < comparisons {
			res := <-doneChan
			switch res.prop {
			case v3.SchemasLabel:
				completedComponents++
				cc.SchemaChanges = res.result.(map[string]*SchemaChanges)
			case v3.SecuritySchemesLabel:
				completedComponents++
				cc.SecuritySchemeChanges = res.result.(map[string]*SecuritySchemeChanges)
			case v3.ResponsesLabel, v3.ParametersLabel, v3.ExamplesLabel, v3.RequestBodiesLabel, v3.HeadersLabel,
				v3.LinksLabel, v3.CallbacksLabel, v3.MediaTypesLabel:
				completedComponents++
			}
		}
	}

	cc.PropertyChanges = NewPropertyChanges(changes)
	if cc.TotalChanges() <= 0 {
		return nil
	}
	return cc
}

type componentComparison struct {
	prop   string
	result any
}

// run a generic comparison in a thread which in turn splits checks into further threads.
func runComparison[T any, R any](l, r *orderedmap.Map[low.KeyReference[string], low.ValueReference[T]],
	changes *[]*Change, label string, compareFunc func(l, r T) R, doneChan chan componentComparison,
) {
	// for schemas
	if label == v3.SchemasLabel || label == v2.DefinitionsLabel || label == v3.SecuritySchemesLabel {
		doneChan <- componentComparison{
			prop:   label,
			result: CheckMapForChanges(l, r, changes, label, compareFunc),
		}
		return
	} else {
		doneChan <- componentComparison{
			prop:   label,
			result: CheckMapForAdditionRemoval(l, r, changes, label),
		}
	}
}

// GetAllChanges returns a slice of all changes made between Callback objects
func (c *ComponentsChanges) GetAllChanges() []*Change {
	if c == nil {
		return nil
	}
	var changes []*Change
	changes = append(changes, c.Changes...)
	for k := range c.SchemaChanges {
		changes = append(changes, c.SchemaChanges[k].GetAllChanges()...)
	}
	for k := range c.SecuritySchemeChanges {
		changes = append(changes, c.SecuritySchemeChanges[k].GetAllChanges()...)
	}
	if c.ExtensionChanges != nil {
		changes = append(changes, c.ExtensionChanges.GetAllChanges()...)
	}
	return changes
}

// TotalChanges returns total changes for all Components and Definitions
func (c *ComponentsChanges) TotalChanges() int {
	if c == nil {
		return 0
	}
	v := c.PropertyChanges.TotalChanges()
	for k := range c.SchemaChanges {
		v += c.SchemaChanges[k].TotalChanges()
	}
	for k := range c.SecuritySchemeChanges {
		v += c.SecuritySchemeChanges[k].TotalChanges()
	}
	if c.ExtensionChanges != nil {
		v += c.ExtensionChanges.TotalChanges()
	}
	return v
}

// TotalBreakingChanges returns all breaking changes found for all Components and Definitions
func (c *ComponentsChanges) TotalBreakingChanges() int {
	v := c.PropertyChanges.TotalBreakingChanges()
	for k := range c.SchemaChanges {
		v += c.SchemaChanges[k].TotalBreakingChanges()
	}
	for k := range c.SecuritySchemeChanges {
		v += c.SecuritySchemeChanges[k].TotalBreakingChanges()
	}
	return v
}
