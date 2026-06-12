// Copyright 2023-2026 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package utils

import "go.yaml.in/yaml/v4"

type simpleJSONPathStepKind uint8

const (
	simpleJSONPathProperty simpleJSONPathStepKind = iota
	simpleJSONPathIndex
)

type simpleJSONPathStep struct {
	kind     simpleJSONPathStepKind
	property string
	index    int
}

func findNodesWithoutDeserializingFastPath(node *yaml.Node, jsonPath string) ([]*yaml.Node, bool) {
	steps, ok := parseSimpleJSONPath(jsonPath)
	if !ok {
		return nil, false
	}

	current := NodeAlias(node)
	if current == nil {
		return nil, true
	}
	if current.Kind == yaml.DocumentNode && len(current.Content) > 0 {
		current = NodeAlias(current.Content[0])
	}

	for _, step := range steps {
		switch step.kind {
		case simpleJSONPathProperty:
			current = navigateJSONPathProperty(current, step.property)
		case simpleJSONPathIndex:
			current = navigateJSONPathIndex(current, step.index)
		}
		if current == nil {
			return nil, true
		}
	}

	return []*yaml.Node{current}, true
}

func parseSimpleJSONPath(path string) ([]simpleJSONPathStep, bool) {
	if path == "" || path[0] != '$' {
		return nil, false
	}

	steps := make([]simpleJSONPathStep, 0, 8)
	for i := 1; i < len(path); {
		switch path[i] {
		case '.':
			i++
			if i >= len(path) {
				return nil, false
			}
			start := i
			for i < len(path) && path[i] != '.' && path[i] != '[' {
				switch path[i] {
				case '*', '?', '(', ')':
					return nil, false
				}
				i++
			}
			if start == i {
				return nil, false
			}
			token := path[start:i]
			if index, ok := parseSmallUint(token); ok {
				steps = append(steps, simpleJSONPathStep{kind: simpleJSONPathIndex, index: index})
			} else {
				steps = append(steps, simpleJSONPathStep{kind: simpleJSONPathProperty, property: token})
			}
		case '[':
			if i+1 >= len(path) {
				return nil, false
			}
			if path[i+1] == '\'' {
				i += 2
				start := i
				for i < len(path) && path[i] != '\'' {
					i++
				}
				if i >= len(path) || i+1 >= len(path) || path[i+1] != ']' {
					return nil, false
				}
				steps = append(steps, simpleJSONPathStep{
					kind:     simpleJSONPathProperty,
					property: path[start:i],
				})
				i += 2
				continue
			}

			i++
			start := i
			for i < len(path) && path[i] != ']' {
				if path[i] < '0' || path[i] > '9' {
					return nil, false
				}
				i++
			}
			if i >= len(path) || start == i {
				return nil, false
			}
			index, _ := parseSmallUint(path[start:i])
			steps = append(steps, simpleJSONPathStep{kind: simpleJSONPathIndex, index: index})
			i++
		default:
			return nil, false
		}
	}

	return steps, true
}

func navigateJSONPathProperty(node *yaml.Node, property string) *yaml.Node {
	current := NodeAlias(node)
	if !IsNodeMap(current) {
		return nil
	}
	for i := 0; i < len(current.Content)-1; i += 2 {
		if current.Content[i].Value == property {
			return NodeAlias(current.Content[i+1])
		}
	}
	return nil
}

func navigateJSONPathIndex(node *yaml.Node, index int) *yaml.Node {
	current := NodeAlias(node)
	if !IsNodeArray(current) || index < 0 || index >= len(current.Content) {
		return nil
	}
	return NodeAlias(current.Content[index])
}
