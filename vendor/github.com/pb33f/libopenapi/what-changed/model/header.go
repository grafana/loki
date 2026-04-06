// Copyright 2022-2025 Princess Beef Heavy Industries, LLC / Dave Shanley
// SPDX-License-Identifier: MIT

package model

import (
	"reflect"

	"github.com/pb33f/libopenapi/datamodel/low"
	v2 "github.com/pb33f/libopenapi/datamodel/low/v2"
	"github.com/pb33f/libopenapi/datamodel/low/v3"
)

// HeaderChanges represents changes made between two Header objects. Supports both Swagger and OpenAPI header
// objects, V2 only property Items is broken out into its own.
type HeaderChanges struct {
	*PropertyChanges
	SchemaChanges    *SchemaChanges               `json:"schemas,omitempty" yaml:"schemas,omitempty"`
	ExamplesChanges  map[string]*ExampleChanges   `json:"examples,omitempty" yaml:"examples,omitempty"`
	ContentChanges   map[string]*MediaTypeChanges `json:"content,omitempty" yaml:"content,omitempty"`
	ExtensionChanges *ExtensionChanges            `json:"extensions,omitempty" yaml:"extensions,omitempty"`

	// Items only supported by Swagger (V2)
	ItemsChanges *ItemsChanges `json:"items,omitempty" yaml:"items,omitempty"`
}

// GetAllChanges returns a slice of all changes made between Header objects
func (h *HeaderChanges) GetAllChanges() []*Change {
	if h == nil {
		return nil
	}
	var changes []*Change
	changes = append(changes, h.Changes...)
	for k := range h.ExamplesChanges {
		changes = append(changes, h.ExamplesChanges[k].GetAllChanges()...)
	}
	for k := range h.ContentChanges {
		changes = append(changes, h.ContentChanges[k].GetAllChanges()...)
	}
	if h.ExtensionChanges != nil {
		changes = append(changes, h.ExtensionChanges.GetAllChanges()...)
	}
	if h.SchemaChanges != nil {
		changes = append(changes, h.SchemaChanges.GetAllChanges()...)
	}
	if h.ItemsChanges != nil {
		changes = append(changes, h.ItemsChanges.GetAllChanges()...)
	}
	return changes
}

// TotalChanges returns the total number of changes made between two Header objects.
func (h *HeaderChanges) TotalChanges() int {
	if h == nil {
		return 0
	}
	c := h.PropertyChanges.TotalChanges()
	for k := range h.ExamplesChanges {
		c += h.ExamplesChanges[k].TotalChanges()
	}
	for k := range h.ContentChanges {
		c += h.ContentChanges[k].TotalChanges()
	}
	if h.ExtensionChanges != nil {
		c += h.ExtensionChanges.TotalChanges()
	}
	if h.SchemaChanges != nil {
		c += h.SchemaChanges.TotalChanges()
	}
	if h.ItemsChanges != nil {
		c += h.ItemsChanges.TotalChanges()
	}
	return c
}

// TotalBreakingChanges returns the total number of breaking changes made between two Header instances.
func (h *HeaderChanges) TotalBreakingChanges() int {
	c := h.PropertyChanges.TotalBreakingChanges()
	for k := range h.ContentChanges {
		c += h.ContentChanges[k].TotalBreakingChanges()
	}
	if h.ItemsChanges != nil {
		c += h.ItemsChanges.TotalBreakingChanges()
	}
	if h.SchemaChanges != nil {
		c += h.SchemaChanges.TotalBreakingChanges()
	}
	return c
}

// shared header properties
func addOpenAPIHeaderProperties(left, right low.OpenAPIHeader, changes *[]*Change) []*PropertyCheck {
	var props []*PropertyCheck

	// style
	addPropertyCheck(&props, left.GetStyle().ValueNode, right.GetStyle().ValueNode,
		left.GetStyle(), right.GetStyle(), changes, v3.StyleLabel,
		BreakingModified(CompHeader, PropStyle), CompHeader, PropStyle)

	// allow reserved
	addPropertyCheck(&props, left.GetAllowReserved().ValueNode, right.GetAllowReserved().ValueNode,
		left.GetAllowReserved(), right.GetAllowReserved(), changes, v3.AllowReservedLabel,
		BreakingModified(CompHeader, PropAllowReserved), CompHeader, PropAllowReserved)

	// allow empty value
	addPropertyCheck(&props, left.GetAllowEmptyValue().ValueNode, right.GetAllowEmptyValue().ValueNode,
		left.GetAllowEmptyValue(), right.GetAllowEmptyValue(), changes, v3.AllowEmptyValueLabel,
		BreakingModified(CompHeader, PropAllowEmptyValue), CompHeader, PropAllowEmptyValue)

	// explode
	addPropertyCheck(&props, left.GetExplode().ValueNode, right.GetExplode().ValueNode,
		left.GetExplode(), right.GetExplode(), changes, v3.ExplodeLabel,
		BreakingModified(CompHeader, PropExplode), CompHeader, PropExplode)

	// example
	CheckPropertyAdditionOrRemovalWithEncoding(left.GetExample().ValueNode, right.GetExample().ValueNode,
		v3.ExampleLabel, changes,
		BreakingAdded(CompHeader, PropExample) || BreakingRemoved(CompHeader, PropExample),
		left.GetExample(), right.GetExample())
	CheckForModificationWithEncoding(left.GetExample().ValueNode, right.GetExample().ValueNode,
		v3.ExampleLabel, changes, BreakingModified(CompHeader, PropExample),
		left.GetExample(), right.GetExample())

	// deprecated
	addPropertyCheck(&props, left.GetDeprecated().ValueNode, right.GetDeprecated().ValueNode,
		left.GetDeprecated(), right.GetDeprecated(), changes, v3.DeprecatedLabel,
		BreakingModified(CompHeader, PropDeprecated), CompHeader, PropDeprecated)

	// required
	addPropertyCheck(&props, left.GetRequired().ValueNode, right.GetRequired().ValueNode,
		left.GetRequired(), right.GetRequired(), changes, v3.RequiredLabel,
		BreakingModified(CompHeader, PropRequired), CompHeader, PropRequired)

	return props
}

