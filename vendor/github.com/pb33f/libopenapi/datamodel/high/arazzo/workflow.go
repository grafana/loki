// Copyright 2022-2026 Princess Beef Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package arazzo

import (
	"github.com/pb33f/libopenapi/datamodel/high"
	lowmodel "github.com/pb33f/libopenapi/datamodel/low"
	low "github.com/pb33f/libopenapi/datamodel/low/arazzo"
	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
)

// Workflow represents a high-level Arazzo Workflow Object.
// https://spec.openapis.org/arazzo/v1.0.1#workflow-object
type Workflow struct {
	WorkflowId     string                              `json:"workflowId,omitempty" yaml:"workflowId,omitempty"`
	Summary        string                              `json:"summary,omitempty" yaml:"summary,omitempty"`
	Description    string                              `json:"description,omitempty" yaml:"description,omitempty"`
	Inputs         *yaml.Node                          `json:"inputs,omitempty" yaml:"inputs,omitempty"`
	DependsOn      []string                            `json:"dependsOn,omitempty" yaml:"dependsOn,omitempty"`
	Steps          []*Step                             `json:"steps,omitempty" yaml:"steps,omitempty"`
	SuccessActions []*SuccessAction                    `json:"successActions,omitempty" yaml:"successActions,omitempty"`
	FailureActions []*FailureAction                    `json:"failureActions,omitempty" yaml:"failureActions,omitempty"`
	Outputs        *orderedmap.Map[string, string]     `json:"outputs,omitempty" yaml:"outputs,omitempty"`
	Parameters     []*Parameter                        `json:"parameters,omitempty" yaml:"parameters,omitempty"`
	Extensions     *orderedmap.Map[string, *yaml.Node] `json:"-" yaml:"-"`
	low            *low.Workflow
}

// NewWorkflow creates a new high-level Workflow instance from a low-level one.
func NewWorkflow(wf *low.Workflow) *Workflow {
	w := new(Workflow)
	w.low = wf
	if !wf.WorkflowId.IsEmpty() {
		w.WorkflowId = wf.WorkflowId.Value
	}
	if !wf.Summary.IsEmpty() {
		w.Summary = wf.Summary.Value
	}
	if !wf.Description.IsEmpty() {
		w.Description = wf.Description.Value
	}
	if !wf.Inputs.IsEmpty() {
		w.Inputs = wf.Inputs.Value
	}
	if !wf.DependsOn.IsEmpty() {
		w.DependsOn = buildValueSlice(wf.DependsOn.Value)
	}
	if !wf.Steps.IsEmpty() {
		w.Steps = buildSlice(wf.Steps.Value, NewStep)
	}
	if !wf.SuccessActions.IsEmpty() {
		w.SuccessActions = buildSlice(wf.SuccessActions.Value, NewSuccessAction)
	}
	if !wf.FailureActions.IsEmpty() {
		w.FailureActions = buildSlice(wf.FailureActions.Value, NewFailureAction)
	}
	if !wf.Outputs.IsEmpty() {
		w.Outputs = lowmodel.FromReferenceMap[string, string](wf.Outputs.Value)
	}
	if !wf.Parameters.IsEmpty() {
		w.Parameters = buildSlice(wf.Parameters.Value, NewParameter)
	}
	w.Extensions = high.ExtractExtensions(wf.Extensions)
	return w
}

// GoLow returns the low-level Workflow instance used to create the high-level one.
func (w *Workflow) GoLow() *low.Workflow {
	return w.low
}

// GoLowUntyped returns the low-level Workflow instance with no type.
func (w *Workflow) GoLowUntyped() any {
	return w.low
}

// Render returns a YAML representation of the Workflow object as a byte slice.
func (w *Workflow) Render() ([]byte, error) {
	return yaml.Marshal(w)
}

// MarshalYAML creates a ready to render YAML representation of the Workflow object.
func (w *Workflow) MarshalYAML() (any, error) {
	m := orderedmap.New[string, any]()
	if w.WorkflowId != "" {
		m.Set(low.WorkflowIdLabel, w.WorkflowId)
	}
	if w.Summary != "" {
		m.Set(low.SummaryLabel, w.Summary)
	}
	if w.Description != "" {
		m.Set(low.DescriptionLabel, w.Description)
	}
	if w.Inputs != nil {
		m.Set(low.InputsLabel, w.Inputs)
	}
	if len(w.DependsOn) > 0 {
		m.Set(low.DependsOnLabel, w.DependsOn)
	}
	if len(w.Steps) > 0 {
		m.Set(low.StepsLabel, w.Steps)
	}
	if len(w.SuccessActions) > 0 {
		m.Set(low.SuccessActionsLabel, w.SuccessActions)
	}
	if len(w.FailureActions) > 0 {
		m.Set(low.FailureActionsLabel, w.FailureActions)
	}
	if w.Outputs != nil && w.Outputs.Len() > 0 {
		m.Set(low.OutputsLabel, w.Outputs)
	}
	if len(w.Parameters) > 0 {
		m.Set(low.ParametersLabel, w.Parameters)
	}
	marshalExtensions(m, w.Extensions)
	return m, nil
}
