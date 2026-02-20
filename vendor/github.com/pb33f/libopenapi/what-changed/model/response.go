// Copyright 2022-2025 Princess Beef Heavy Industries, LLC / Dave Shanley
// SPDX-License-Identifier: MIT

package model

import (
	"reflect"

	"github.com/pb33f/libopenapi/datamodel/low"
	v3 "github.com/pb33f/libopenapi/datamodel/low/v3"
)

import (
	"github.com/pb33f/libopenapi/datamodel/low/v2"
)

// ResponseChanges represents changes found between two Swagger or OpenAPI Response objects.
type ResponseChanges struct {
	*PropertyChanges
	ExtensionChanges *ExtensionChanges         `json:"extensions,omitempty" yaml:"extensions,omitempty"`
	HeadersChanges   map[string]*HeaderChanges `json:"headers,omitempty" yaml:"headers,omitempty"`

	// Swagger Response Properties.
	SchemaChanges   *SchemaChanges   `json:"schemas,omitempty" yaml:"schemas,omitempty"`
	ExamplesChanges *ExamplesChanges `json:"examples,omitempty" yaml:"examples,omitempty"`

	// OpenAPI Response Properties.
	ContentChanges map[string]*MediaTypeChanges `json:"content,omitempty" yaml:"content,omitempty"`
	LinkChanges    map[string]*LinkChanges      `json:"links,omitempty" yaml:"links,omitempty"`
}

// GetAllChanges returns a slice of all changes made between RequestBody objects
func (r *ResponseChanges) GetAllChanges() []*Change {
	if r == nil {
		return nil
	}
	var changes []*Change
	changes = append(changes, r.Changes...)
	if r.ExtensionChanges != nil {
		changes = append(changes, r.ExtensionChanges.GetAllChanges()...)
	}
	if r.SchemaChanges != nil {
		changes = append(changes, r.SchemaChanges.GetAllChanges()...)
	}
	if r.ExamplesChanges != nil {
		changes = append(changes, r.ExamplesChanges.GetAllChanges()...)
	}
	for k := range r.HeadersChanges {
		changes = append(changes, r.HeadersChanges[k].GetAllChanges()...)
	}
	for k := range r.ContentChanges {
		changes = append(changes, r.ContentChanges[k].GetAllChanges()...)
	}
	for k := range r.LinkChanges {
		changes = append(changes, r.LinkChanges[k].GetAllChanges()...)
	}
	return changes
}

// TotalChanges returns the total number of changes found between two Swagger or OpenAPI Response Objects
func (r *ResponseChanges) TotalChanges() int {
	if r == nil {
		return 0
	}
	c := r.PropertyChanges.TotalChanges()
	if r.ExtensionChanges != nil {
		c += r.ExtensionChanges.TotalChanges()
	}
	if r.SchemaChanges != nil {
		c += r.SchemaChanges.TotalChanges()
	}
	if r.ExamplesChanges != nil {
		c += r.ExamplesChanges.TotalChanges()
	}
	for k := range r.HeadersChanges {
		c += r.HeadersChanges[k].TotalChanges()
	}
	for k := range r.ContentChanges {
		c += r.ContentChanges[k].TotalChanges()
	}
	for k := range r.LinkChanges {
		c += r.LinkChanges[k].TotalChanges()
	}
	return c
}

// TotalBreakingChanges returns the total number of breaking changes found between two swagger or OpenAPI
// Response Objects
func (r *ResponseChanges) TotalBreakingChanges() int {
	c := r.PropertyChanges.TotalBreakingChanges()
	if r.SchemaChanges != nil {
		c += r.SchemaChanges.TotalBreakingChanges()
	}
	for k := range r.HeadersChanges {
		c += r.HeadersChanges[k].TotalBreakingChanges()
	}
	for k := range r.ContentChanges {
		c += r.ContentChanges[k].TotalBreakingChanges()
	}
	for k := range r.LinkChanges {
		c += r.LinkChanges[k].TotalBreakingChanges()
	}
	return c
}

// CompareResponseV2 is a Swagger type safe proxy for CompareResponse
func CompareResponseV2(l, r *v2.Response) *ResponseChanges {
	return CompareResponse(l, r)
}

// CompareResponseV3 is an OpenAPI type safe proxy for CompareResponse
func CompareResponseV3(l, r *v3.Response) *ResponseChanges {
	return CompareResponse(l, r)
}

