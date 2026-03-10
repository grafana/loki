// Copyright 2023 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package base

import (
	"reflect"

	"github.com/pb33f/libopenapi/datamodel/high"
	"go.yaml.in/yaml/v4"
)

// DynamicValue is used to hold multiple possible types for a schema property. There are two values, a left
// value (A) and a right value (B). The A and B values represent different types that a property can have,
// not necessarily different OpenAPI versions.
//
// For example:
//   - additionalProperties: A = SchemaProxy (when it's a schema), B = bool (when it's a boolean)
//   - items: A = SchemaProxy (when it's a schema), B = bool (when it's a boolean in 3.1)
//   - type: A = string (single type), B = []string (multiple types in 3.1)
//   - exclusiveMinimum: A = bool (in 3.0), B = float64 (in 3.1)
//
// The N value indicates which value is set (0 = A, 1 == B), preventing the need to check both values.
type DynamicValue[A any, B any] struct {
	N         int // 0 == A, 1 == B
	A         A
	B         B
	inline    bool
	renderCtx any // Context for inline rendering (typed as any to avoid import cycles)
}

// IsA will return true if the 'A' or left value is set.
func (d *DynamicValue[A, B]) IsA() bool {
	return d.N == 0
}

// IsB will return true if the 'B' or right value is set.
func (d *DynamicValue[A, B]) IsB() bool {
	return d.N == 1
}

func (d *DynamicValue[A, B]) Render() ([]byte, error) {
	d.inline = false
	return yaml.Marshal(d)
}

func (d *DynamicValue[A, B]) RenderInline() ([]byte, error) {
	d.inline = true
	return yaml.Marshal(d)
}

// MarshalYAML will create a ready to render YAML representation of the DynamicValue object.
func (d *DynamicValue[A, B]) MarshalYAML() (interface{}, error) {
	// this is a custom renderer, we can't use the NodeBuilder out of the gate.
	var n yaml.Node
	var err error
	var value any

	if d.IsA() {
		value = d.A
	}
	if d.IsB() {
		value = d.B
	}
	to := reflect.TypeOf(value)
	switch to.Kind() {
	case reflect.Ptr:
		if d.inline {
			// prefer context-aware method when context is available
			if d.renderCtx != nil {
				if r, ok := value.(high.RenderableInlineWithContext); ok {
					return r.MarshalYAMLInlineWithContext(d.renderCtx)
				}
			}
			// fall back to context-less method
			if r, ok := value.(high.RenderableInline); ok {
				return r.MarshalYAMLInline()
			} else {
				_ = n.Encode(value)
			}
		} else {
			if r, ok := value.(high.Renderable); ok {
				return r.MarshalYAML()
			} else {
				_ = n.Encode(value)
			}
		}
	case reflect.Bool:
		_ = n.Encode(value.(bool))
	case reflect.Int:
		_ = n.Encode(value.(int))
	case reflect.String:
		_ = n.Encode(value.(string))
	case reflect.Int64:
		_ = n.Encode(value.(int64))
	case reflect.Float64:
		_ = n.Encode(value.(float64))
	case reflect.Float32:
		_ = n.Encode(value.(float32))
	case reflect.Int32:
		_ = n.Encode(value.(int32))

	}
	return &n, err
}

// MarshalYAMLInline will create a ready to render YAML representation of the DynamicValue object. The
// references will be inlined instead of kept as references.
func (d *DynamicValue[A, B]) MarshalYAMLInline() (interface{}, error) {
	d.inline = true
	d.renderCtx = nil
	return d.MarshalYAML()
}

// MarshalYAMLInlineWithContext will create a ready to render YAML representation of the DynamicValue object.
// The references will be inlined and the provided context will be passed through to nested schemas.
// The ctx parameter should be *InlineRenderContext but is typed as any to avoid import cycles.
func (d *DynamicValue[A, B]) MarshalYAMLInlineWithContext(ctx any) (interface{}, error) {
	d.inline = true
	d.renderCtx = ctx
	return d.MarshalYAML()
}
