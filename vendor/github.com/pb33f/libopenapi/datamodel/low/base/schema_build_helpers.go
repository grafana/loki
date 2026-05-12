// Copyright 2022-2026 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package base

import (
	"context"
	"errors"
	"fmt"

	"github.com/pb33f/libopenapi/datamodel/low"
	"github.com/pb33f/libopenapi/index"
	"github.com/pb33f/libopenapi/orderedmap"
	"github.com/pb33f/libopenapi/utils"
	"go.yaml.in/yaml/v4"
)

type resolvedSchemaBuildInput struct {
	ctx         context.Context
	idx         *index.SpecIndex
	valueNode   *yaml.Node
	refNode     *yaml.Node
	refLocation string
}

func buildPropertyMap(ctx context.Context, parent *Schema, root *yaml.Node, idx *index.SpecIndex, label string) (*low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[*SchemaProxy]]], error) {
	_, propLabel, propsNode := utils.FindKeyNodeFullTop(label, root.Content)
	if propsNode != nil {
		propertyMap := orderedmap.New[low.KeyReference[string], low.ValueReference[*SchemaProxy]]()
		for i := 0; i < len(propsNode.Content)-1; i += 2 {
			currentProp := propsNode.Content[i]
			prop := propsNode.Content[i+1]
			parent.Nodes.Store(currentProp.Line, currentProp)

			resolved, err := resolveSchemaBuildInput(ctx, prop, idx,
				"schema properties build failed: cannot find reference %s, line %d, col %d")
			if err != nil {
				return nil, err
			}

			propertyMap.Set(low.KeyReference[string]{
				KeyNode: currentProp,
				Value:   currentProp.Value,
			}, buildSchemaProxy(resolved.ctx, resolved.idx, currentProp, resolved.valueNode, resolved.refNode, resolved.refLocation != "", resolved.refLocation))
		}

		return &low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[*SchemaProxy]]]{
			Value:     propertyMap,
			KeyNode:   propLabel,
			ValueNode: propsNode,
		}, nil
	}
	return nil, nil
}

// buildDependentRequiredMap builds an ordered map of string arrays for the dependentRequired property
func buildDependentRequiredMap(root *yaml.Node, label string) (*low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[[]string]]], error) {
	_, propLabel, propsNode := utils.FindKeyNodeFullTop(label, root.Content)
	if propsNode != nil {
		dependentRequiredMap := orderedmap.New[low.KeyReference[string], low.ValueReference[[]string]]()
		for i := 0; i < len(propsNode.Content)-1; i += 2 {
			currentKey := propsNode.Content[i]
			node := propsNode.Content[i+1]

			if !utils.IsNodeArray(node) {
				return nil, fmt.Errorf("dependentRequired value must be an array, found %v at line %d, col %d",
					node.Kind, node.Line, node.Column)
			}

			requiredProps := make([]string, 0, len(node.Content))
			for _, propNode := range node.Content {
				if propNode.Kind != yaml.ScalarNode {
					return nil, fmt.Errorf("dependentRequired array items must be strings, found %v at line %d, col %d",
						propNode.Kind, propNode.Line, propNode.Column)
				}
				requiredProps = append(requiredProps, propNode.Value)
			}

			dependentRequiredMap.Set(low.KeyReference[string]{
				KeyNode: currentKey,
				Value:   currentKey.Value,
			}, low.ValueReference[[]string]{
				Value:     requiredProps,
				ValueNode: node,
			})
		}

		return &low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[[]string]]]{
			Value:     dependentRequiredMap,
			KeyNode:   propLabel,
			ValueNode: propsNode,
		}, nil
	}
	return nil, nil
}

// extract extensions from schema
func (s *Schema) extractExtensions(root *yaml.Node) {
	s.Extensions = low.ExtractExtensions(root)
}

// buildSchemaProxy builds out a SchemaProxy for a single node.
func buildSchemaProxy(ctx context.Context, idx *index.SpecIndex, kn, vn *yaml.Node, rf *yaml.Node, isRef bool, refLocation string) low.ValueReference[*SchemaProxy] {
	sp := new(SchemaProxy)
	if isRef {
		sp.prepareForResolvedBuild(ctx, kn, vn, idx, refLocation, rf)
	} else {
		sp.prepareForResolvedBuild(ctx, kn, vn, idx, "", nil)
	}
	return low.ValueReference[*SchemaProxy]{
		Value:     sp,
		ValueNode: sp.vn,
	}
}