// CompareResponse compares a left and right Swagger or OpenAPI Response object. If anything is found
// a pointer to a ResponseChanges is returned, otherwise it returns nil.
func CompareResponse(l, r any) *ResponseChanges {
	var changes []*Change
	var props []*PropertyCheck

	rc := new(ResponseChanges)

	if reflect.TypeOf(&v2.Response{}) == reflect.TypeOf(l) && reflect.TypeOf(&v2.Response{}) == reflect.TypeOf(r) {

		lResponse := l.(*v2.Response)
		rResponse := r.(*v2.Response)

		// perform hash check to avoid further processing
		if low.AreEqual(lResponse, rResponse) {
			return nil
		}

		// description
		addPropertyCheck(&props, lResponse.Description.ValueNode, rResponse.Description.ValueNode,
			lResponse.Description.Value, rResponse.Description.Value, &changes, v3.DescriptionLabel, false, CompResponse, PropDescription)

		if !lResponse.Schema.IsEmpty() && !rResponse.Schema.IsEmpty() {
			rc.SchemaChanges = CompareSchemas(lResponse.Schema.Value, rResponse.Schema.Value)
		}
		if !lResponse.Schema.IsEmpty() && rResponse.Schema.IsEmpty() {
			CreateChange(&changes, ObjectRemoved, v3.SchemaLabel,
				lResponse.Schema.ValueNode, nil, BreakingRemoved(CompResponse, PropSchema),
				lResponse.Schema.Value, nil)
		}
		if lResponse.Schema.IsEmpty() && !rResponse.Schema.IsEmpty() {
			CreateChange(&changes, ObjectAdded, v3.SchemaLabel,
				nil, rResponse.Schema.ValueNode, BreakingAdded(CompResponse, PropSchema),
				nil, rResponse.Schema.Value)
		}

		rc.HeadersChanges = CheckMapForChanges(lResponse.Headers.Value, rResponse.Headers.Value,
			&changes, v3.HeadersLabel, CompareHeadersV2)

		if !lResponse.Examples.IsEmpty() && !rResponse.Examples.IsEmpty() {
			rc.ExamplesChanges = CompareExamplesV2(lResponse.Examples.Value, rResponse.Examples.Value)
		}
		if !lResponse.Examples.IsEmpty() && rResponse.Examples.IsEmpty() {
			CreateChange(&changes, PropertyRemoved, v3.ExamplesLabel,
				lResponse.Schema.ValueNode, nil, BreakingRemoved(CompResponse, PropExamples),
				lResponse.Schema.Value, nil)
		}
		if lResponse.Examples.IsEmpty() && !rResponse.Examples.IsEmpty() {
			CreateChange(&changes, ObjectAdded, v3.ExamplesLabel,
				nil, rResponse.Schema.ValueNode, BreakingAdded(CompResponse, PropExamples),
				nil, lResponse.Schema.Value)
		}

		rc.ExtensionChanges = CompareExtensions(lResponse.Extensions, rResponse.Extensions)
	}

	if reflect.TypeOf(&v3.Response{}) == reflect.TypeOf(l) && reflect.TypeOf(&v3.Response{}) == reflect.TypeOf(r) {

		lResponse := l.(*v3.Response)
		rResponse := r.(*v3.Response)

		// perform hash check to avoid further processing
		if low.AreEqual(lResponse, rResponse) {
			return nil
		}

		// summary (OpenAPI 3.2+)
		addPropertyCheck(&props, lResponse.Summary.ValueNode, rResponse.Summary.ValueNode,
			lResponse.Summary.Value, rResponse.Summary.Value, &changes, v3.SummaryLabel,
			BreakingModified(CompResponse, PropSummary), CompResponse, PropSummary)

		// description
		addPropertyCheck(&props, lResponse.Description.ValueNode, rResponse.Description.ValueNode,
			lResponse.Description.Value, rResponse.Description.Value, &changes, v3.DescriptionLabel,
			BreakingModified(CompResponse, PropDescription), CompResponse, PropDescription)

		rc.HeadersChanges = CheckMapForChanges(lResponse.Headers.Value, rResponse.Headers.Value,
			&changes, v3.HeadersLabel, CompareHeadersV3)

		rc.ContentChanges = CheckMapForChanges(lResponse.Content.Value, rResponse.Content.Value,
			&changes, v3.ContentLabel, CompareMediaTypes)

		rc.LinkChanges = CheckMapForChanges(lResponse.Links.Value, rResponse.Links.Value,
			&changes, v3.LinksLabel, CompareLinks)

		rc.ExtensionChanges = CompareExtensions(lResponse.Extensions, rResponse.Extensions)
	}

	CheckProperties(props)
	rc.PropertyChanges = NewPropertyChanges(changes)
	return rc
}
