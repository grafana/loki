// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package v2

import (
	"context"
	"hash/maphash"
	"sync"

	"github.com/pb33f/libopenapi/datamodel"
	"github.com/pb33f/libopenapi/datamodel/low"
	"github.com/pb33f/libopenapi/datamodel/low/base"
	"github.com/pb33f/libopenapi/index"
	"github.com/pb33f/libopenapi/orderedmap"
	"github.com/pb33f/libopenapi/utils"
	"go.yaml.in/yaml/v4"
)

// ParameterDefinitions is a low-level representation of a Swagger / OpenAPI 2 Parameters Definitions object.
//
// ParameterDefinitions holds parameters to be reused across operations. Parameter definitions can be
// referenced to the ones defined here. It does not define global operation parameters
//   - https://swagger.io/specification/v2/#parametersDefinitionsObject
type ParameterDefinitions struct {
	Definitions *orderedmap.Map[low.KeyReference[string], low.ValueReference[*Parameter]]
}

// ResponsesDefinitions is a low-level representation of a Swagger / OpenAPI 2 Responses Definitions object.
//
// ResponsesDefinitions is an object to hold responses to be reused across operations. Response definitions can be
// referenced to the ones defined here. It does not define global operation responses
//   - https://swagger.io/specification/v2/#responsesDefinitionsObject
type ResponsesDefinitions struct {
	Definitions *orderedmap.Map[low.KeyReference[string], low.ValueReference[*Response]]
}

// SecurityDefinitions is a low-level representation of a Swagger / OpenAPI 2 Security Definitions object.
//
// A declaration of the security schemes available to be used in the specification. This does not enforce the security
// schemes on the operations and only serves to provide the relevant details for each scheme
//   - https://swagger.io/specification/v2/#securityDefinitionsObject
type SecurityDefinitions struct {
	Definitions *orderedmap.Map[low.KeyReference[string], low.ValueReference[*SecurityScheme]]
}

// Definitions is a low-level representation of a Swagger / OpenAPI 2 Definitions object
//
// An object to hold data types that can be consumed and produced by operations. These data types can be primitives,
// arrays or models.
//   - https://swagger.io/specification/v2/#definitionsObject
type Definitions struct {
	Schemas *orderedmap.Map[low.KeyReference[string], low.ValueReference[*base.SchemaProxy]]
}

// FindSchema will attempt to locate a base.SchemaProxy instance using a name.
func (d *Definitions) FindSchema(schema string) *low.ValueReference[*base.SchemaProxy] {
	return low.FindItemInOrderedMap[*base.SchemaProxy](schema, d.Schemas)
}

// FindParameter will attempt to locate a Parameter instance using a name.
func (pd *ParameterDefinitions) FindParameter(parameter string) *low.ValueReference[*Parameter] {
	return low.FindItemInOrderedMap[*Parameter](parameter, pd.Definitions)
}

// FindResponse will attempt to locate a Response instance using a name.
func (r *ResponsesDefinitions) FindResponse(response string) *low.ValueReference[*Response] {
	return low.FindItemInOrderedMap[*Response](response, r.Definitions)
}

// FindSecurityDefinition will attempt to locate a SecurityScheme using a name.
func (s *SecurityDefinitions) FindSecurityDefinition(securityDef string) *low.ValueReference[*SecurityScheme] {
	return low.FindItemInOrderedMap[*SecurityScheme](securityDef, s.Definitions)
}

// Build will extract all definitions into SchemaProxy instances.
func (d *Definitions) Build(ctx context.Context, _, root *yaml.Node, idx *index.SpecIndex) error {
	root = utils.NodeAlias(root)
	utils.CheckForMergeNodes(root)
	type buildInput struct {
		label *yaml.Node
		value *yaml.Node
	}
	results := orderedmap.New[low.KeyReference[string], low.ValueReference[*base.SchemaProxy]]()
	in := make(chan buildInput)
	out := make(chan definitionResult[*base.SchemaProxy])
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2) // input and output goroutines.

	// TranslatePipeline input.
	go func() {
		defer func() {
			close(in)
			wg.Done()
		}()
		var label *yaml.Node
		for i, value := range root.Content {
			if i%2 == 0 {
				label = value
				continue
			}

			select {
			case in <- buildInput{
				label: label,
				value: value,
			}:
			case <-done:
				return
			}
		}
	}()

	// TranslatePipeline output.
	go func() {
		for {
			result, ok := <-out
			if !ok {
				break
			}

			key := low.KeyReference[string]{
				Value:   result.k.Value,
				KeyNode: result.k,
			}
			results.Set(key, result.v)
		}
		close(done)
		wg.Done()
	}()

	translateFunc := func(value buildInput) (definitionResult[*base.SchemaProxy], error) {
		obj, err, _, rv := low.ExtractObjectRaw[*base.SchemaProxy](ctx, value.label, value.value, idx)
		if err != nil {
			return definitionResult[*base.SchemaProxy]{}, err
		}

		v := low.ValueReference[*base.SchemaProxy]{
			Value: obj, ValueNode: value.value,
		}
		v.SetReference(rv, value.value)

		return definitionResult[*base.SchemaProxy]{k: value.label, v: v}, nil
	}

	err := datamodel.TranslatePipeline[buildInput, definitionResult[*base.SchemaProxy]](in, out, translateFunc)
	wg.Wait()
	if err != nil {
		return err
	}

	d.Schemas = results
	return nil
}

