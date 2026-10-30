// Copyright 2022-2026 Dave Shanley / Quobix
// SPDX-License-Identifier: MIT

package index

import "github.com/pb33f/go-yaml"

type relativeWalkState struct {
	foundRelatives map[string]bool
	journey        []*Reference
	resolve        bool
	depth          int
	schemaIDBase   string
}

func newRelativeWalkState(
	foundRelatives map[string]bool,
	journey []*Reference,
	resolve bool,
	depth int,
	schemaIDBase string,
) relativeWalkState {
	return relativeWalkState{
		foundRelatives: foundRelatives,
		journey:        journey,
		resolve:        resolve,
		depth:          depth,
		schemaIDBase:   schemaIDBase,
	}
}

func (state relativeWalkState) withNodeBase(resolver *Resolver, node *yaml.Node) relativeWalkState {
	state.schemaIDBase = resolver.resolveSchemaIdBase(state.schemaIDBase, node)
	return state
}

func (state relativeWalkState) descend() relativeWalkState {
	state.depth++
	return state
}
