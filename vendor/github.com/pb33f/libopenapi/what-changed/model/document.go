// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

// Package model
//
// What-changed models are unified across OpenAPI and Swagger. Everything is kept flat for simplicity, so please
// excuse the size of the package. There is a lot of data to crunch!
//
// Every model in here is either universal (works across both versions of OpenAPI) or is bound to a specific version
// of OpenAPI. There is only a single model however - so version specific objects are marked accordingly.
package model

import (
	"reflect"

	"github.com/pb33f/libopenapi/datamodel/low"
	"github.com/pb33f/libopenapi/datamodel/low/base"
	v2 "github.com/pb33f/libopenapi/datamodel/low/v2"
	v3 "github.com/pb33f/libopenapi/datamodel/low/v3"
)

// DocumentChanges represents all the changes made to an OpenAPI document.
type DocumentChanges struct {
	*PropertyChanges
	InfoChanges                *InfoChanges                  `json:"info,omitempty" yaml:"info,omitempty"`
	PathsChanges               *PathsChanges                 `json:"paths,omitempty" yaml:"paths,omitempty"`
	TagChanges                 []*TagChanges                 `json:"tags,omitempty" yaml:"tags,omitempty"`
	ExternalDocChanges         *ExternalDocChanges           `json:"externalDoc,omitempty" yaml:"externalDoc,omitempty"`
	WebhookChanges             map[string]*PathItemChanges   `json:"webhooks,omitempty" yaml:"webhooks,omitempty"`
	ServerChanges              []*ServerChanges              `json:"servers,omitempty" yaml:"servers,omitempty"`
	SecurityRequirementChanges []*SecurityRequirementChanges `json:"securityRequirements,omitempty" yaml:"securityRequirements,omitempty"`
	ComponentsChanges          *ComponentsChanges            `json:"components,omitempty" yaml:"components,omitempty"`
	ExtensionChanges           *ExtensionChanges             `json:"extensions,omitempty" yaml:"extensions,omitempty"`
}

// TotalChanges returns a total count of all changes made in the Document
func (d *DocumentChanges) TotalChanges() int {
	if d == nil {
		return 0
	}

	c := d.PropertyChanges.TotalChanges()
	if d.InfoChanges != nil {
		c += d.InfoChanges.TotalChanges()
	}
	if d.PathsChanges != nil {
		c += d.PathsChanges.TotalChanges()
	}
	for k := range d.TagChanges {
		c += d.TagChanges[k].TotalChanges()
	}
	if d.ExternalDocChanges != nil {
		c += d.ExternalDocChanges.TotalChanges()
	}
	for k := range d.WebhookChanges {
		c += d.WebhookChanges[k].TotalChanges()
	}
	for k := range d.ServerChanges {
		c += d.ServerChanges[k].TotalChanges()
	}
	for k := range d.SecurityRequirementChanges {
		c += d.SecurityRequirementChanges[k].TotalChanges()
	}
	if d.ComponentsChanges != nil {
		c += d.ComponentsChanges.TotalChanges()
	}
	if d.ExtensionChanges != nil {
		c += d.ExtensionChanges.TotalChanges()
	}
	return c
}

// GetAllChanges returns a slice of all changes made between Document objects
func (d *DocumentChanges) GetAllChanges() []*Change {
	if d == nil {
		return nil
	}

	var changes []*Change
	changes = append(changes, d.Changes...)
	if d.InfoChanges != nil {
		changes = append(changes, d.InfoChanges.GetAllChanges()...)
	}
	if d.PathsChanges != nil {
		changes = append(changes, d.PathsChanges.GetAllChanges()...)
	}
	for k := range d.TagChanges {
		changes = append(changes, d.TagChanges[k].GetAllChanges()...)
	}
	if d.ExternalDocChanges != nil {
		changes = append(changes, d.ExternalDocChanges.GetAllChanges()...)
	}
	for k := range d.WebhookChanges {
		changes = append(changes, d.WebhookChanges[k].GetAllChanges()...)
	}
	for k := range d.ServerChanges {
		changes = append(changes, d.ServerChanges[k].GetAllChanges()...)
	}
	for k := range d.SecurityRequirementChanges {
		changes = append(changes, d.SecurityRequirementChanges[k].GetAllChanges()...)
	}
	if d.ComponentsChanges != nil {
		changes = append(changes, d.ComponentsChanges.GetAllChanges()...)
	}
	if d.ExtensionChanges != nil {
		changes = append(changes, d.ExtensionChanges.GetAllChanges()...)
	}
	return changes
}

