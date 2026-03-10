// Copyright 2022-2026 Princess Beef Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package arazzo

import (
	"github.com/pb33f/libopenapi/datamodel/high"
	low "github.com/pb33f/libopenapi/datamodel/low/arazzo"
	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
)

// SuccessAction represents a high-level Arazzo Success Action Object.
// A success action can be a full definition or a Reusable Object with a $components reference.
// https://spec.openapis.org/arazzo/v1.0.1#success-action-object
type SuccessAction struct {
	Name       string                              `json:"name,omitempty" yaml:"name,omitempty"`
	Type       string                              `json:"type,omitempty" yaml:"type,omitempty"`
	WorkflowId string                              `json:"workflowId,omitempty" yaml:"workflowId,omitempty"`
	StepId     string                              `json:"stepId,omitempty" yaml:"stepId,omitempty"`
	Criteria   []*Criterion                        `json:"criteria,omitempty" yaml:"criteria,omitempty"`
	Reference  string                              `json:"reference,omitempty" yaml:"reference,omitempty"`
	Extensions *orderedmap.Map[string, *yaml.Node] `json:"-" yaml:"-"`
	low        *low.SuccessAction
}

// IsReusable returns true if this success action is a Reusable Object (has a reference field).
func (s *SuccessAction) IsReusable() bool {
	return s.Reference != ""
}

// NewSuccessAction creates a new high-level SuccessAction instance from a low-level one.
func NewSuccessAction(sa *low.SuccessAction) *SuccessAction {
	s := new(SuccessAction)
	s.low = sa
	if !sa.Name.IsEmpty() {
		s.Name = sa.Name.Value
	}
	if !sa.Type.IsEmpty() {
		s.Type = sa.Type.Value
	}
	if !sa.WorkflowId.IsEmpty() {
		s.WorkflowId = sa.WorkflowId.Value
	}
	if !sa.StepId.IsEmpty() {
		s.StepId = sa.StepId.Value
	}
	if !sa.ComponentRef.IsEmpty() {
		s.Reference = sa.ComponentRef.Value
	}
	if !sa.Criteria.IsEmpty() {
		s.Criteria = buildSlice(sa.Criteria.Value, NewCriterion)
	}
	s.Extensions = high.ExtractExtensions(sa.Extensions)
	return s
}

// GoLow returns the low-level SuccessAction instance used to create the high-level one.
func (s *SuccessAction) GoLow() *low.SuccessAction {
	return s.low
}

// GoLowUntyped returns the low-level SuccessAction instance with no type.
func (s *SuccessAction) GoLowUntyped() any {
	return s.low
}

// Render returns a YAML representation of the SuccessAction object as a byte slice.
func (s *SuccessAction) Render() ([]byte, error) {
	return yaml.Marshal(s)
}

// MarshalYAML creates a ready to render YAML representation of the SuccessAction object.
func (s *SuccessAction) MarshalYAML() (any, error) {
	m := orderedmap.New[string, any]()
	if s.Reference != "" {
		m.Set(low.ReferenceLabel, s.Reference)
		return m, nil
	}
	if s.Name != "" {
		m.Set(low.NameLabel, s.Name)
	}
	if s.Type != "" {
		m.Set(low.TypeLabel, s.Type)
	}
	if s.WorkflowId != "" {
		m.Set(low.WorkflowIdLabel, s.WorkflowId)
	}
	if s.StepId != "" {
		m.Set(low.StepIdLabel, s.StepId)
	}
	if len(s.Criteria) > 0 {
		m.Set(low.CriteriaLabel, s.Criteria)
	}
	marshalExtensions(m, s.Extensions)
	return m, nil
}
