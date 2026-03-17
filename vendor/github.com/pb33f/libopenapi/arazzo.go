// Copyright 2022-2026 Princess Beef Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package libopenapi

import (
	gocontext "context"
	"fmt"

	high "github.com/pb33f/libopenapi/datamodel/high/arazzo"
	"github.com/pb33f/libopenapi/datamodel/low"
	lowArazzo "github.com/pb33f/libopenapi/datamodel/low/arazzo"
	"go.yaml.in/yaml/v4"
)

// NewArazzoDocument parses raw bytes into a high-level Arazzo document.
func NewArazzoDocument(arazzoBytes []byte) (*high.Arazzo, error) {
	var rootNode yaml.Node
	if err := yaml.Unmarshal(arazzoBytes, &rootNode); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	if rootNode.Kind != yaml.DocumentNode || len(rootNode.Content) == 0 {
		return nil, fmt.Errorf("invalid YAML document structure")
	}

	mappingNode := rootNode.Content[0]
	if mappingNode.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("expected YAML mapping, got %v", mappingNode.Kind)
	}

	// Build the low-level model
	lowDoc := &lowArazzo.Arazzo{}
	if err := low.BuildModel(mappingNode, lowDoc); err != nil {
		return nil, fmt.Errorf("failed to build low-level model: %w", err)
	}

	ctx := gocontext.Background()
	if err := lowDoc.Build(ctx, nil, mappingNode, nil); err != nil {
		return nil, fmt.Errorf("failed to build arazzo document: %w", err)
	}

	// Build the high-level model
	highDoc := high.NewArazzo(lowDoc)
	return highDoc, nil
}
