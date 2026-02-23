// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package v2

import (
	"context"
	"hash/maphash"

	"github.com/pb33f/libopenapi/datamodel/low"
	"github.com/pb33f/libopenapi/index"
	"github.com/pb33f/libopenapi/orderedmap"
	"github.com/pb33f/libopenapi/utils"
	"go.yaml.in/yaml/v4"
)

// SecurityScheme is a low-level representation of a Swagger / OpenAPI 2 SecurityScheme object.
//
// SecurityScheme allows the definition of a security scheme that can be used by the operations. Supported schemes are
// basic authentication, an API key (either as a header or as a query parameter) and OAuth2's common flows
// (implicit, password, application and access code)
//   - https://swagger.io/specification/v2/#securityDefinitionsObject
type SecurityScheme struct {
	Type             low.NodeReference[string]
	Description      low.NodeReference[string]
	Name             low.NodeReference[string]
	In               low.NodeReference[string]
	Flow             low.NodeReference[string]
	AuthorizationUrl low.NodeReference[string]
	TokenUrl         low.NodeReference[string]
	Scopes           low.NodeReference[*Scopes]
	Extensions       *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]]
}

// GetExtensions returns all SecurityScheme extensions and satisfies the low.HasExtensions interface.
func (ss *SecurityScheme) GetExtensions() *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]] {
	return ss.Extensions
}

// Build will extract extensions and scopes from the node.
func (ss *SecurityScheme) Build(ctx context.Context, _, root *yaml.Node, idx *index.SpecIndex) error {
	root = utils.NodeAlias(root)
	utils.CheckForMergeNodes(root)
	ss.Extensions = low.ExtractExtensions(root)

	scopes, sErr := low.ExtractObject[*Scopes](ctx, ScopesLabel, root, idx)
	if sErr != nil {
		return sErr
	}
	ss.Scopes = scopes
	return nil
}

// Hash will return a consistent Hash of the SecurityScheme object
func (ss *SecurityScheme) Hash() uint64 {
	return low.WithHasher(func(h *maphash.Hash) uint64 {
		if !ss.Type.IsEmpty() {
			h.WriteString(ss.Type.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if !ss.Description.IsEmpty() {
			h.WriteString(ss.Description.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if !ss.Name.IsEmpty() {
			h.WriteString(ss.Name.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if !ss.In.IsEmpty() {
			h.WriteString(ss.In.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if !ss.Flow.IsEmpty() {
			h.WriteString(ss.Flow.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if !ss.AuthorizationUrl.IsEmpty() {
			h.WriteString(ss.AuthorizationUrl.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if !ss.TokenUrl.IsEmpty() {
			h.WriteString(ss.TokenUrl.Value)
			h.WriteByte(low.HASH_PIPE)
		}
		if !ss.Scopes.IsEmpty() {
			h.WriteString(low.GenerateHashString(ss.Scopes.Value))
			h.WriteByte(low.HASH_PIPE)
		}
		for _, ext := range low.HashExtensions(ss.Extensions) {
			h.WriteString(ext)
			h.WriteByte(low.HASH_PIPE)
		}
		return h.Sum64()
	})
}
