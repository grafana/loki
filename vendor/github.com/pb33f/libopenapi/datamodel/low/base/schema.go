package base

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/pb33f/libopenapi/datamodel/low"
	"github.com/pb33f/libopenapi/index"
	"github.com/pb33f/libopenapi/orderedmap"
	"github.com/pb33f/libopenapi/utils"
	"go.yaml.in/yaml/v4"
)

// SchemaDynamicValue is used to hold multiple possible types for a schema property. There are two values, a left
// value (A) and a right value (B). The A and B values represent different types that a property can have,
// not necessarily different OpenAPI versions.
//
// For example:
//   - additionalProperties: A = *SchemaProxy (when it's a schema), B = bool (when it's a boolean)
//   - items: A = *SchemaProxy (when it's a schema), B = bool (when it's a boolean in 3.1)
//   - type: A = string (single type), B = []ValueReference[string] (multiple types in 3.1)
//   - exclusiveMinimum: A = bool (in 3.0), B = float64 (in 3.1)
//
// The N value indicates which value is set (0 = A, 1 = B), preventing the need to check both values.
type SchemaDynamicValue[A any, B any] struct {
	N int // 0 == A, 1 == B
	A A
	B B
}

// IsA will return true if the 'A' or left value is set.
func (s *SchemaDynamicValue[A, B]) IsA() bool {
	return s.N == 0
}

// IsB will return true if the 'B' or right value is set.
func (s *SchemaDynamicValue[A, B]) IsB() bool {
	return s.N == 1
}

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
	// Reference to the '$schema' dialect setting (3.1 only)
	SchemaTypeRef low.NodeReference[string]

	// In versions 2 and 3.0, this ExclusiveMaximum can only be a boolean.
	ExclusiveMaximum low.NodeReference[*SchemaDynamicValue[bool, float64]]

	// In versions 2 and 3.0, this ExclusiveMinimum can only be a boolean.
	ExclusiveMinimum low.NodeReference[*SchemaDynamicValue[bool, float64]]

	// In versions 2 and 3.0, this Type is a single value, so array will only ever have one value
	// in version 3.1, Type can be multiple values
	Type low.NodeReference[SchemaDynamicValue[string, []low.ValueReference[string]]]

	// Schemas are resolved on demand using a SchemaProxy
	AllOf low.NodeReference[[]low.ValueReference[*SchemaProxy]]

	// Polymorphic Schemas are only available in version 3+
	OneOf         low.NodeReference[[]low.ValueReference[*SchemaProxy]]
	AnyOf         low.NodeReference[[]low.ValueReference[*SchemaProxy]]
	Discriminator low.NodeReference[*Discriminator]

	// in 3.1 examples can be an array (which is recommended)
	Examples low.NodeReference[[]low.ValueReference[*yaml.Node]]
	// in 3.1 PrefixItems provides tuple validation using prefixItems.
	PrefixItems low.NodeReference[[]low.ValueReference[*SchemaProxy]]
	// in 3.1 Contains is used by arrays and points to a Schema.
	Contains    low.NodeReference[*SchemaProxy]
	MinContains low.NodeReference[int64]
	MaxContains low.NodeReference[int64]

	// items can be a schema in 2.0, 3.0 and 3.1 or a bool in 3.1
	Items low.NodeReference[*SchemaDynamicValue[*SchemaProxy, bool]]

	// 3.1 only
	If                low.NodeReference[*SchemaProxy]
	Else              low.NodeReference[*SchemaProxy]
	Then              low.NodeReference[*SchemaProxy]
	DependentSchemas  low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[*SchemaProxy]]]
	DependentRequired low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[[]string]]]

	PatternProperties     low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[*SchemaProxy]]]
	PropertyNames         low.NodeReference[*SchemaProxy]
	UnevaluatedItems      low.NodeReference[*SchemaProxy]
	UnevaluatedProperties low.NodeReference[*SchemaDynamicValue[*SchemaProxy, bool]]
	Id                    low.NodeReference[string] // JSON Schema 2020-12 $id - schema resource identifier
	Anchor                low.NodeReference[string]
	DynamicAnchor         low.NodeReference[string]
	DynamicRef            low.NodeReference[string]

	// Compatible with all versions
	Title                low.NodeReference[string]
	MultipleOf           low.NodeReference[float64]
	Maximum              low.NodeReference[float64]
	Minimum              low.NodeReference[float64]
	MaxLength            low.NodeReference[int64]
	MinLength            low.NodeReference[int64]
	Pattern              low.NodeReference[string]
	Format               low.NodeReference[string]
	MaxItems             low.NodeReference[int64]
	MinItems             low.NodeReference[int64]
	UniqueItems          low.NodeReference[bool]
	MaxProperties        low.NodeReference[int64]
	MinProperties        low.NodeReference[int64]
	Required             low.NodeReference[[]low.ValueReference[string]]
	Enum                 low.NodeReference[[]low.ValueReference[*yaml.Node]]
	Not                  low.NodeReference[*SchemaProxy]
	Properties           low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[*SchemaProxy]]]
	AdditionalProperties low.NodeReference[*SchemaDynamicValue[*SchemaProxy, bool]]
	Description          low.NodeReference[string]
	ContentEncoding      low.NodeReference[string]
	ContentMediaType     low.NodeReference[string]
	ContentSchema        low.NodeReference[*SchemaProxy]                                                        // JSON Schema 2020-12 contentSchema
	Comment              low.NodeReference[string]                                                              // JSON Schema 2020-12 $comment
	Vocabulary           low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[bool]]] // JSON Schema 2020-12 $vocabulary
	Default              low.NodeReference[*yaml.Node]
	Const                low.NodeReference[*yaml.Node]
	Nullable             low.NodeReference[bool]
	ReadOnly             low.NodeReference[bool]
	WriteOnly            low.NodeReference[bool]
	XML                  low.NodeReference[*XML]
	ExternalDocs         low.NodeReference[*ExternalDoc]
	Example              low.NodeReference[*yaml.Node]
	Deprecated           low.NodeReference[bool]
	Extensions           *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]]

	// Parent Proxy refers back to the low level SchemaProxy that is proxying this schema.
	ParentProxy *SchemaProxy

	// Index is a reference to the SpecIndex that was used to build this schema.
	Index     *index.SpecIndex
	RootNode  *yaml.Node
	index     *index.SpecIndex
	context   context.Context
	nodeStore sync.Map
	reference low.Reference
	*low.Reference
	low.NodeMap
}

