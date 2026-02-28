// Copyright 2022-2026 Princess Beef Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package arazzo

import (
	"github.com/pb33f/libopenapi/datamodel/high"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
	low "github.com/pb33f/libopenapi/datamodel/low/arazzo"
	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
)

// Arazzo represents a high-level Arazzo document.
// https://spec.openapis.org/arazzo/v1.0.1
type Arazzo struct {
	Arazzo             string                              `json:"arazzo,omitempty" yaml:"arazzo,omitempty"`
	Info               *Info                               `json:"info,omitempty" yaml:"info,omitempty"`
	SourceDescriptions []*SourceDescription                `json:"sourceDescriptions,omitempty" yaml:"sourceDescriptions,omitempty"`
	Workflows          []*Workflow                         `json:"workflows,omitempty" yaml:"workflows,omitempty"`
	Components         *Components                         `json:"components,omitempty" yaml:"components,omitempty"`
	Extensions         *orderedmap.Map[string, *yaml.Node] `json:"-" yaml:"-"`
	openAPISourceDocs  []*v3.Document
	low                *low.Arazzo
}

// NewArazzo creates a new high-level Arazzo instance from a low-level one.
func NewArazzo(a *low.Arazzo) *Arazzo {
	h := new(Arazzo)
	h.low = a
	if !a.Arazzo.IsEmpty() {
		h.Arazzo = a.Arazzo.Value
	}
	if !a.Info.IsEmpty() {
		h.Info = NewInfo(a.Info.Value)
	}
	if !a.SourceDescriptions.IsEmpty() {
		h.SourceDescriptions = buildSlice(a.SourceDescriptions.Value, NewSourceDescription)
	}
	if !a.Workflows.IsEmpty() {
		h.Workflows = buildSlice(a.Workflows.Value, NewWorkflow)
	}
	if !a.Components.IsEmpty() {
		h.Components = NewComponents(a.Components.Value)
	}
	h.Extensions = high.ExtractExtensions(a.Extensions)
	return h
}

// GoLow returns the low-level Arazzo instance used to create the high-level one.
func (a *Arazzo) GoLow() *low.Arazzo {
	return a.low
}

// GoLowUntyped returns the low-level Arazzo instance with no type.
func (a *Arazzo) GoLowUntyped() any {
	return a.low
}

// AddOpenAPISourceDocument attaches one or more OpenAPI source documents to this Arazzo model.
// Attached documents are runtime metadata and are not rendered or serialized.
func (a *Arazzo) AddOpenAPISourceDocument(docs ...*v3.Document) {
	if a == nil || len(docs) == 0 {
		return
	}
	for _, doc := range docs {
		if doc != nil {
			a.openAPISourceDocs = append(a.openAPISourceDocs, doc)
		}
	}
}

// GetOpenAPISourceDocuments returns attached OpenAPI source documents.
func (a *Arazzo) GetOpenAPISourceDocuments() []*v3.Document {
	if a == nil || len(a.openAPISourceDocs) == 0 {
		return nil
	}
	docs := make([]*v3.Document, len(a.openAPISourceDocs))
	copy(docs, a.openAPISourceDocs)
	return docs
}

// Render returns a YAML representation of the Arazzo object as a byte slice.
func (a *Arazzo) Render() ([]byte, error) {
	return yaml.Marshal(a)
}

// MarshalYAML creates a ready to render YAML representation of the Arazzo object.
func (a *Arazzo) MarshalYAML() (any, error) {
	m := orderedmap.New[string, any]()
	if a.Arazzo != "" {
		m.Set(low.ArazzoLabel, a.Arazzo)
	}
	if a.Info != nil {
		m.Set(low.InfoLabel, a.Info)
	}
	if len(a.SourceDescriptions) > 0 {
		m.Set(low.SourceDescriptionsLabel, a.SourceDescriptions)
	}
	if len(a.Workflows) > 0 {
		m.Set(low.WorkflowsLabel, a.Workflows)
	}
	if a.Components != nil {
		m.Set(low.ComponentsLabel, a.Components)
	}
	marshalExtensions(m, a.Extensions)
	return m, nil
}
