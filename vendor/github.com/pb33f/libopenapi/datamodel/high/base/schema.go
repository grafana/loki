// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package base

import (
	"encoding/json"

	"errors"

	"github.com/pb33f/libopenapi/datamodel/high"
	lowmodel "github.com/pb33f/libopenapi/datamodel/low"
	"github.com/pb33f/libopenapi/datamodel/low/base"
	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
)

// Schema represents a JSON Schema that support Swagger, OpenAPI 3 and OpenAPI 3.1
//
// Until 3.1 OpenAPI had a strange relationship with JSON Schema. It's been a super-set/sub-set
// mix, which has been confusing. So, instead of building a bunch of different models, we have compressed
// all variations into a single model that makes it easy to support multiple spec types.
//
//   - v2 schema: https://swagger.io/specification/v2/#schemaObject
//   - v3 schema: https://swagger.io/specification/#schema-object
//   - v3.1 schema: https://spec.openapis.org/oas/v3.1.0#schema-object
type Schema struct {
	// 3.1 only, used to define a dialect for this schema, label is '$schema'.
	SchemaTypeRef string `json:"$schema,omitempty" yaml:"$schema,omitempty"`

	// In versions 2 and 3.0, this ExclusiveMaximum can only be a boolean.
	// In version 3.1, ExclusiveMaximum is a number.
	ExclusiveMaximum *DynamicValue[bool, float64] `json:"exclusiveMaximum,omitempty" yaml:"exclusiveMaximum,omitempty"`

	// In versions 2 and 3.0, this ExclusiveMinimum can only be a boolean.
	// In version 3.1, ExclusiveMinimum is a number.
	ExclusiveMinimum *DynamicValue[bool, float64] `json:"exclusiveMinimum,omitempty" yaml:"exclusiveMinimum,omitempty"`

	// In versions 2 and 3.0, this Type is a single value, so array will only ever have one value
	// in version 3.1, Type can be multiple values
	Type []string `json:"type,omitempty" yaml:"type,omitempty"`

	// Schemas are resolved on demand using a SchemaProxy
	AllOf []*SchemaProxy `json:"allOf,omitempty" yaml:"allOf,omitempty"`

	// Polymorphic Schemas are only available in version 3+
	OneOf         []*SchemaProxy `json:"oneOf,omitempty" yaml:"oneOf,omitempty"`
	AnyOf         []*SchemaProxy `json:"anyOf,omitempty" yaml:"anyOf,omitempty"`
	Discriminator *Discriminator `json:"discriminator,omitempty" yaml:"discriminator,omitempty"`

	// in 3.1 examples can be an array (which is recommended)
	Examples []*yaml.Node `json:"examples,omitempty" yaml:"examples,omitempty"`

	// in 3.1 prefixItems provides tuple validation support.
	PrefixItems []*SchemaProxy `json:"prefixItems,omitempty" yaml:"prefixItems,omitempty"`

	// 3.1 Specific properties
	Contains          *SchemaProxy                          `json:"contains,omitempty" yaml:"contains,omitempty"`
	MinContains       *int64                                `json:"minContains,omitempty" yaml:"minContains,omitempty"`
	MaxContains       *int64                                `json:"maxContains,omitempty" yaml:"maxContains,omitempty"`
	If                *SchemaProxy                          `json:"if,omitempty" yaml:"if,omitempty"`
	Else              *SchemaProxy                          `json:"else,omitempty" yaml:"else,omitempty"`
	Then              *SchemaProxy                          `json:"then,omitempty" yaml:"then,omitempty"`
	DependentSchemas  *orderedmap.Map[string, *SchemaProxy] `json:"dependentSchemas,omitempty" yaml:"dependentSchemas,omitempty"`
	DependentRequired *orderedmap.Map[string, []string]     `json:"dependentRequired,omitempty" yaml:"dependentRequired,omitempty"`
	PatternProperties *orderedmap.Map[string, *SchemaProxy] `json:"patternProperties,omitempty" yaml:"patternProperties,omitempty"`
	PropertyNames     *SchemaProxy                          `json:"propertyNames,omitempty" yaml:"propertyNames,omitempty"`
	UnevaluatedItems  *SchemaProxy                          `json:"unevaluatedItems,omitempty" yaml:"unevaluatedItems,omitempty"`

	// in 3.1 UnevaluatedProperties can be a Schema or a boolean
	// https://github.com/pb33f/libopenapi/issues/118
	UnevaluatedProperties *DynamicValue[*SchemaProxy, bool] `json:"unevaluatedProperties,omitempty" yaml:"unevaluatedProperties,omitempty"`

	// in 3.1 Items can be a Schema or a boolean
	Items *DynamicValue[*SchemaProxy, bool] `json:"items,omitempty" yaml:"items,omitempty"`

	// 3.1+ only, JSON Schema 2020-12 $id - declares this schema as a schema resource with a URI identifier
	Id string `json:"$id,omitempty" yaml:"$id,omitempty"`

	// 3.1 only, part of the JSON Schema spec provides a way to identify a sub-schema
	Anchor string `json:"$anchor,omitempty" yaml:"$anchor,omitempty"`

	// 3.1+ only, JSON Schema 2020-12 dynamic anchor for recursive schema resolution
	DynamicAnchor string `json:"$dynamicAnchor,omitempty" yaml:"$dynamicAnchor,omitempty"`

	// 3.1+ only, JSON Schema 2020-12 dynamic reference for recursive schema resolution
	DynamicRef string `json:"$dynamicRef,omitempty" yaml:"$dynamicRef,omitempty"`

	// 3.1+ only, JSON Schema 2020-12 $comment - explanatory notes without affecting validation
	Comment string `json:"$comment,omitempty" yaml:"$comment,omitempty"`

	// 3.1+ only, JSON Schema 2020-12 contentSchema - describes structure of decoded content
	ContentSchema *SchemaProxy `json:"contentSchema,omitempty" yaml:"contentSchema,omitempty"`

	// 3.1+ only, JSON Schema 2020-12 $vocabulary - defines available vocabularies in meta-schemas
	Vocabulary *orderedmap.Map[string, bool] `json:"$vocabulary,omitempty" yaml:"$vocabulary,omitempty"`

	// Compatible with all versions
	Not                  *SchemaProxy                          `json:"not,omitempty" yaml:"not,omitempty"`
	Properties           *orderedmap.Map[string, *SchemaProxy] `json:"properties,omitempty" yaml:"properties,omitempty"`
	Title                string                                `json:"title,omitempty" yaml:"title,omitempty"`
	MultipleOf           *float64                              `json:"multipleOf,omitempty" yaml:"multipleOf,omitempty"`
	Maximum              *float64                              `json:"maximum,renderZero,omitempty" yaml:"maximum,renderZero,omitempty"`
	Minimum              *float64                              `json:"minimum,renderZero,omitempty," yaml:"minimum,renderZero,omitempty"`
	MaxLength            *int64                                `json:"maxLength,omitempty" yaml:"maxLength,omitempty"`
	MinLength            *int64                                `json:"minLength,omitempty" yaml:"minLength,omitempty"`
	Pattern              string                                `json:"pattern,omitempty" yaml:"pattern,omitempty"`
	Format               string                                `json:"format,omitempty" yaml:"format,omitempty"`
	MaxItems             *int64                                `json:"maxItems,omitempty" yaml:"maxItems,omitempty"`
	MinItems             *int64                                `json:"minItems,omitempty" yaml:"minItems,omitempty"`
	UniqueItems          *bool                                 `json:"uniqueItems,omitempty" yaml:"uniqueItems,omitempty"`
	MaxProperties        *int64                                `json:"maxProperties,omitempty" yaml:"maxProperties,omitempty"`
	MinProperties        *int64                                `json:"minProperties,omitempty" yaml:"minProperties,omitempty"`
	Required             []string                              `json:"required,omitempty" yaml:"required,omitempty"`
	Enum                 []*yaml.Node                          `json:"enum,omitempty" yaml:"enum,omitempty"`
	AdditionalProperties *DynamicValue[*SchemaProxy, bool]     `json:"additionalProperties,renderZero,omitempty" yaml:"additionalProperties,renderZero,omitempty"`
	Description          string                                `json:"description,omitempty" yaml:"description,omitempty"`
	ContentEncoding      string                                `json:"contentEncoding,omitempty" yaml:"contentEncoding,omitempty"`
	ContentMediaType     string                                `json:"contentMediaType,omitempty" yaml:"contentMediaType,omitempty"`
	Default              *yaml.Node                            `json:"default,omitempty" yaml:"default,renderZero,omitempty"`
	Const                *yaml.Node                            `json:"const,omitempty" yaml:"const,renderZero,omitempty"`
	Nullable             *bool                                 `json:"nullable,omitempty" yaml:"nullable,omitempty"`
	ReadOnly             *bool                                 `json:"readOnly,renderZero,omitempty" yaml:"readOnly,renderZero,omitempty"`   // https://github.com/pb33f/libopenapi/issues/30
	WriteOnly            *bool                                 `json:"writeOnly,renderZero,omitempty" yaml:"writeOnly,renderZero,omitempty"` // https://github.com/pb33f/libopenapi/issues/30
	XML                  *XML                                  `json:"xml,omitempty" yaml:"xml,omitempty"`
	ExternalDocs         *ExternalDoc                          `json:"externalDocs,omitempty" yaml:"externalDocs,omitempty"`
	Example              *yaml.Node                            `json:"example,omitempty" yaml:"example,omitempty"`
	Deprecated           *bool                                 `json:"deprecated,omitempty" yaml:"deprecated,omitempty"`
	Extensions           *orderedmap.Map[string, *yaml.Node]   `json:"-" yaml:"-"`
	low                  *base.Schema

	// Parent Proxy refers back to the low level SchemaProxy that is proxying this schema.
	ParentProxy *SchemaProxy `json:"-" yaml:"-"`
}

