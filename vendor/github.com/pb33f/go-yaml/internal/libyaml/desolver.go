// Copyright 2025 The go-yaml Project Contributors
// SPDX-License-Identifier: Apache-2.0

// Desolver stage: Removes inferable tags from YAML nodes.
// This is the inverse of the Resolver - it works out which tags of a tagged
// node tree can be inferred during parsing, producing cleaner YAML output
// without unnecessary type annotations.
//
// Unlike the Resolver, the Desolver never modifies nodes: the tree being
// dumped may be owned by the caller (a *Node passed to Dump or Marshal).
// The Serializer asks it for the tag and style of each node instead.

package libyaml

// Desolver handles tag removal for YAML nodes during serialization.
// It removes tags that would be automatically resolved to the same type
// during parsing, making the output cleaner and more readable.
type Desolver struct {
	opts *Options
}

// NewDesolver creates a new Desolver with the given options.
func NewDesolver(opts *Options) *Desolver {
	return &Desolver{opts: opts}
}

// Desolve returns the tag and style that n should be serialized with, once
// tags that can be inferred are removed.
// This is the inverse of Resolver - it takes a node of a fully-tagged node
// tree (from Representer) and removes unnecessary tags to produce clean
// output.
//
// For scalar nodes: if the value would resolve to the same tag when parsed,
// the tag is removed. For strings that would resolve differently, the tag is
// removed and quoting style is set to preserve the string type.
//
// For collection nodes (maps/sequences): default tags (!!map, !!seq) are
// removed since they're implied by the structure.
//
// Desolve does not modify n, which may be part of a tree owned by the caller.
func (d *Desolver) Desolve(n *Node) (tag string, style Style) {
	switch n.Kind {
	case ScalarNode:
		return d.desolveScalar(n)
	case DocumentNode, SequenceNode, MappingNode:
		return d.desolveCollection(n), n.Style
	case AliasNode:
		// Alias nodes don't have tags to remove
	}
	return n.Tag, n.Style
}

// desolveScalar returns the tag and style of a scalar node, removing the tag
// when it can be inferred.
func (d *Desolver) desolveScalar(n *Node) (tag string, style Style) {
	tag, style = n.Tag, n.Style

	// If explicitly tagged by user (TaggedStyle), keep it
	if style&TaggedStyle != 0 {
		return tag, style
	}

	// Empty tag means it's already untagged - nothing to do
	if tag == "" {
		return tag, style
	}

	stag := shortTag(tag)

	// Check if this is a standard scalar tag that we can potentially remove
	isStandardTag := false
	switch stag {
	case nullTag, boolTag, strTag, intTag, floatTag, timestampTag:
		isStandardTag = true
	case binaryTag:
		// Binary scalars are not implicitly resolvable - never remove.
		return tag, style
	case mergeTag:
		// Elide the implicit !!merge tag when the value is the canonical
		// merge key marker. The TaggedStyle early-return above already
		// preserves !!merge when it was explicit in the source.
		if n.Value == "<<" {
			tag = ""
		}
		return tag, style
	default:
		// Custom tag - preserve it
		return tag, style
	}

	// Only process standard tags from here
	if !isStandardTag {
		return tag, style
	}

	// What tag would this value resolve to?
	rtag, _ := resolve("", n.Value)

	// If resolved tag matches current tag, we can elide the tag
	if rtag == stag {
		// Tag can be inferred - remove it
		tag = ""
	} else if stag == strTag {
		// This is a string type, but would resolve to something else.
		// Remove the tag and force quoting to preserve string type.
		tag = ""
		// If not already quoted, set quote style based on content
		if style&(SingleQuotedStyle|DoubleQuotedStyle|LiteralStyle|FoldedStyle) == 0 {
			// Determine quote style based on options or default to single quotes
			if d.opts != nil {
				// Convert ScalarStyle to Style
				switch d.opts.QuotePreference.ScalarStyle() {
				case DOUBLE_QUOTED_SCALAR_STYLE:
					style |= DoubleQuotedStyle
				default:
					style |= SingleQuotedStyle
				}
			} else {
				style |= SingleQuotedStyle
			}
		}
	} else if stag == floatTag || stag == intTag {
		// For numeric type mismatches (like float64(1) → "1" with !!float tag):
		// Elide the tag and let YAML resolve naturally.
		// Without the tag, "1" resolves as !!int, which may change the type,
		// but that's acceptable for cleaner output (and matches old behavior).
		tag = ""
	}
	// For other standard tags with mismatches, keep the tag to preserve type
	return tag, style
}

// desolveCollection returns the tag of a collection node, removing default
// tags.
func (d *Desolver) desolveCollection(n *Node) string {
	// If explicitly tagged by user, keep it
	if n.Style&TaggedStyle != 0 {
		return n.Tag
	}

	stag := shortTag(n.Tag)
	switch n.Kind {
	case MappingNode:
		// !!map is the default for mappings - remove it
		if stag == mapTag {
			return ""
		}
	case SequenceNode:
		// !!seq is the default for sequences - remove it
		if stag == seqTag {
			return ""
		}
	case DocumentNode:
		// Documents don't have tags in YAML output
		return ""
	}
	// For other tags, keep them - they're explicit type information
	return n.Tag
}
