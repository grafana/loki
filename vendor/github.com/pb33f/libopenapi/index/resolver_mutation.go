// Copyright 2022-2026 Dave Shanley / Quobix
// SPDX-License-Identifier: MIT

package index

import "go.yaml.in/yaml/v4"

func (resolver *Resolver) visitReferenceShortCircuit(ref *Reference, resolve bool) ([]*yaml.Node, bool) {
	if ref == nil {
		return nil, true
	}
	if resolve && ref.Seen {
		if ref.Resolved {
			if ref.Node != nil {
				return ref.Node.Content, true
			}
			return nil, true
		}
	}
	if !resolve && ref.Seen {
		if ref.Node != nil {
			return ref.Node.Content, true
		}
		return nil, true
	}
	return nil, false
}

func (resolver *Resolver) collectReferenceRelatives(
	ref *Reference,
	seen map[string]bool,
	journey []*Reference,
	resolve bool,
) []*Reference {
	base := resolver.resolveSchemaIdBase(ref.SchemaIdBase, ref.Node)
	return resolver.extractRelatives(ref, ref.Node, nil, seen, journey, resolve, 0, base)
}

func (resolver *Resolver) visitReferenceRelatives(
	ref *Reference,
	relatives []*Reference,
	seen map[string]bool,
	journey []*Reference,
	resolve bool,
) {
	for _, relative := range relatives {
		if resolver.handleCircularJourneyRelative(ref, relative, journey) {
			continue
		}
		resolver.resolveRelativeReference(ref, relative, seen, journey, resolve)
	}
}

func (resolver *Resolver) resolveRelativeReference(
	ref, relative *Reference,
	seen map[string]bool,
	journey []*Reference,
	resolve bool,
) {
	original := relative
	foundRef, _, _ := resolver.searchReferenceWithContext(ref, relative)
	if foundRef != nil {
		original = foundRef
	}

	resolved := resolver.VisitReference(original, seen, journey, resolve)
	if resolve && original != nil && !original.Circular {
		ref.Resolved = true
		relative.Resolved = true
		if relative.Node != nil {
			relative.Node.Content = resolved
		}
	}
	relative.Seen = true
	ref.Seen = true
}