// TotalBreakingChanges returns a total count of all breaking changes made in the Document
func (d *DocumentChanges) TotalBreakingChanges() int {
	if d == nil {
		return 0
	}

	c := d.PropertyChanges.TotalBreakingChanges()
	if d.InfoChanges != nil {
		c += d.InfoChanges.TotalBreakingChanges()
	}
	if d.PathsChanges != nil {
		c += d.PathsChanges.TotalBreakingChanges()
	}
	for k := range d.TagChanges {
		c += d.TagChanges[k].TotalBreakingChanges()
	}
	if d.ExternalDocChanges != nil {
		c += d.ExternalDocChanges.TotalBreakingChanges()
	}
	for k := range d.WebhookChanges {
		c += d.WebhookChanges[k].TotalBreakingChanges()
	}
	for k := range d.ServerChanges {
		c += d.ServerChanges[k].TotalBreakingChanges()
	}
	for k := range d.SecurityRequirementChanges {
		c += d.SecurityRequirementChanges[k].TotalBreakingChanges()
	}
	if d.ComponentsChanges != nil {
		c += d.ComponentsChanges.TotalBreakingChanges()
	}
	return c
}

// CompareDocuments will compare any two OpenAPI documents (either Swagger or OpenAPI) and return a pointer to
// DocumentChanges that outlines everything that was found to have changed.
func CompareDocuments(l, r any) *DocumentChanges {
	var changes []*Change
	var props []*PropertyCheck

	dc := new(DocumentChanges)

	// reset schema hashmap
	base.SchemaQuickHashMap.Clear()

	// clear hash cache to ensure clean state for comparison
	low.ClearHashCache()

	if reflect.TypeOf(&v2.Swagger{}) == reflect.TypeOf(l) && reflect.TypeOf(&v2.Swagger{}) == reflect.TypeOf(r) {
		lDoc := l.(*v2.Swagger)
		rDoc := r.(*v2.Swagger)

		// version
		addPropertyCheck(&props, lDoc.Swagger.ValueNode, rDoc.Swagger.ValueNode,
			lDoc.Swagger.Value, rDoc.Swagger.Value, &changes, v3.SwaggerLabel, true, CompOpenAPI, "")

		// host
		addPropertyCheck(&props, lDoc.Host.ValueNode, rDoc.Host.ValueNode,
			lDoc.Host.Value, rDoc.Host.Value, &changes, v3.HostLabel, true, "", "")

		// base path
		addPropertyCheck(&props, lDoc.BasePath.ValueNode, rDoc.BasePath.ValueNode,
			lDoc.BasePath.Value, rDoc.BasePath.Value, &changes, v3.BasePathLabel, true, "", "")

		// schemes
		if len(lDoc.Schemes.Value) > 0 || len(rDoc.Schemes.Value) > 0 {
			ExtractStringValueSliceChanges(lDoc.Schemes.Value, rDoc.Schemes.Value,
				&changes, v3.SchemesLabel, true)
		}
		// consumes
		if len(lDoc.Consumes.Value) > 0 || len(rDoc.Consumes.Value) > 0 {
			ExtractStringValueSliceChanges(lDoc.Consumes.Value, rDoc.Consumes.Value,
				&changes, v3.ConsumesLabel, true)
		}
		// produces
		if len(lDoc.Produces.Value) > 0 || len(rDoc.Produces.Value) > 0 {
			ExtractStringValueSliceChanges(lDoc.Produces.Value, rDoc.Produces.Value,
				&changes, v3.ProducesLabel, true)
		}

		// tags
		dc.TagChanges = CompareTags(lDoc.Tags.Value, rDoc.Tags.Value)

		// paths
		if !lDoc.Paths.IsEmpty() || !rDoc.Paths.IsEmpty() {
			dc.PathsChanges = ComparePaths(lDoc.Paths.Value, rDoc.Paths.Value)
		}

		// external docs
		compareDocumentExternalDocs(lDoc, rDoc, dc, &changes)

		// info
		compareDocumentInfo(&lDoc.Info, &rDoc.Info, dc, &changes)

		// security
		if !lDoc.Security.IsEmpty() || !rDoc.Security.IsEmpty() {
			checkSecurity(lDoc.Security, rDoc.Security, &changes, dc)
		}

		// components / definitions
		// swagger (damn you) decided to put all this stuff at the document root, rather than cleanly
		// placing it under a parent, like they did with OpenAPI. This means picking through each definition
		// creating a new set of changes and then morphing them into a single changes object.
		cc := new(ComponentsChanges)
		cc.PropertyChanges = new(PropertyChanges)
		if n := CompareComponents(lDoc.Definitions.Value, rDoc.Definitions.Value); n != nil {
			cc.SchemaChanges = n.SchemaChanges
		}
		if n := CompareComponents(lDoc.SecurityDefinitions.Value, rDoc.SecurityDefinitions.Value); n != nil {
			cc.SecuritySchemeChanges = n.SecuritySchemeChanges
		}
		if n := CompareComponents(lDoc.Parameters.Value, rDoc.Parameters.Value); n != nil {
			cc.PropertyChanges.Changes = append(cc.PropertyChanges.Changes, n.Changes...)
		}
		if n := CompareComponents(lDoc.Responses.Value, rDoc.Responses.Value); n != nil {
			cc.Changes = append(cc.Changes, n.Changes...)
		}
		dc.ExtensionChanges = CompareExtensions(lDoc.Extensions, rDoc.Extensions)
		if cc.TotalChanges() > 0 {
			dc.ComponentsChanges = cc
		}
	}

	if reflect.TypeOf(&v3.Document{}) == reflect.TypeOf(l) && reflect.TypeOf(&v3.Document{}) == reflect.TypeOf(r) {
		lDoc := l.(*v3.Document)
		rDoc := r.(*v3.Document)

		// version
		addPropertyCheck(&props, lDoc.Version.ValueNode, rDoc.Version.ValueNode,
			lDoc.Version.Value, rDoc.Version.Value, &changes, v3.OpenAPILabel,
			BreakingModified(CompOpenAPI, ""), CompOpenAPI, "")

		// schema dialect
		addPropertyCheck(&props, lDoc.JsonSchemaDialect.ValueNode, rDoc.JsonSchemaDialect.ValueNode,
			lDoc.JsonSchemaDialect.Value, rDoc.JsonSchemaDialect.Value, &changes, v3.JSONSchemaDialectLabel,
			BreakingModified(CompJSONSchemaDialect, ""), CompJSONSchemaDialect, "")

		// $self field (3.2+)
		addPropertyCheck(&props, lDoc.Self.ValueNode, rDoc.Self.ValueNode,
			lDoc.Self.Value, rDoc.Self.Value, &changes, v3.SelfLabel,
			BreakingModified(CompSelf, ""), CompSelf, "")

		// tags
		dc.TagChanges = CompareTags(lDoc.Tags.Value, rDoc.Tags.Value)

		// paths
		if !lDoc.Paths.IsEmpty() || !rDoc.Paths.IsEmpty() {
			dc.PathsChanges = ComparePaths(lDoc.Paths.Value, rDoc.Paths.Value)
		}

		// external docs
		compareDocumentExternalDocs(lDoc, rDoc, dc, &changes)

		// info
		compareDocumentInfo(&lDoc.Info, &rDoc.Info, dc, &changes)

		// security
		if !lDoc.Security.IsEmpty() || !rDoc.Security.IsEmpty() {
			checkSecurity(lDoc.Security, rDoc.Security, &changes, dc)
		}

		// compare components.
		if !lDoc.Components.IsEmpty() && !rDoc.Components.IsEmpty() {
			if n := CompareComponents(lDoc.Components.Value, rDoc.Components.Value); n != nil {
				dc.ComponentsChanges = n
			}
		}
		if !lDoc.Components.IsEmpty() && rDoc.Components.IsEmpty() {
			CreateChange(&changes, PropertyRemoved, v3.ComponentsLabel,
				lDoc.Components.ValueNode, nil, BreakingRemoved(CompComponents, ""), lDoc.Components.Value, nil)
		}
		if lDoc.Components.IsEmpty() && !rDoc.Components.IsEmpty() {
			CreateChange(&changes, PropertyAdded, v3.ComponentsLabel,
				nil, rDoc.Components.ValueNode, BreakingAdded(CompComponents, ""), nil, lDoc.Components.Value)
		}

		// compare servers
		if n := checkServers(lDoc.Servers, rDoc.Servers, CompServers, ""); n != nil {
			dc.ServerChanges = n
		}

		// compare webhooks
		dc.WebhookChanges = CheckMapForChanges(lDoc.Webhooks.Value, rDoc.Webhooks.Value, &changes,
			v3.WebhooksLabel, ComparePathItemsV3)

		// extensions
		dc.ExtensionChanges = CompareExtensions(lDoc.Extensions, rDoc.Extensions)
	}

	CheckProperties(props)
	dc.PropertyChanges = NewPropertyChanges(changes)
	if dc.TotalChanges() <= 0 {
		return nil
	}
	base.SchemaQuickHashMap.Clear()
	return dc
}

