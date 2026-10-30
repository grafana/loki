// Copyright 2023-2025 Princess Beef Heavy Industries, LLC / Dave Shanley
// https://pb33f.io

package base

import (
	"slices"

	"github.com/pb33f/go-yaml"
	"github.com/pb33f/libopenapi/datamodel/low"
	"github.com/pb33f/libopenapi/index"
	"github.com/pb33f/libopenapi/utils"
)

// CheckSchemaProxyForCircularRefs checks if the provided SchemaProxy has any circular references, extracted from
// The rolodex attached to the index.
func CheckSchemaProxyForCircularRefs(s *SchemaProxy) bool {
	if s.GetIndex() == nil || s.GetIndex().GetRolodex() == nil {
		return false // no index or rolodex, so no circular references
	}
	if !s.IsReference() {
		return false // an inline schema is not a reference, so it cannot be a point on a reference loop.
	}
	rolo := s.GetIndex().GetRolodex()
	allCircs := rolo.GetRootIndex().GetCircularReferences()
	safeCircularRefs := rolo.GetSafeCircularReferences()
	ignoredCircularRefs := rolo.GetIgnoredCircularReferences()
	// these getters hand back the index and rolodex's own slices, which carry spare capacity from
	// the appends the resolver builds them with. clone before extending, or this writes into memory
	// shared with every other reader.
	combinedCircularRefs := slices.Clone(safeCircularRefs)
	combinedCircularRefs = append(combinedCircularRefs, ignoredCircularRefs...)
	combinedCircularRefs = append(combinedCircularRefs, allCircs...)
	var target *yaml.Node
	resolved := false
	for _, ref := range combinedCircularRefs {
		// hash the root node of the schema reference
		if ref.LoopPoint.FullDefinition == s.GetReference() || ref.LoopPoint.Definition == s.GetReference() {
			return true
		}
		// check the loop itself: a journey starts with the path that leads into the loop, and the schemas on
		// that path are not circular. a loop point that only appears at the end (the resolver's depth limit)
		// leaves no way to tell the two apart, so the whole journey counts.
		loop := ref.Journey
		if start := slices.IndexFunc(loop, func(ji *index.Reference) bool {
			return ji.FullDefinition == ref.LoopPoint.FullDefinition
		}); start >= 0 && start < len(loop)-1 {
			loop = loop[start:]
		}
		for _, ji := range loop {
			if ji.FullDefinition == s.GetReference() {
				return true
			}
			// journey entries hold absolute definitions, which a relative reference such as 'b.yaml#/B' never
			// matches as written, so compare the node the reference resolves to.
			if ji.Node == nil {
				continue
			}
			if !resolved {
				target, resolved = schemaProxyTarget(s), true
			}
			if target != nil && utils.NodeAlias(ji.Node) == target {
				return true
			}
		}
	}
	return false
}

// schemaProxyTarget returns the node the proxy's reference resolves to, or nil if it cannot be found.
func schemaProxyTarget(s *SchemaProxy) *yaml.Node {
	node := s.GetValueNode()
	if isRef, _, _ := utils.IsNodeRefValue(node); !isRef {
		return utils.NodeAlias(node) // child schema proxies are built from the node their reference already resolved to.
	}
	node, _, _, _ = low.LocateRefNodeWithContext(s.GetContext(), node, s.GetIndex())
	return node
}