// swagger only properties
func addSwaggerHeaderProperties(left, right low.SwaggerHeader, changes *[]*Change) []*PropertyCheck {
	var props []*PropertyCheck

	// type
	addPropertyCheck(&props, left.GetType().ValueNode, right.GetType().ValueNode,
		left.GetType(), right.GetType(), changes, v3.TypeLabel, true, CompHeader, PropType)

	// format
	addPropertyCheck(&props, left.GetFormat().ValueNode, right.GetFormat().ValueNode,
		left.GetFormat(), right.GetFormat(), changes, v3.FormatLabel, true, CompHeader, PropFormat)

	// collection format
	addPropertyCheck(&props, left.GetCollectionFormat().ValueNode, right.GetCollectionFormat().ValueNode,
		left.GetCollectionFormat(), right.GetCollectionFormat(), changes, v3.CollectionFormatLabel, true, CompHeader, PropCollectionFormat)

	// maximum
	addPropertyCheck(&props, left.GetMaximum().ValueNode, right.GetMaximum().ValueNode,
		left.GetMaximum(), right.GetMaximum(), changes, v3.MaximumLabel, true, CompHeader, PropMaximum)

	// minimum
	addPropertyCheck(&props, left.GetMinimum().ValueNode, right.GetMinimum().ValueNode,
		left.GetMinimum(), right.GetMinimum(), changes, v3.MinimumLabel, true, CompHeader, PropMinimum)

	// exclusive maximum
	addPropertyCheck(&props, left.GetExclusiveMaximum().ValueNode, right.GetExclusiveMaximum().ValueNode,
		left.GetExclusiveMaximum(), right.GetExclusiveMaximum(), changes, v3.ExclusiveMaximumLabel, true, CompHeader, PropExclusiveMaximum)

	// exclusive minimum
	addPropertyCheck(&props, left.GetExclusiveMinimum().ValueNode, right.GetExclusiveMinimum().ValueNode,
		left.GetExclusiveMinimum(), right.GetExclusiveMinimum(), changes, v3.ExclusiveMinimumLabel, true, CompHeader, PropExclusiveMinimum)

	// max length
	addPropertyCheck(&props, left.GetMaxLength().ValueNode, right.GetMaxLength().ValueNode,
		left.GetMaxLength(), right.GetMaxLength(), changes, v3.MaxLengthLabel, true, CompHeader, PropMaxLength)

	// min length
	addPropertyCheck(&props, left.GetMinLength().ValueNode, right.GetMinLength().ValueNode,
		left.GetMinLength(), right.GetMinLength(), changes, v3.MinLengthLabel, true, CompHeader, PropMinLength)

	// pattern
	addPropertyCheck(&props, left.GetPattern().ValueNode, right.GetPattern().ValueNode,
		left.GetPattern(), right.GetPattern(), changes, v3.PatternLabel, true, CompHeader, PropPattern)

	// max items
	addPropertyCheck(&props, left.GetMaxItems().ValueNode, right.GetMaxItems().ValueNode,
		left.GetMaxItems(), right.GetMaxItems(), changes, v3.MaxItemsLabel, true, CompHeader, PropMaxItems)

	// min items
	addPropertyCheck(&props, left.GetMinItems().ValueNode, right.GetMinItems().ValueNode,
		left.GetMinItems(), right.GetMinItems(), changes, v3.MinItemsLabel, true, CompHeader, PropMinItems)

	// unique items
	addPropertyCheck(&props, left.GetUniqueItems().ValueNode, right.GetUniqueItems().ValueNode,
		left.GetUniqueItems(), right.GetUniqueItems(), changes, v3.UniqueItemsLabel, true, CompHeader, PropUniqueItems)

	// multiple of
	addPropertyCheck(&props, left.GetMultipleOf().ValueNode, right.GetMultipleOf().ValueNode,
		left.GetMultipleOf(), right.GetMultipleOf(), changes, v3.MultipleOfLabel, true, CompHeader, PropMultipleOf)

	return props
}