// Hash will return a consistent Hash of the Definitions object
func (d *Definitions) Hash() uint64 {
	return low.WithHasher(func(h *maphash.Hash) uint64 {
		for k := range orderedmap.SortAlpha(d.Schemas).KeysFromOldest() {
			h.WriteString(low.GenerateHashString(d.FindSchema(k.Value).Value))
			h.WriteByte(low.HASH_PIPE)
		}
		return h.Sum64()
	})
}

// Build will extract all ParameterDefinitions into Parameter instances.
func (pd *ParameterDefinitions) Build(ctx context.Context, _, root *yaml.Node, idx *index.SpecIndex) error {
	errorChan := make(chan error)
	resultChan := make(chan definitionResult[*Parameter])
	var defLabel *yaml.Node
	totalDefinitions := 0
	buildFunc := func(label *yaml.Node, value *yaml.Node, idx *index.SpecIndex,
		r chan definitionResult[*Parameter], e chan error,
	) {
		obj, err, _, rv := low.ExtractObjectRaw[*Parameter](ctx, label, value, idx)
		if err != nil {
			e <- err
		}

		v := low.ValueReference[*Parameter]{
			Value:     obj,
			ValueNode: value,
		}
		v.SetReference(rv, value)

		r <- definitionResult[*Parameter]{k: label, v: v}
	}
	for i := range root.Content {
		if i%2 == 0 {
			defLabel = root.Content[i]
			continue
		}
		totalDefinitions++
		go buildFunc(defLabel, root.Content[i], idx, resultChan, errorChan)
	}

	completedDefs := 0
	results := orderedmap.New[low.KeyReference[string], low.ValueReference[*Parameter]]()
	for completedDefs < totalDefinitions {
		select {
		case err := <-errorChan:
			return err
		case sch := <-resultChan:
			completedDefs++
			key := low.KeyReference[string]{
				Value:   sch.k.Value,
				KeyNode: sch.k,
			}
			results.Set(key, sch.v)
		}
	}
	pd.Definitions = results
	return nil
}

// re-usable struct for holding results as k/v pairs.
type definitionResult[T any] struct {
	k *yaml.Node
	v low.ValueReference[T]
}

// Build will extract all ResponsesDefinitions into Response instances.
func (r *ResponsesDefinitions) Build(ctx context.Context, _, root *yaml.Node, idx *index.SpecIndex) error {
	errorChan := make(chan error)
	resultChan := make(chan definitionResult[*Response])
	var defLabel *yaml.Node
	totalDefinitions := 0
	buildFunc := func(label *yaml.Node, value *yaml.Node, idx *index.SpecIndex,
		r chan definitionResult[*Response], e chan error,
	) {
		obj, err, _, rv := low.ExtractObjectRaw[*Response](ctx, label, value, idx)
		if err != nil {
			e <- err
		}

		v := low.ValueReference[*Response]{
			Value:     obj,
			ValueNode: value,
		}
		v.SetReference(rv, value)

		r <- definitionResult[*Response]{k: label, v: v}
	}
	for i := range root.Content {
		if i%2 == 0 {
			defLabel = root.Content[i]
			continue
		}
		totalDefinitions++
		go buildFunc(defLabel, root.Content[i], idx, resultChan, errorChan)
	}

	completedDefs := 0
	results := orderedmap.New[low.KeyReference[string], low.ValueReference[*Response]]()
	for completedDefs < totalDefinitions {
		select {
		case err := <-errorChan:
			return err
		case sch := <-resultChan:
			completedDefs++
			key := low.KeyReference[string]{
				Value:   sch.k.Value,
				KeyNode: sch.k,
			}
			results.Set(key, sch.v)
		}
	}
	r.Definitions = results
	return nil
}

// Build will extract all SecurityDefinitions into SecurityScheme instances.
func (s *SecurityDefinitions) Build(ctx context.Context, _, root *yaml.Node, idx *index.SpecIndex) error {
	errorChan := make(chan error)
	resultChan := make(chan definitionResult[*SecurityScheme])
	var defLabel *yaml.Node
	totalDefinitions := 0

	buildFunc := func(label *yaml.Node, value *yaml.Node, idx *index.SpecIndex,
		r chan definitionResult[*SecurityScheme], e chan error,
	) {
		obj, err, _, rv := low.ExtractObjectRaw[*SecurityScheme](ctx, label, value, idx)
		if err != nil {
			e <- err
		}

		v := low.ValueReference[*SecurityScheme]{
			Value: obj, ValueNode: value,
		}
		v.SetReference(rv, value)

		r <- definitionResult[*SecurityScheme]{k: label, v: v}
	}

	for i := range root.Content {
		if i%2 == 0 {
			defLabel = root.Content[i]
			continue
		}
		totalDefinitions++
		go buildFunc(defLabel, root.Content[i], idx, resultChan, errorChan)
	}

	completedDefs := 0
	results := orderedmap.New[low.KeyReference[string], low.ValueReference[*SecurityScheme]]()
	for completedDefs < totalDefinitions {
		select {
		case err := <-errorChan:
			return err
		case sch := <-resultChan:
			completedDefs++
			key := low.KeyReference[string]{
				Value:   sch.k.Value,
				KeyNode: sch.k,
			}
			results.Set(key, sch.v)
		}
	}
	s.Definitions = results
	return nil
}
