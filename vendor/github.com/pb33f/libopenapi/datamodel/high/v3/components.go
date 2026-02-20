// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package v3

import (
	"sync"

	"github.com/pb33f/libopenapi/datamodel"
	"github.com/pb33f/libopenapi/datamodel/high"
	highbase "github.com/pb33f/libopenapi/datamodel/high/base"
	lowmodel "github.com/pb33f/libopenapi/datamodel/low"
	"github.com/pb33f/libopenapi/datamodel/low/base"
	low "github.com/pb33f/libopenapi/datamodel/low/v3"
	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
)

// Components represents a high-level OpenAPI 3+ Components Object, that is backed by a low-level one.
//
// Holds a set of reusable objects for different aspects of the OAS. All objects defined within the components object
// will have no effect on the API unless they are explicitly referenced from properties outside the components object.
//   - https://spec.openapis.org/oas/v3.1.0#components-object
type Components struct {
	Schemas         *orderedmap.Map[string, *highbase.SchemaProxy] `json:"schemas,omitempty" yaml:"schemas,omitempty"`
	Responses       *orderedmap.Map[string, *Response]             `json:"responses,omitempty" yaml:"responses,omitempty"`
	Parameters      *orderedmap.Map[string, *Parameter]            `json:"parameters,omitempty" yaml:"parameters,omitempty"`
	Examples        *orderedmap.Map[string, *highbase.Example]     `json:"examples,omitempty" yaml:"examples,omitempty"`
	RequestBodies   *orderedmap.Map[string, *RequestBody]          `json:"requestBodies,omitempty" yaml:"requestBodies,omitempty"`
	Headers         *orderedmap.Map[string, *Header]               `json:"headers,omitempty" yaml:"headers,omitempty"`
	SecuritySchemes *orderedmap.Map[string, *SecurityScheme]       `json:"securitySchemes,omitempty" yaml:"securitySchemes,omitempty"`
	Links           *orderedmap.Map[string, *Link]                 `json:"links,omitempty" yaml:"links,omitempty"`
	Callbacks       *orderedmap.Map[string, *Callback]             `json:"callbacks,omitempty" yaml:"callbacks,omitempty"`
	PathItems       *orderedmap.Map[string, *PathItem]             `json:"pathItems,omitempty" yaml:"pathItems,omitempty"`
	MediaTypes      *orderedmap.Map[string, *MediaType]            `json:"mediaTypes,omitempty" yaml:"mediaTypes,omitempty"` // OpenAPI 3.2+ mediaTypes section
	Extensions      *orderedmap.Map[string, *yaml.Node]            `json:"-" yaml:"-"`
	low             *low.Components
}

// NewComponents will create new high-level instance of Components from a low-level one. Components can be considerable
// in scope, with a lot of different properties across different categories. All components are built asynchronously
// in order to keep things fast.
func NewComponents(comp *low.Components) *Components {
	c := new(Components)
	c.low = comp
	if orderedmap.Len(comp.Extensions) > 0 {
		c.Extensions = high.ExtractExtensions(comp.Extensions)
	}
	cbMap := orderedmap.New[string, *Callback]()
	linkMap := orderedmap.New[string, *Link]()
	responseMap := orderedmap.New[string, *Response]()
	parameterMap := orderedmap.New[string, *Parameter]()
	exampleMap := orderedmap.New[string, *highbase.Example]()
	requestBodyMap := orderedmap.New[string, *RequestBody]()
	headerMap := orderedmap.New[string, *Header]()
	pathItemMap := orderedmap.New[string, *PathItem]()
	securitySchemeMap := orderedmap.New[string, *SecurityScheme]()
	mediaTypesMap := orderedmap.New[string, *MediaType]()
	schemas := orderedmap.New[string, *highbase.SchemaProxy]()

	// build all components asynchronously.
	var wg sync.WaitGroup
	wg.Add(11)
	go func() {
		buildComponent[*low.Callback, *Callback](comp.Callbacks.Value, cbMap, NewCallback)
		wg.Done()
	}()
	go func() {
		buildComponent[*low.Link, *Link](comp.Links.Value, linkMap, NewLink)
		wg.Done()
	}()
	go func() {
		buildComponent[*low.Response, *Response](comp.Responses.Value, responseMap, NewResponse)
		wg.Done()
	}()
	go func() {
		buildComponent[*low.Parameter, *Parameter](comp.Parameters.Value, parameterMap, NewParameter)
		wg.Done()
	}()
	go func() {
		buildComponent[*base.Example, *highbase.Example](comp.Examples.Value, exampleMap, highbase.NewExample)
		wg.Done()
	}()
	go func() {
		buildComponent[*low.RequestBody, *RequestBody](comp.RequestBodies.Value, requestBodyMap, NewRequestBody)
		wg.Done()
	}()
	go func() {
		buildComponent[*low.Header, *Header](comp.Headers.Value, headerMap, NewHeader)
		wg.Done()
	}()
	go func() {
		buildComponent[*low.PathItem, *PathItem](comp.PathItems.Value, pathItemMap, NewPathItem)
		wg.Done()
	}()
	go func() {
		buildComponent[*low.SecurityScheme, *SecurityScheme](comp.SecuritySchemes.Value, securitySchemeMap, NewSecurityScheme)
		wg.Done()
	}()
	go func() {
		buildSchema(comp.Schemas.Value, schemas)
		wg.Done()
	}()
	go func() {
		buildComponent[*low.MediaType, *MediaType](comp.MediaTypes.Value, mediaTypesMap, NewMediaType)
		wg.Done()
	}()

	wg.Wait()
	c.Schemas = schemas
	c.Callbacks = cbMap
	c.Links = linkMap
	c.Parameters = parameterMap
	c.Headers = headerMap
	c.Responses = responseMap
	c.RequestBodies = requestBodyMap
	c.Examples = exampleMap
	c.SecuritySchemes = securitySchemeMap
	c.PathItems = pathItemMap
	c.MediaTypes = mediaTypesMap
	return c
}

