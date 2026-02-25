// Copyright 2022-2004 Princess B33f Heavy Industries / Dave Shanley / Quobix
// SPDX-License-Identifier: MIT

// Package low contains a set of low-level models that represent OpenAPI 2 and 3 documents.
// These low-level models (plumbing) are used to create high-level models, and used when deep knowledge
// about the original data, positions, comments and the original node structures.
//
// Low-level models are not designed to be easily navigated, every single property is either a NodeReference
// an KeyReference or a ValueReference. These references hold the raw value and key or value nodes that contain
// the original yaml.Node trees that make up the object.
//
// Navigating maps that use a KeyReference as a key is tricky, because there is no easy way to provide a lookup.
// Convenience methods for lookup up properties in a low-level model have therefore been provided.
package low

import "go.yaml.in/yaml/v4"

// HasRootNode is an interface that is used to extract the root yaml.Node from a low-level model. The root node is
// the top-level node that represents the entire object as represented in the original source file.
type HasRootNode interface {
	GetRootNode() *yaml.Node
}
