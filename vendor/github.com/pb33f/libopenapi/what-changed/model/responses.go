// Copyright 2022-2025 Princess Beef Heavy Industries, LLC / Dave Shanley
// SPDX-License-Identifier: MIT

package model

import (
	"reflect"

	"github.com/pb33f/libopenapi/datamodel/low"
	"github.com/pb33f/libopenapi/datamodel/low/v2"
	"github.com/pb33f/libopenapi/datamodel/low/v3"
)

// ResponsesChanges represents changes made between two Swagger or OpenAPI Responses objects.
type ResponsesChanges struct {
	*PropertyChanges
	ResponseChanges  map[string]*ResponseChanges `json:"response,omitempty" yaml:"response,omitempty"`
	DefaultChanges   *ResponseChanges            `json:"default,omitempty" yaml:"default,omitempty"`
	ExtensionChanges *ExtensionChanges           `json:"extensions,omitempty" yaml:"extensions,omitempty"`
}

// GetAllChanges returns a slice of all changes made between Responses objects
func (r *ResponsesChanges) GetAllChanges() []*Change {
	if r == nil {
		return nil
	}
	var changes []*Change
	changes = append(changes, r.Changes...)
	for k := range r.ResponseChanges {
		changes = append(changes, r.ResponseChanges[k].GetAllChanges()...)
	}
	if r.DefaultChanges != nil {
		changes = append(changes, r.DefaultChanges.GetAllChanges()...)
	}
	if r.ExtensionChanges != nil {
		changes = append(changes, r.ExtensionChanges.GetAllChanges()...)
	}
	return changes
}

// TotalChanges returns the total number of changes found between two Swagger or OpenAPI Responses objects
func (r *ResponsesChanges) TotalChanges() int {
	if r == nil {
		return 0
	}
	c := r.PropertyChanges.TotalChanges()
	for k := range r.ResponseChanges {
		c += r.ResponseChanges[k].TotalChanges()
	}
	if r.DefaultChanges != nil {
		c += r.DefaultChanges.TotalChanges()
	}
	if r.ExtensionChanges != nil {
		c += r.ExtensionChanges.TotalChanges()
	}
	return c
}

// TotalBreakingChanges returns the total number of changes found between two Swagger or OpenAPI
// Responses Objects
func (r *ResponsesChanges) TotalBreakingChanges() int {
	c := r.PropertyChanges.TotalBreakingChanges()
	for k := range r.ResponseChanges {
		c += r.ResponseChanges[k].TotalBreakingChanges()
	}
	if r.DefaultChanges != nil {
		c += r.DefaultChanges.TotalBreakingChanges()
	}
	return c
}

// CompareResponses compares a left and right Swagger or OpenAPI Responses object for any changes. If found
// returns a pointer to ResponsesChanges, or returns nil.
func CompareResponses(l, r any) *ResponsesChanges {
	var changes []*Change

	rc := new(ResponsesChanges)

	// swagger
	if reflect.TypeOf(&v2.Responses{}) == reflect.TypeOf(l) &&
		reflect.TypeOf(&v2.Responses{}) == reflect.TypeOf(r) {

		lResponses := l.(*v2.Responses)
		rResponses := r.(*v2.Responses)

		// perform hash check to avoid further processing
		if low.AreEqual(lResponses, rResponses) {
			return nil
		}

		if !lResponses.Default.IsEmpty() && !rResponses.Default.IsEmpty() {
			rc.DefaultChanges = CompareResponse(lResponses.Default.Value, rResponses.Default.Value)
		}
		if !lResponses.Default.IsEmpty() && rResponses.Default.IsEmpty() {
			CreateChange(&changes, ObjectRemoved, v3.DefaultLabel,
				lResponses.Default.ValueNode, nil, BreakingRemoved(CompResponses, PropDefault),
				lResponses.Default.Value, nil)
		}
		if lResponses.Default.IsEmpty() && !rResponses.Default.IsEmpty() {
			CreateChange(&changes, ObjectAdded, v3.DefaultLabel,
				nil, rResponses.Default.ValueNode, BreakingAdded(CompResponses, PropDefault),
				nil, lResponses.Default.Value)
		}

		rc.ResponseChanges = CheckMapForChangesWithRules(lResponses.Codes, rResponses.Codes,
			&changes, v3.CodesLabel, CompareResponseV2, CompResponses, PropCodes)

		rc.ExtensionChanges = CompareExtensions(lResponses.Extensions, rResponses.Extensions)
	}

	// openapi
	if reflect.TypeOf(&v3.Responses{}) == reflect.TypeOf(l) &&
		reflect.TypeOf(&v3.Responses{}) == reflect.TypeOf(r) {

		lResponses := l.(*v3.Responses)
		rResponses := r.(*v3.Responses)

		// perform hash check to avoid further processing
		if low.AreEqual(lResponses, rResponses) {
			return nil
		}

		if !lResponses.Default.IsEmpty() && !rResponses.Default.IsEmpty() {
			rc.DefaultChanges = CompareResponse(lResponses.Default.Value, rResponses.Default.Value)
		}
		if !lResponses.Default.IsEmpty() && rResponses.Default.IsEmpty() {
			CreateChange(&changes, ObjectRemoved, v3.DefaultLabel,
				lResponses.Default.ValueNode, nil, BreakingRemoved(CompResponses, PropDefault),
				lResponses.Default.Value, nil)
		}
		if lResponses.Default.IsEmpty() && !rResponses.Default.IsEmpty() {
			CreateChange(&changes, ObjectAdded, v3.DefaultLabel,
				nil, rResponses.Default.ValueNode, BreakingAdded(CompResponses, PropDefault),
				nil, lResponses.Default.Value)
		}

		rc.ResponseChanges = CheckMapForChangesWithRules(lResponses.Codes, rResponses.Codes,
			&changes, v3.CodesLabel, CompareResponseV3, CompResponses, PropCodes)

		rc.ExtensionChanges = CompareExtensions(lResponses.Extensions, rResponses.Extensions)

	}

	rc.PropertyChanges = NewPropertyChanges(changes)
	return rc
}
