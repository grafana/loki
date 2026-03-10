package json

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
)

// YAMLNodeToJSON converts yaml/json stored in a yaml.Node to json ordered matching the original yaml/json
func YAMLNodeToJSON(node *yaml.Node, indentation string) ([]byte, error) {
	v, err := handleYAMLNode(node)
	if err != nil {
		return nil, err
	}

	return json.MarshalIndent(v, "", indentation)
}

func handleYAMLNode(node *yaml.Node) (any, error) {
	switch node.Kind {
	case yaml.DocumentNode:
		return handleYAMLNode(node.Content[0])
	case yaml.SequenceNode:
		return handleSequenceNode(node)
	case yaml.MappingNode:
		return handleMappingNode(node)
	case yaml.ScalarNode:
		return handleScalarNode(node)
	case yaml.AliasNode:
		return handleYAMLNode(node.Alias)
	default:
		return nil, fmt.Errorf("unknown node kind: %v", node.Kind)
	}
}

func handleMappingNode(node *yaml.Node) (any, error) {
	v := orderedmap.New[string, any]()
	for i, n := range node.Content {
		if i%2 == 0 {
			continue
		}
		keyNode := node.Content[i-1]
		kv, err := handleYAMLNode(keyNode)
		if err != nil {
			return nil, err
		}

		if reflect.TypeOf(kv).Kind() != reflect.String {
			keyData, err := json.Marshal(kv)
			if err != nil {
				return nil, err // unreachable code in test case, but kept for safety
			}
			kv = string(keyData)
		}

		vv, err := handleYAMLNode(n)
		if err != nil {
			return nil, err
		}

		v.Set(fmt.Sprintf("%v", kv), vv)
	}

	return v, nil
}

func handleSequenceNode(node *yaml.Node) (any, error) {
	var s []yaml.Node

	if err := node.Decode(&s); err != nil {
		return nil, err // unreachable code in test case, but kept for safety
	}

	v := make([]any, len(s))
	for i, n := range s {
		vv, err := handleYAMLNode(&n)
		if err != nil {
			return nil, err
		}

		v[i] = vv
	}

	return v, nil
}

func handleScalarNode(node *yaml.Node) (any, error) {
	var v any

	if err := node.Decode(&v); err != nil {
		return nil, err
	}

	return v, nil
}