func compareDocumentExternalDocs(l, r low.HasExternalDocs, dc *DocumentChanges, changes *[]*Change) {
	// external docs
	if !l.GetExternalDocs().IsEmpty() && !r.GetExternalDocs().IsEmpty() {
		lExtDoc := l.GetExternalDocs().Value.(*base.ExternalDoc)
		rExtDoc := r.GetExternalDocs().Value.(*base.ExternalDoc)
		if !low.AreEqual(lExtDoc, rExtDoc) {
			dc.ExternalDocChanges = CompareExternalDocs(lExtDoc, rExtDoc)
		}
	}
	if l.GetExternalDocs().IsEmpty() && !r.GetExternalDocs().IsEmpty() {
		CreateChange(changes, PropertyAdded, v3.ExternalDocsLabel,
			nil, r.GetExternalDocs().ValueNode, false, nil,
			r.GetExternalDocs().Value)
	}
	if !l.GetExternalDocs().IsEmpty() && r.GetExternalDocs().IsEmpty() {
		CreateChange(changes, PropertyRemoved, v3.ExternalDocsLabel,
			l.GetExternalDocs().ValueNode, nil, false, l.GetExternalDocs().Value,
			nil)
	}
}

func compareDocumentInfo(l, r *low.NodeReference[*base.Info], dc *DocumentChanges, changes *[]*Change) {
	// info
	if !l.IsEmpty() && !r.IsEmpty() {
		lInfo := l.Value
		rInfo := r.Value
		if !low.AreEqual(lInfo, rInfo) {
			dc.InfoChanges = CompareInfo(lInfo, rInfo)
		}
	}
	if l.IsEmpty() && !r.IsEmpty() {
		CreateChange(changes, PropertyAdded, v3.InfoLabel,
			nil, r.ValueNode, false, nil,
			r.Value)
	}
	if !l.IsEmpty() && r.IsEmpty() {
		CreateChange(changes, PropertyRemoved, v3.InfoLabel,
			l.ValueNode, nil, false, l.Value,
			nil)
	}
}