// common header properties
func addCommonHeaderProperties(left, right low.HasDescription, changes *[]*Change) []*PropertyCheck {
	var props []*PropertyCheck

	// description
	addPropertyCheck(&props, left.GetDescription().ValueNode, right.GetDescription().ValueNode,
		left.GetDescription(), right.GetDescription(), changes, v3.DescriptionLabel,
		BreakingModified(CompHeader, PropDescription), CompHeader, PropDescription)

	return props
}

// CompareHeadersV2 is a Swagger compatible, typed signature used for other generic functions. It simply
// wraps CompareHeaders and provides nothing other that a typed interface.
func CompareHeadersV2(l, r *v2.Header) *HeaderChanges {
	return CompareHeaders(l, r)
}

// CompareHeadersV3 is an OpenAPI 3+ compatible, typed signature used for other generic functions. It simply
// wraps CompareHeaders and provides nothing other that a typed interface.
func CompareHeadersV3(l, r *v3.Header) *HeaderChanges {
	return CompareHeaders(l, r)
}

// CompareHeaders will compare left and right Header objects (any version of Swagger or OpenAPI) and return
// a pointer to HeaderChanges with anything that has changed, or nil if nothing changed.
func CompareHeaders(l, r any) *HeaderChanges {
	var changes []*Change
	var props []*PropertyCheck
	hc := new(HeaderChanges)

	// handle swagger.
	if reflect.TypeOf(&v2.Header{}) == reflect.TypeOf(l) && reflect.TypeOf(&v2.Header{}) == reflect.TypeOf(r) {
		lHeader := l.(*v2.Header)
		rHeader := r.(*v2.Header)

		// perform hash check to avoid further processing
		if low.AreEqual(lHeader, rHeader) {
			return nil
		}

		props = append(props, addCommonHeaderProperties(lHeader, rHeader, &changes)...)
		props = append(props, addSwaggerHeaderProperties(lHeader, rHeader, &changes)...)

		// enum
		if len(lHeader.Enum.Value) > 0 || len(rHeader.Enum.Value) > 0 {
			ExtractRawValueSliceChanges(lHeader.Enum.Value, rHeader.Enum.Value, &changes, v3.EnumLabel, true)
		}

		// items
		if !lHeader.Items.IsEmpty() && !rHeader.Items.IsEmpty() {
			if !low.AreEqual(lHeader.Items.Value, rHeader.Items.Value) {
				hc.ItemsChanges = CompareItems(lHeader.Items.Value, rHeader.Items.Value)
			}
		}
		if lHeader.Items.IsEmpty() && !rHeader.Items.IsEmpty() {
			CreateChange(&changes, ObjectAdded, v3.ItemsLabel, nil,
				rHeader.Items.ValueNode, BreakingAdded(CompHeader, PropItems), nil, rHeader.Items.Value)
		}
		if !lHeader.Items.IsEmpty() && rHeader.Items.IsEmpty() {
			CreateChange(&changes, ObjectRemoved, v3.SchemaLabel, lHeader.Items.ValueNode,
				nil, BreakingRemoved(CompHeader, PropItems), lHeader.Items.Value, nil)
		}
		hc.ExtensionChanges = CompareExtensions(lHeader.Extensions, rHeader.Extensions)
	}

	// handle OpenAPI
	if reflect.TypeOf(&v3.Header{}) == reflect.TypeOf(l) && reflect.TypeOf(&v3.Header{}) == reflect.TypeOf(r) {
		lHeader := l.(*v3.Header)
		rHeader := r.(*v3.Header)

		// perform hash check to avoid further processing
		if low.AreEqual(lHeader, rHeader) {
			return nil
		}

		props = append(props, addCommonHeaderProperties(lHeader, rHeader, &changes)...)
		props = append(props, addOpenAPIHeaderProperties(lHeader, rHeader, &changes)...)

		// header
		if !lHeader.Schema.IsEmpty() || !rHeader.Schema.IsEmpty() {
			hc.SchemaChanges = CompareSchemas(lHeader.Schema.Value, rHeader.Schema.Value)
		}

		// examples
		hc.ExamplesChanges = CheckMapForChanges(lHeader.Examples.Value, rHeader.Examples.Value,
			&changes, v3.ExamplesLabel, CompareExamples)

		// content
		hc.ContentChanges = CheckMapForChanges(lHeader.Content.Value, rHeader.Content.Value,
			&changes, v3.ContentLabel, CompareMediaTypes)

		hc.ExtensionChanges = CompareExtensions(lHeader.Extensions, rHeader.Extensions)

	}
	CheckProperties(props)
	hc.PropertyChanges = NewPropertyChanges(changes)
	return hc
}