// NewSchema will create a new high-level schema from a low-level one.
func NewSchema(schema *base.Schema) *Schema {
	s := new(Schema)
	s.low = schema
	s.Title = schema.Title.Value
	if !schema.SchemaTypeRef.IsEmpty() {
		s.SchemaTypeRef = schema.SchemaTypeRef.Value
	}
	if !schema.MultipleOf.IsEmpty() {
		s.MultipleOf = &schema.MultipleOf.Value
	}
	if !schema.Maximum.IsEmpty() {
		s.Maximum = &schema.Maximum.Value
	}
	if !schema.Minimum.IsEmpty() {
		s.Minimum = &schema.Minimum.Value
	}
	// if we're dealing with a 3.0 spec using a bool
	if !schema.ExclusiveMaximum.IsEmpty() && schema.ExclusiveMaximum.Value.IsA() {
		s.ExclusiveMaximum = &DynamicValue[bool, float64]{
			A: schema.ExclusiveMaximum.Value.A,
		}
	}
	// if we're dealing with a 3.1 spec using an int
	if !schema.ExclusiveMaximum.IsEmpty() && schema.ExclusiveMaximum.Value.IsB() {
		s.ExclusiveMaximum = &DynamicValue[bool, float64]{
			N: 1,
			B: schema.ExclusiveMaximum.Value.B,
		}
	}
	// if we're dealing with a 3.0 spec using a bool
	if !schema.ExclusiveMinimum.IsEmpty() && schema.ExclusiveMinimum.Value.IsA() {
		s.ExclusiveMinimum = &DynamicValue[bool, float64]{
			A: schema.ExclusiveMinimum.Value.A,
		}
	}
	// if we're dealing with a 3.1 spec, using an int
	if !schema.ExclusiveMinimum.IsEmpty() && schema.ExclusiveMinimum.Value.IsB() {
		s.ExclusiveMinimum = &DynamicValue[bool, float64]{
			N: 1,
			B: schema.ExclusiveMinimum.Value.B,
		}
	}
	if !schema.MaxLength.IsEmpty() {
		s.MaxLength = &schema.MaxLength.Value
	}
	if !schema.MinLength.IsEmpty() {
		s.MinLength = &schema.MinLength.Value
	}
	if !schema.MaxItems.IsEmpty() {
		s.MaxItems = &schema.MaxItems.Value
	}
	if !schema.MinItems.IsEmpty() {
		s.MinItems = &schema.MinItems.Value
	}
	if !schema.MaxProperties.IsEmpty() {
		s.MaxProperties = &schema.MaxProperties.Value
	}
	if !schema.MinProperties.IsEmpty() {
		s.MinProperties = &schema.MinProperties.Value
	}

	if !schema.MaxContains.IsEmpty() {
		s.MaxContains = &schema.MaxContains.Value
	}
	if !schema.MinContains.IsEmpty() {
		s.MinContains = &schema.MinContains.Value
	}
	if !schema.UniqueItems.IsEmpty() {
		s.UniqueItems = &schema.UniqueItems.Value
	}
	if !schema.Contains.IsEmpty() {
		s.Contains = NewSchemaProxy(&lowmodel.NodeReference[*base.SchemaProxy]{
			ValueNode: schema.Contains.ValueNode,
			Value:     schema.Contains.Value,
		})
	}
	if !schema.If.IsEmpty() {
		s.If = NewSchemaProxy(&lowmodel.NodeReference[*base.SchemaProxy]{
			ValueNode: schema.If.ValueNode,
			Value:     schema.If.Value,
		})
	}
	if !schema.Else.IsEmpty() {
		s.Else = NewSchemaProxy(&lowmodel.NodeReference[*base.SchemaProxy]{
			ValueNode: schema.Else.ValueNode,
			Value:     schema.Else.Value,
		})
	}
	if !schema.Then.IsEmpty() {
		s.Then = NewSchemaProxy(&lowmodel.NodeReference[*base.SchemaProxy]{
			ValueNode: schema.Then.ValueNode,
			Value:     schema.Then.Value,
		})
	}
	if !schema.PropertyNames.IsEmpty() {
		s.PropertyNames = NewSchemaProxy(&lowmodel.NodeReference[*base.SchemaProxy]{
			ValueNode: schema.PropertyNames.ValueNode,
			Value:     schema.PropertyNames.Value,
		})
	}
	if !schema.UnevaluatedItems.IsEmpty() {
		s.UnevaluatedItems = NewSchemaProxy(&lowmodel.NodeReference[*base.SchemaProxy]{
			ValueNode: schema.UnevaluatedItems.ValueNode,
			Value:     schema.UnevaluatedItems.Value,
		})
	}

	var unevaluatedProperties *DynamicValue[*SchemaProxy, bool]
	if !schema.UnevaluatedProperties.IsEmpty() {
		if schema.UnevaluatedProperties.Value.IsA() {
			unevaluatedProperties = &DynamicValue[*SchemaProxy, bool]{
				A: NewSchemaProxy(&lowmodel.NodeReference[*base.SchemaProxy]{
					ValueNode: schema.UnevaluatedProperties.ValueNode,
					Value:     schema.UnevaluatedProperties.Value.A,
					KeyNode:   schema.UnevaluatedProperties.KeyNode,
				}),
			}
		} else {
			unevaluatedProperties = &DynamicValue[*SchemaProxy, bool]{N: 1, B: schema.UnevaluatedProperties.Value.B}
		}
	}
	s.UnevaluatedProperties = unevaluatedProperties

	s.Pattern = schema.Pattern.Value
	s.Format = schema.Format.Value

	// 3.0 spec is a single value
	if !schema.Type.IsEmpty() && schema.Type.Value.IsA() {
		s.Type = []string{schema.Type.Value.A}
	}
	// 3.1 spec may have multiple values
	if !schema.Type.IsEmpty() && schema.Type.Value.IsB() {
		for i := range schema.Type.Value.B {
			s.Type = append(s.Type, schema.Type.Value.B[i].Value)
		}
	}

	var additionalProperties *DynamicValue[*SchemaProxy, bool]
	if !schema.AdditionalProperties.IsEmpty() {
		if schema.AdditionalProperties.Value.IsA() {
			additionalProperties = &DynamicValue[*SchemaProxy, bool]{
				A: NewSchemaProxy(&lowmodel.NodeReference[*base.SchemaProxy]{
					ValueNode: schema.AdditionalProperties.ValueNode,
					Value:     schema.AdditionalProperties.Value.A,
					KeyNode:   schema.AdditionalProperties.KeyNode,
				}),
			}
		} else {
			additionalProperties = &DynamicValue[*SchemaProxy, bool]{N: 1, B: schema.AdditionalProperties.Value.B}
		}
	}
	s.AdditionalProperties = additionalProperties

	s.Description = schema.Description.Value
	s.ContentEncoding = schema.ContentEncoding.Value
	s.ContentMediaType = schema.ContentMediaType.Value
	s.Default = schema.Default.Value
	s.Const = schema.Const.Value
	if !schema.Nullable.IsEmpty() {
		s.Nullable = &schema.Nullable.Value
	}
	if !schema.ReadOnly.IsEmpty() {
		s.ReadOnly = &schema.ReadOnly.Value
	}
	if !schema.WriteOnly.IsEmpty() {
		s.WriteOnly = &schema.WriteOnly.Value
	}
	if !schema.Deprecated.IsEmpty() {
		s.Deprecated = &schema.Deprecated.Value
	}
	s.Example = schema.Example.Value
	if len(schema.Examples.Value) > 0 {
		examples := make([]*yaml.Node, len(schema.Examples.Value))
		for i := 0; i < len(schema.Examples.Value); i++ {
			examples[i] = schema.Examples.Value[i].Value
		}
		s.Examples = examples
	}
	s.Extensions = high.ExtractExtensions(schema.Extensions)
	if !schema.Discriminator.IsEmpty() {
		s.Discriminator = NewDiscriminator(schema.Discriminator.Value)
	}
	if !schema.XML.IsEmpty() {
		s.XML = NewXML(schema.XML.Value)
	}
	if !schema.ExternalDocs.IsEmpty() {
		s.ExternalDocs = NewExternalDoc(schema.ExternalDocs.Value)
	}
	var req []string
	for i := range schema.Required.Value {
		req = append(req, schema.Required.Value[i].Value)
	}
	s.Required = req

	if !schema.Id.IsEmpty() {
		s.Id = schema.Id.Value
	}
	if !schema.Anchor.IsEmpty() {
		s.Anchor = schema.Anchor.Value
	}
	if !schema.DynamicAnchor.IsEmpty() {
		s.DynamicAnchor = schema.DynamicAnchor.Value
	}
	if !schema.DynamicRef.IsEmpty() {
		s.DynamicRef = schema.DynamicRef.Value
	}
	if !schema.Comment.IsEmpty() {
		s.Comment = schema.Comment.Value
	}
	if !schema.ContentSchema.IsEmpty() {
		s.ContentSchema = NewSchemaProxy(&lowmodel.NodeReference[*base.SchemaProxy]{
			ValueNode: schema.ContentSchema.ValueNode,
			Value:     schema.ContentSchema.Value,
		})
	}
	if schema.Vocabulary.Value != nil {
		vocabularyMap := orderedmap.New[string, bool]()
		for k, v := range schema.Vocabulary.Value.FromOldest() {
			vocabularyMap.Set(k.Value, v.Value)
		}
		s.Vocabulary = vocabularyMap
	}

	var enum []*yaml.Node
	for i := range schema.Enum.Value {
		enum = append(enum, schema.Enum.Value[i].Value)
	}
	s.Enum = enum

	// async work.
	// any polymorphic properties need to be handled in their own threads
	// any properties each need to be processed in their own thread.
	// we go as fast as we can.
	polyCompletedChan := make(chan struct{})
	errChan := make(chan error)

	type buildResult struct {
		idx int
		s   *SchemaProxy
	}

	// for every item, build schema async
	buildSchema := func(sch lowmodel.ValueReference[*base.SchemaProxy], idx int, bChan chan buildResult) {
		n := &lowmodel.NodeReference[*base.SchemaProxy]{
			ValueNode: sch.ValueNode,
			Value:     sch.Value,
		}
		n.SetReference(sch.GetReference(), sch.GetReferenceNode())

		p := NewSchemaProxy(n)

		bChan <- buildResult{idx: idx, s: p}
	}

	// schema async
	buildOutSchemas := func(schemas []lowmodel.ValueReference[*base.SchemaProxy], items *[]*SchemaProxy,
		doneChan chan struct{}, e chan error,
	) {
		bChan := make(chan buildResult)
		totalSchemas := len(schemas)
		for i := range schemas {
			go buildSchema(schemas[i], i, bChan)
		}
		j := 0
		for j < totalSchemas {
			r := <-bChan
			j++
			(*items)[r.idx] = r.s
		}
		doneChan <- struct{}{}
	}

	// props async
	buildProps := func(k lowmodel.KeyReference[string], v lowmodel.ValueReference[*base.SchemaProxy],
		props *orderedmap.Map[string, *SchemaProxy], sw int,
	) {
		props.Set(k.Value, NewSchemaProxy(&lowmodel.NodeReference[*base.SchemaProxy]{
			Value:     v.Value,
			KeyNode:   k.KeyNode,
			ValueNode: v.ValueNode,
		}))

		switch sw {
		case 0:
			s.Properties = props
		case 1:
			s.DependentSchemas = props
		case 2:
			s.PatternProperties = props
		}
	}

	props := orderedmap.New[string, *SchemaProxy]()
	for name, schemaProxy := range schema.Properties.Value.FromOldest() {
		buildProps(name, schemaProxy, props, 0)
	}

	dependents := orderedmap.New[string, *SchemaProxy]()
	for name, schemaProxy := range schema.DependentSchemas.Value.FromOldest() {
		buildProps(name, schemaProxy, dependents, 1)
	}

	// Handle DependentRequired
	if schema.DependentRequired.Value != nil {
		depRequired := orderedmap.New[string, []string]()
		for prop, requiredProps := range schema.DependentRequired.Value.FromOldest() {
			depRequired.Set(prop.Value, requiredProps.Value)
		}
		s.DependentRequired = depRequired
	}

	patternProps := orderedmap.New[string, *SchemaProxy]()
	for name, schemaProxy := range schema.PatternProperties.Value.FromOldest() {
		buildProps(name, schemaProxy, patternProps, 2)
	}

	var allOf []*SchemaProxy
	var oneOf []*SchemaProxy
	var anyOf []*SchemaProxy
	var not *SchemaProxy
	var items *DynamicValue[*SchemaProxy, bool]
	var prefixItems []*SchemaProxy

	children := 0
	if !schema.AllOf.IsEmpty() {
		children++
		allOf = make([]*SchemaProxy, len(schema.AllOf.Value))
		go buildOutSchemas(schema.AllOf.Value, &allOf, polyCompletedChan, errChan)
	}
	if !schema.AnyOf.IsEmpty() {
		children++
		anyOf = make([]*SchemaProxy, len(schema.AnyOf.Value))
		go buildOutSchemas(schema.AnyOf.Value, &anyOf, polyCompletedChan, errChan)
	}
	if !schema.OneOf.IsEmpty() {
		children++
		oneOf = make([]*SchemaProxy, len(schema.OneOf.Value))
		go buildOutSchemas(schema.OneOf.Value, &oneOf, polyCompletedChan, errChan)
	}
	if !schema.Not.IsEmpty() {
		not = NewSchemaProxy(&schema.Not)
	}
	if !schema.Items.IsEmpty() {
		if schema.Items.Value.IsA() {
			items = &DynamicValue[*SchemaProxy, bool]{
				A: NewSchemaProxy(&lowmodel.NodeReference[*base.SchemaProxy]{
					ValueNode: schema.Items.ValueNode,
					Value:     schema.Items.Value.A,
					KeyNode:   schema.Items.KeyNode,
				},
				),
			}
		} else {
			items = &DynamicValue[*SchemaProxy, bool]{N: 1, B: schema.Items.Value.B}
		}
	}
	if !schema.PrefixItems.IsEmpty() {
		children++
		prefixItems = make([]*SchemaProxy, len(schema.PrefixItems.Value))
		go buildOutSchemas(schema.PrefixItems.Value, &prefixItems, polyCompletedChan, errChan)
	}

	completeChildren := 0
	if children > 0 {
	allDone:
		for {
			<-polyCompletedChan
			completeChildren++
			if children == completeChildren {
				break allDone
			}
		}
	}
	s.OneOf = oneOf
	s.AnyOf = anyOf
	s.AllOf = allOf
	s.Items = items
	s.PrefixItems = prefixItems
	s.Not = not
	return s
}