// buildSchema builds out a child schema for parent schema. Expected to be a singular schema object.
func buildSchema(ctx context.Context, labelNode, valueNode *yaml.Node, idx *index.SpecIndex) (low.ValueReference[*SchemaProxy], error) {
	if valueNode == nil {
		return low.ValueReference[*SchemaProxy]{}, nil
	}

	if !utils.IsNodeMap(valueNode) {
		return low.ValueReference[*SchemaProxy]{}, fmt.Errorf("build schema failed: expected a single schema object for '%s', but found an array or scalar at line %d, col %d",
			utils.MakeTagReadable(valueNode), valueNode.Line, valueNode.Column)
	}

	resolved, err := resolveSchemaBuildInput(ctx, valueNode, idx,
		"build schema failed: reference cannot be found: %s, line %d, col %d")
	if err != nil {
		return low.ValueReference[*SchemaProxy]{}, err
	}

	return buildSchemaProxy(resolved.ctx, resolved.idx, labelNode, resolved.valueNode, resolved.refNode, resolved.refLocation != "", resolved.refLocation), nil
}

// buildSchemaList builds out child schemas for a parent schema. Expected to be an array of schema objects.
func buildSchemaList(ctx context.Context, labelNode, valueNode *yaml.Node, idx *index.SpecIndex) ([]low.ValueReference[*SchemaProxy], error) {
	if valueNode == nil {
		return nil, nil
	}

	if !utils.IsNodeArray(valueNode) {
		return nil, fmt.Errorf("build schema failed: expected an array of schemas for '%s', but found an object or scalar at line %d, col %d",
			utils.MakeTagReadable(valueNode), valueNode.Line, valueNode.Column)
	}

	results := make([]low.ValueReference[*SchemaProxy], 0, len(valueNode.Content))

	for _, vn := range valueNode.Content {
		resolved, err := resolveSchemaBuildInput(ctx, vn, idx,
			"build schema failed: reference cannot be found: %s, line %d, col %d")
		if err != nil {
			return nil, err
		}
		r := buildSchemaProxy(resolved.ctx, resolved.idx, resolved.valueNode, resolved.valueNode, resolved.refNode, resolved.refLocation != "", resolved.refLocation)
		results = append(results, r)
	}

	return results, nil
}

func assignBuiltSchema(ctx context.Context, labelNode, valueNode *yaml.Node, idx *index.SpecIndex, dst *low.ValueReference[*SchemaProxy]) error {
	if valueNode == nil {
		return nil
	}

	res, err := buildSchema(ctx, labelNode, valueNode, idx)
	if err != nil {
		return err
	}
	*dst = res
	return nil
}

func assignBuiltSchemaList(ctx context.Context, labelNode, valueNode *yaml.Node, idx *index.SpecIndex, dst *[]low.ValueReference[*SchemaProxy]) error {
	if valueNode == nil {
		return nil
	}

	res, err := buildSchemaList(ctx, labelNode, valueNode, idx)
	if err != nil {
		return err
	}
	*dst = res
	return nil
}

func resolveSchemaBuildInput(ctx context.Context, valueNode *yaml.Node, idx *index.SpecIndex, errFormat string) (resolvedSchemaBuildInput, error) {
	resolved := resolvedSchemaBuildInput{
		ctx:       ctx,
		idx:       idx,
		valueNode: valueNode,
	}

	if valueNode == nil {
		return resolved, nil
	}

	if hasRef, _, refLocation := utils.IsNodeRefValue(valueNode); hasRef {
		ref, foundIdx, err, foundCtx := low.LocateRefNodeWithContext(ctx, valueNode, idx)
		if ref != nil {
			resolved.refNode = valueNode
			resolved.valueNode = ref
			resolved.refLocation = refLocation
			resolved.ctx = foundCtx
			resolved.idx = foundIdx
			return resolved, nil
		}
		if errors.Is(err, low.ErrExternalRefSkipped) {
			resolved.refNode = valueNode
			resolved.refLocation = refLocation
			return resolved, nil
		}
		return resolved, fmt.Errorf(errFormat, valueNode.Content[1].Value, valueNode.Content[1].Line, valueNode.Content[1].Column)
	}

	return resolved, nil
}