// contains a component build result.
type componentResult[T any] struct {
	res T
	key string
}

// buildComponent builds component structs from low level structs.
func buildComponent[IN any, OUT any](inMap *orderedmap.Map[lowmodel.KeyReference[string], lowmodel.ValueReference[IN]], outMap *orderedmap.Map[string, OUT], translateItem func(IN) OUT) {
	translateFunc := func(pair orderedmap.Pair[lowmodel.KeyReference[string], lowmodel.ValueReference[IN]]) (componentResult[OUT], error) {
		return componentResult[OUT]{key: pair.Key().Value, res: translateItem(pair.Value().Value)}, nil
	}
	resultFunc := func(value componentResult[OUT]) error {
		outMap.Set(value.key, value.res)
		return nil
	}
	_ = datamodel.TranslateMapParallel(inMap, translateFunc, resultFunc)
}

// buildSchema builds a schema from low level structs.
func buildSchema(inMap *orderedmap.Map[lowmodel.KeyReference[string], lowmodel.ValueReference[*base.SchemaProxy]], outMap *orderedmap.Map[string, *highbase.SchemaProxy]) {
	translateFunc := func(pair orderedmap.Pair[lowmodel.KeyReference[string], lowmodel.ValueReference[*base.SchemaProxy]]) (componentResult[*highbase.SchemaProxy], error) {
		value := pair.Value()
		sch := highbase.NewSchemaProxy(&lowmodel.NodeReference[*base.SchemaProxy]{
			Value:     value.Value,
			ValueNode: value.ValueNode,
		})
		return componentResult[*highbase.SchemaProxy]{res: sch, key: pair.Key().Value}, nil
	}
	resultFunc := func(value componentResult[*highbase.SchemaProxy]) error {
		outMap.Set(value.key, value.res)
		return nil
	}
	_ = datamodel.TranslateMapParallel(inMap, translateFunc, resultFunc)
}

// GoLow returns the low-level Components instance used to create the high-level one.
func (c *Components) GoLow() *low.Components {
	return c.low
}

// GoLowUntyped returns the low-level Components instance used to create the high-level one as an interface{}.
func (c *Components) GoLowUntyped() any {
	return c.low
}

// Render will return a YAML representation of the Components object as a byte slice.
func (c *Components) Render() ([]byte, error) {
	return yaml.Marshal(c)
}

// MarshalYAML will create a ready to render YAML representation of the Response object.
func (c *Components) MarshalYAML() (interface{}, error) {
	nb := high.NewNodeBuilder(c, c.low)
	return nb.Render(), nil
}

// RenderInline will return a YAML representation of the Components object as a byte slice with references resolved.
func (c *Components) RenderInline() ([]byte, error) {
	d, _ := c.MarshalYAMLInline()
	return yaml.Marshal(d)
}

// MarshalYAMLInline will create a ready to render YAML representation of the Components object with references resolved.
func (c *Components) MarshalYAMLInline() (interface{}, error) {
	nb := high.NewNodeBuilder(c, c.low)
	nb.Resolve = true
	return nb.Render(), nil
}
