// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package base

import (
	"fmt"
	"hash/maphash"

	"go.yaml.in/yaml/v4"

	"github.com/pb33f/libopenapi/datamodel/low"
	"github.com/pb33f/libopenapi/orderedmap"
	"github.com/pb33f/libopenapi/utils"
)

// Discriminator is only used by OpenAPI 3+ documents, it represents a polymorphic discriminator used for schemas
//
// When request bodies or response payloads may be one of a number of different schemas, a discriminator object can be
// used to aid in serialization, deserialization, and validation. The discriminator is a specific object in a schema
// which is used to inform the consumer of the document of an alternative schema based on the value associated with it.
//
// When using the discriminator, inline schemas will not be considered.
//
//	v3 - https://spec.openapis.org/oas/v3.1.0#discriminator-object
type Discriminator struct {
	PropertyName   low.NodeReference[string]
	Mapping        low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[string]]]
	DefaultMapping low.NodeReference[string] // OpenAPI 3.2+ defaultMapping for fallback schema
	KeyNode        *yaml.Node
	RootNode       *yaml.Node
	low.Reference
	low.NodeMap
}

// ValidateDiscriminatorMappingValueNodes checks that discriminator mapping values are scalar strings.
func ValidateDiscriminatorMappingValueNodes(discriminatorNode *yaml.Node) error {
	discriminatorNode = utils.NodeAlias(discriminatorNode)
	if discriminatorNode == nil || discriminatorNode.Kind != yaml.MappingNode {
		return nil
	}
	utils.CheckForMergeNodes(discriminatorNode)

	for i := 0; i < len(discriminatorNode.Content); i += 2 {
		keyNode := utils.NodeAlias(discriminatorNode.Content[i])
		if keyNode == nil {
			continue
		}
		if keyNode.Value != "mapping" {
			continue
		}

		mappingNode := utils.NodeAlias(discriminatorNode.Content[i+1])
		if mappingNode == nil || mappingNode.Kind != yaml.MappingNode {
			return fmt.Errorf("discriminator.mapping must be an object")
		}
		utils.CheckForMergeNodes(mappingNode)

		for j := 0; j < len(mappingNode.Content); j += 2 {
			keyNode := utils.NodeAlias(mappingNode.Content[j])
			if keyNode == nil {
				continue
			}
			mappingName := keyNode.Value
			valueNode := utils.NodeAlias(mappingNode.Content[j+1])
			if valueNode == nil || valueNode.Kind != yaml.ScalarNode || valueNode.Tag != "!!str" {
				return fmt.Errorf("discriminator.mapping.%s must be a string, found %s", mappingName, describeDiscriminatorMappingNode(valueNode))
			}
		}
		return nil
	}

	return nil
}

func describeDiscriminatorMappingNode(node *yaml.Node) string {
	if node == nil {
		return "nil"
	}
	if node.Kind == yaml.ScalarNode {
		return node.Tag
	}

	switch node.Kind {
	case yaml.MappingNode:
		return "object"
	case yaml.SequenceNode:
		return "array"
	case yaml.DocumentNode:
		return "document"
	case yaml.AliasNode:
		return "alias"
	default:
		return fmt.Sprintf("kind %d", node.Kind)
	}
}

// GetRootNode will return the root yaml node of the Discriminator object
func (d *Discriminator) GetRootNode() *yaml.Node {
	return d.RootNode
}

// GetKeyNode will return the key yaml node of the Discriminator object
func (d *Discriminator) GetKeyNode() *yaml.Node {
	return d.KeyNode
}

// FindMappingValue will return a ValueReference containing the string mapping value
func (d *Discriminator) FindMappingValue(key string) *low.ValueReference[string] {
	for k, v := range d.Mapping.Value.FromOldest() {
		if k.Value == key {
			return &v
		}
	}
	return nil
}

// Hash will return a consistent hash of the Discriminator object
func (d *Discriminator) Hash() uint64 {
	return low.WithHasher(func(h *maphash.Hash) uint64 {
		if d.PropertyName.Value != "" {
			h.WriteString(d.PropertyName.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		for v := range orderedmap.SortAlpha(d.Mapping.Value).ValuesFromOldest() {
			h.WriteString(v.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if d.DefaultMapping.Value != "" {
			h.WriteString(d.DefaultMapping.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		return h.Sum64()
	})
}
