// Copyright 2022-2026 Princess Beef Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package arazzo

import (
	"github.com/pb33f/libopenapi/datamodel/high"
	low "github.com/pb33f/libopenapi/datamodel/low/arazzo"
	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
)

// FailureAction represents a high-level Arazzo Failure Action Object.
// A failure action can be a full definition or a Reusable Object with a $components reference.
// https://spec.openapis.org/arazzo/v1.0.1#failure-action-object
type FailureAction struct {
	Name       string                              `json:"name,omitempty" yaml:"name,omitempty"`
	Type       string                              `json:"type,omitempty" yaml:"type,omitempty"`
	WorkflowId string                              `json:"workflowId,omitempty" yaml:"workflowId,omitempty"`
	StepId     string                              `json:"stepId,omitempty" yaml:"stepId,omitempty"`
	RetryAfter *float64                            `json:"retryAfter,omitempty" yaml:"retryAfter,omitempty"`
	RetryLimit *int64                              `json:"retryLimit,omitempty" yaml:"retryLimit,omitempty"`
	Criteria   []*Criterion                        `json:"criteria,omitempty" yaml:"criteria,omitempty"`
	Reference  string                              `json:"reference,omitempty" yaml:"reference,omitempty"`
	Extensions *orderedmap.Map[string, *yaml.Node] `json:"-" yaml:"-"`
	low        *low.FailureAction
}

// IsReusable returns true if this failure action is a Reusable Object (has a reference field).
func (f *FailureAction) IsReusable() bool {
	return f.Reference != ""
}

// NewFailureAction creates a new high-level FailureAction instance from a low-level one.
func NewFailureAction(fa *low.FailureAction) *FailureAction {
	f := new(FailureAction)
	f.low = fa
	if !fa.Name.IsEmpty() {
		f.Name = fa.Name.Value
	}
	if !fa.Type.IsEmpty() {
		f.Type = fa.Type.Value
	}
	if !fa.WorkflowId.IsEmpty() {
		f.WorkflowId = fa.WorkflowId.Value
	}
	if !fa.StepId.IsEmpty() {
		f.StepId = fa.StepId.Value
	}
	if !fa.RetryAfter.IsEmpty() {
		v := fa.RetryAfter.Value
		f.RetryAfter = &v
	}
	if !fa.RetryLimit.IsEmpty() {
		v := fa.RetryLimit.Value
		f.RetryLimit = &v
	}
	if !fa.ComponentRef.IsEmpty() {
		f.Reference = fa.ComponentRef.Value
	}
	if !fa.Criteria.IsEmpty() {
		f.Criteria = buildSlice(fa.Criteria.Value, NewCriterion)
	}
	f.Extensions = high.ExtractExtensions(fa.Extensions)
	return f
}

// GoLow returns the low-level FailureAction instance used to create the high-level one.
func (f *FailureAction) GoLow() *low.FailureAction {
	return f.low
}

// GoLowUntyped returns the low-level FailureAction instance with no type.
func (f *FailureAction) GoLowUntyped() any {
	return f.low
}

// Render returns a YAML representation of the FailureAction object as a byte slice.
func (f *FailureAction) Render() ([]byte, error) {
	return yaml.Marshal(f)
}

// MarshalYAML creates a ready to render YAML representation of the FailureAction object.
func (f *FailureAction) MarshalYAML() (any, error) {
	m := orderedmap.New[string, any]()
	if f.Reference != "" {
		m.Set(low.ReferenceLabel, f.Reference)
		return m, nil
	}
	if f.Name != "" {
		m.Set(low.NameLabel, f.Name)
	}
	if f.Type != "" {
		m.Set(low.TypeLabel, f.Type)
	}
	if f.WorkflowId != "" {
		m.Set(low.WorkflowIdLabel, f.WorkflowId)
	}
	if f.StepId != "" {
		m.Set(low.StepIdLabel, f.StepId)
	}
	if f.RetryAfter != nil {
		m.Set(low.RetryAfterLabel, *f.RetryAfter)
	}
	if f.RetryLimit != nil {
		m.Set(low.RetryLimitLabel, *f.RetryLimit)
	}
	if len(f.Criteria) > 0 {
		m.Set(low.CriteriaLabel, f.Criteria)
	}
	marshalExtensions(m, f.Extensions)
	return m, nil
}
