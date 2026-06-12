// Copyright 2022-2026 Dave Shanley / Quobix
// SPDX-License-Identifier: MIT

package index

import (
	"net/url"
	"path"
	"path/filepath"
	"strings"

	"github.com/pb33f/libopenapi/utils"
	"go.yaml.in/yaml/v4"
)

func (resolver *Resolver) buildDefPath(ref *Reference, l string) string {
	def := ""
	exp := strings.Split(l, "#/")
	if len(exp) == 2 {
		// Reference contains a fragment (e.g. "file.yaml#/components/schemas/Foo" or "#/definitions/Bar").
		if exp[0] != "" {
			// Has a file/URL portion before the fragment.
			if !strings.HasPrefix(exp[0], "http") {
				if !filepath.IsAbs(exp[0]) {
					// Relative file path — resolve against the parent ref's location.
					if strings.HasPrefix(ref.FullDefinition, "http") {
						// Parent is a remote URL: resolve relative path against the URL's directory.
						u, _ := url.Parse(ref.FullDefinition)
						p, _ := filepath.Abs(utils.CheckPathOverlap(path.Dir(u.Path), exp[0], string(filepath.Separator)))
						u.Path = utils.ReplaceWindowsDriveWithLinuxPath(p)
						def = l
						if len(exp[1]) > 0 {
							def = u.String() + "#/" + exp[1]
						}
					} else {
						// Parent is a local file path: resolve relative to the parent's directory.
						z := strings.Split(ref.FullDefinition, "#/")
						if len(z) == 2 {
							if len(z[0]) > 0 {
								abs := resolver.resolveLocalRefPath(filepath.Dir(z[0]), exp[0])
								def = abs + "#/" + exp[1]
							} else {
								abs, _ := filepath.Abs(exp[0])
								def = abs + "#/" + exp[1]
							}
						} else {
							abs := resolver.resolveLocalRefPath(filepath.Dir(ref.FullDefinition), exp[0])
							def = abs + "#/" + exp[1]
						}
					}
				}
			} else if len(exp[1]) > 0 {
				// Absolute HTTP URL with a fragment — use as-is.
				def = l
			} else {
				// HTTP URL with no fragment content — use just the URL part.
				def = exp[0]
			}
		} else if strings.HasPrefix(ref.FullDefinition, "http") {
			// Fragment-only ref (e.g. "#/components/schemas/Foo") with a remote parent.
			u, _ := url.Parse(ref.FullDefinition)
			u.Fragment = ""
			def = u.String() + "#/" + exp[1]
		} else if strings.HasPrefix(ref.FullDefinition, "#/") {
			// Fragment-only ref with a fragment-only parent — keep as local fragment.
			def = "#/" + exp[1]
		} else {
			// Fragment-only ref with a local file parent — prepend the file portion.
			fdexp := strings.Split(ref.FullDefinition, "#/")
			def = fdexp[0] + "#/" + exp[1]
		}
	} else if strings.HasPrefix(l, "http") {
		// No fragment, absolute HTTP URL — use as-is.
		def = l
	} else if strings.HasPrefix(ref.FullDefinition, "http") {
		// No fragment, relative path with a remote parent — resolve against the URL.
		u, _ := url.Parse(ref.FullDefinition)
		abs, _ := filepath.Abs(utils.CheckPathOverlap(path.Dir(u.Path), l, string(filepath.Separator)))
		u.Path = utils.ReplaceWindowsDriveWithLinuxPath(abs)
		u.Fragment = ""
		def = u.String()
	} else {
		// No fragment, local relative path — resolve against the parent's directory.
		lookupRef := strings.Split(ref.FullDefinition, "#/")
		def = resolver.resolveLocalRefPath(filepath.Dir(lookupRef[0]), l)
	}

	return def
}

func (resolver *Resolver) resolveLocalRefPath(base, ref string) string {
	if resolver != nil && resolver.specIndex != nil {
		return resolver.specIndex.ResolveRelativeFilePath(base, ref)
	}
	abs, _ := filepath.Abs(utils.CheckPathOverlap(base, ref, string(filepath.Separator)))
	return abs
}

func (resolver *Resolver) buildDefPathWithSchemaBase(ref *Reference, l string, schemaIDBase string) string {
	if schemaIDBase != "" {
		normalized := resolveRefWithSchemaBase(l, schemaIDBase)
		if normalized != l {
			return normalized
		}
	}
	return resolver.buildDefPath(ref, l)
}

func (resolver *Resolver) resolveSchemaIdBase(parentBase string, node *yaml.Node) string {
	if node == nil {
		return parentBase
	}
	idValue := FindSchemaIdInNode(node)
	if idValue == "" {
		return parentBase
	}
	base := parentBase
	if base == "" && resolver.specIndex != nil {
		base = resolver.specIndex.specAbsolutePath
	}
	resolved, err := ResolveSchemaId(idValue, base)
	if err != nil || resolved == "" {
		return idValue
	}
	return resolved
}

// ResolvePendingNodes applies deferred node content replacements that were collected during resolution.
func (resolver *Resolver) ResolvePendingNodes() {
	for _, r := range resolver.specIndex.pendingResolve {
		r.ref.Node.Content = r.nodes
	}
}