// GoLow will return the low-level instance of Schema that was used to create the high level one.
func (s *Schema) GoLow() *base.Schema {
	return s.low
}

// GoLowUntyped will return the low-level Schema instance that was used to create the high-level one, with no type
func (s *Schema) GoLowUntyped() any {
	return s.low
}

// Render will return a YAML representation of the Schema object as a byte slice.
func (s *Schema) Render() ([]byte, error) {
	return yaml.Marshal(s)
}

// RenderInlineWithContext will return a YAML representation of the Schema object as a byte slice
// using the provided InlineRenderContext for cycle detection.
// Use this when multiple goroutines may render the same schemas concurrently.
// The ctx parameter should be *InlineRenderContext but is typed as any to avoid import cycles.
func (s *Schema) RenderInlineWithContext(ctx any) ([]byte, error) {
	d, err := s.MarshalYAMLInlineWithContext(ctx)
	if err != nil {
		return nil, err
	}
	return yaml.Marshal(d)
}

// RenderInline will return a YAML representation of the Schema object as a byte slice.
// All the $ref values will be inlined, as in resolved in place.
// This method creates a fresh InlineRenderContext internally.
//
// Make sure you don't have any circular references!
func (s *Schema) RenderInline() ([]byte, error) {
	ctx := NewInlineRenderContext()
	return s.RenderInlineWithContext(ctx)
}