// ExtractSchema will return a pointer to a NodeReference that contains a *SchemaProxy if successful. The function
// will specifically look for a key node named 'schema' and extract the value mapped to that key. If the operation
// fails then no NodeReference is returned and an error is returned instead.
func ExtractSchema(ctx context.Context, root *yaml.Node, idx *index.SpecIndex) (*low.NodeReference[*SchemaProxy], error) {
	var schLabel, schNode *yaml.Node
	errStr := "schema build failed: reference '%s' cannot be found at line %d, col %d"

	refLocation := ""
	var refNode *yaml.Node

	foundIndex := idx
	foundCtx := ctx
	if rf, rl, rv := utils.IsNodeRefValue(root); rf {
		// locate reference in index.
		ref, fIdx, err, nCtx := low.LocateRefNodeWithContext(ctx, root, idx)
		if ref != nil {
			schNode = ref
			schLabel = rl
			foundCtx = nCtx
			foundIndex = fIdx
		} else if errors.Is(err, low.ErrExternalRefSkipped) {
			refLocation = rv
			schema := &SchemaProxy{kn: root, vn: root, idx: idx, ctx: ctx}
			_ = schema.Build(ctx, root, root, idx)
			n := &low.NodeReference[*SchemaProxy]{Value: schema, KeyNode: root, ValueNode: root}
			n.SetReference(refLocation, root)
			schema.SetReference(refLocation, root)
			return n, nil
		} else {
			v := root.Content[1].Value
			if root.Content[1].Value == "" {
				v = "[empty]"
			}
			return nil, fmt.Errorf(errStr,
				v, root.Content[1].Line, root.Content[1].Column)
		}
	} else {
		_, schLabel, schNode = utils.FindKeyNodeFull(SchemaLabel, root.Content)
		if schNode != nil {
			h := false
			if h, _, refLocation = utils.IsNodeRefValue(schNode); h {
				ref, fIdx, lerr, nCtx := low.LocateRefNodeWithContext(foundCtx, schNode, foundIndex)
				if ref != nil {
					refNode = schNode
					schNode = ref
					if fIdx != nil {
						foundIndex = fIdx
					}
					foundCtx = nCtx
				} else if errors.Is(lerr, low.ErrExternalRefSkipped) {
					refNode = schNode
				} else {
					v := schNode.Content[1].Value
					if schNode.Content[1].Value == "" {
						v = "[empty]"
					}
					return nil, fmt.Errorf(errStr,
						v, schNode.Content[1].Line, schNode.Content[1].Column)
				}
			}
		}
	}

	if schNode != nil {
		// check if schema has already been built.
		schema := &SchemaProxy{kn: schLabel, vn: schNode, idx: foundIndex, ctx: foundCtx}

		// call Build to ensure transformation happens
		_ = schema.Build(foundCtx, schLabel, schNode, foundIndex)

		schema.SetReference(refLocation, refNode)

		n := &low.NodeReference[*SchemaProxy]{
			Value:     schema,
			KeyNode:   schLabel,
			ValueNode: schema.vn, // use transformed node
		}
		n.SetReference(refLocation, refNode)
		return n, nil
	}
	return nil, nil
}
