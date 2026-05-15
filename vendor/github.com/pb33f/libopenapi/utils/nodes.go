// Copyright 2023 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package utils

import (
	"go.yaml.in/yaml/v4"
)

func CreateRefNode(ref string) *yaml.Node {
	m := CreateEmptyMapNode()
	nodes := make([]*yaml.Node, 2)
	nodes[0] = CreateStringNode("$ref")
	nodes[1] = CreateStringNode(ref)
	nodes[1].Style = yaml.SingleQuotedStyle
	m.Content = nodes
	return m
}

func CreateEmptyMapNode() *yaml.Node {
	n := &yaml.Node{
		Kind: yaml.MappingNode,
		Tag:  "!!map",
	}
	return n
}

func CreateYamlNode(a any) *yaml.Node {
	var n yaml.Node
	_ = n.Encode(a)

	return &n
}

func CreateEmptySequenceNode() *yaml.Node {
	n := &yaml.Node{
		Kind: yaml.SequenceNode,
		Tag:  "!!seq",
	}
	return n
}

func CreateStringNode(str string) *yaml.Node {
	n := &yaml.Node{
		Kind:  yaml.ScalarNode,
		Tag:   "!!str",
		Value: str,
	}
	return n
}

func CreateBoolNode(str string) *yaml.Node {
	n := &yaml.Node{
		Kind:  yaml.ScalarNode,
		Value: str,
	}
	return n
}

func CreateIntNode(str string) *yaml.Node {
	n := &yaml.Node{
		Kind:  yaml.ScalarNode,
		Value: str,
	}
	return n
}

func CreateEmptyScalarNode() *yaml.Node {
	n := &yaml.Node{
		Kind:  yaml.ScalarNode,
		Tag:   "!!null",
		Value: "",
	}
	return n
}

func CreateFloatNode(str string) *yaml.Node {
	n := &yaml.Node{
		Kind:  yaml.ScalarNode,
		Value: str,
	}
	return n
}