// MarshalYAML will create a ready to render YAML representation of the Schema object.
func (s *Schema) MarshalYAML() (interface{}, error) {
	nb := high.NewNodeBuilder(s, s.low)

	// determine index version
	idx := s.GoLow().Index
	if idx != nil {
		if idx.GetConfig().SpecInfo != nil {
			nb.Version = idx.GetConfig().SpecInfo.VersionNumeric
		}
	}
	return nb.Render(), nil
}

// MarshalJSON will create a ready to render JSON representation of the Schema object.
func (s *Schema) MarshalJSON() ([]byte, error) {
	nb := high.NewNodeBuilder(s, s.low)

	// determine index version
	idx := s.GoLow().Index
	if idx != nil {
		if idx.GetConfig().SpecInfo != nil {
			nb.Version = idx.GetConfig().SpecInfo.VersionNumeric
		}
	}
	// render node
	node := nb.Render()
	var renderedJSON map[string]interface{}

	// marshal into struct
	_ = node.Decode(&renderedJSON)

	// return JSON bytes
	return json.Marshal(renderedJSON)
}

// MarshalYAMLInlineWithContext will render out the Schema pointer as YAML using the provided
// InlineRenderContext for cycle detection. All refs will be inlined fully.
// Use this when multiple goroutines may render the same schemas concurrently.
// The ctx parameter should be *InlineRenderContext but is typed as any to satisfy the
// high.RenderableInlineWithContext interface without import cycles.
func (s *Schema) MarshalYAMLInlineWithContext(ctx any) (interface{}, error) {
	// ensure we have a valid render context; create default bundle mode context if nil.
	// this ensures backward compatibility where nil context = bundle mode behavior.
	renderCtx, ok := ctx.(*InlineRenderContext)
	if !ok || renderCtx == nil {
		renderCtx = NewInlineRenderContext()
		ctx = renderCtx
	}

	// determine if we should preserve discriminator refs based on rendering mode.
	// in validation mode, we need to fully inline all refs for the JSON schema compiler.
	// in bundle mode (default), we preserve discriminator refs for mapping compatibility.
	if s.Discriminator != nil && renderCtx.Mode != RenderingModeValidation {
		// mark oneOf/anyOf refs as preserved in the context (not on the SchemaProxy).
		// this avoids mutating shared state and prevents race conditions.
		for _, sp := range s.OneOf {
			if sp != nil && sp.IsReference() {
				renderCtx.MarkRefAsPreserved(sp.GetReference())
			}
		}
		for _, sp := range s.AnyOf {
			if sp != nil && sp.IsReference() {
				renderCtx.MarkRefAsPreserved(sp.GetReference())
			}
		}
	}

	nb := high.NewNodeBuilder(s, s.low)
	nb.Resolve = true
	nb.RenderContext = ctx
	// determine index version
	idx := s.GoLow().Index
	if idx != nil {
		if idx.GetConfig().SpecInfo != nil {
			nb.Version = idx.GetConfig().SpecInfo.VersionNumeric
		}
	}
	return nb.Render(), errors.Join(nb.Errors...)
}

// MarshalYAMLInline will render out the Schema pointer as YAML, and all refs will be inlined fully.
// This method creates a fresh InlineRenderContext internally.
func (s *Schema) MarshalYAMLInline() (interface{}, error) {
	ctx := NewInlineRenderContext()
	return s.MarshalYAMLInlineWithContext(ctx)
}

// MarshalJSONInline will render out the Schema pointer as JSON, and all refs will be inlined fully
func (s *Schema) MarshalJSONInline() ([]byte, error) {
	nb := high.NewNodeBuilder(s, s.low)
	nb.Resolve = true
	// determine index version
	idx := s.GoLow().Index
	if idx != nil {
		if idx.GetConfig().SpecInfo != nil {
			nb.Version = idx.GetConfig().SpecInfo.VersionNumeric
		}
	}
	// render node
	node := nb.Render()
	var renderedJSON map[string]interface{}

	// marshal into struct
	_ = node.Decode(&renderedJSON)

	// return JSON bytes
	return json.Marshal(renderedJSON)
}
