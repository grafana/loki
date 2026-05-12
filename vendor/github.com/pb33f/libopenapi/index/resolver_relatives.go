// Copyright 2022-2026 Dave Shanley / Quobix
// SPDX-License-Identifier: MIT

package index

import (
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"strings"

	"github.com/pb33f/libopenapi/utils"
	"go.yaml.in/yaml/v4"
)

func (resolver *Resolver) extractRelatives(
	ref *Reference,
	node, parent *yaml.Node,
	foundRelatives map[string]bool,
	journey []*Reference,
	resolve bool,
	depth int,
	schemaIDBase string,
) []*Reference {
	state := newRelativeWalkState(foundRelatives, journey, resolve, depth, schemaIDBase)
	return resolver.extractRelativesWithState(ref, node, parent, state)
}

func (resolver *Resolver) extractRelativesWithState(
	ref *Reference,
	node, parent *yaml.Node,
	state relativeWalkState,
) []*Reference {
	// Guard against stack overflow from deeply nested or circular specs.
	// Journey tracks the reference chain (100 max); depth tracks recursive calls (500 max).
	if len(state.journey) > 100 {
		return nil
	}
	if state.depth > 500 {
		return resolver.handleRelativeDepthLimit(ref, state)
	}

	state = state.withNodeBase(resolver, node)
	var found []*Reference

	if node != nil && len(node.Content) > 0 {
		skip := false
		for i, n := range node.Content {
			if skip {
				skip = false
				continue
			}

			if utils.IsNodeMap(n) || utils.IsNodeArray(n) {
				found = append(found, resolver.extractNestedRelatives(ref, node, n, state)...)
			}

			if i%2 == 0 && n.Value == "$ref" && len(node.Content) > i%2+1 {
				if relative, handled, skipNext := resolver.extractRelativeReference(ref, node, parent, n, i, state); handled {
					if relative != nil {
						found = append(found, relative)
					}
					skip = skipNext
					continue
				}
			}

			if i%2 == 0 && shouldExtractPolymorphicRelatives(parent, n) {
				found = append(found, resolver.extractPolymorphicRelatives(ref, node, n, state, i)...)
				skip = true
			}
		}
	}

	resolver.relativesSeen += len(found)
	return found
}

func (resolver *Resolver) handleRelativeDepthLimit(ref *Reference, state relativeWalkState) []*Reference {
	def := "unknown"
	if ref != nil {
		def = ref.FullDefinition
	}
	if resolver.specIndex != nil && resolver.specIndex.logger != nil {
		resolver.specIndex.logger.Warn("libopenapi resolver: relative depth exceeded 100 levels, "+
			"check for circular references - resolving may be incomplete",
			"reference", def)
	}

	loop := append(state.journey, ref)
	circRef := &CircularReferenceResult{
		Journey:        loop,
		Start:          ref,
		LoopIndex:      state.depth,
		LoopPoint:      ref,
		IsInfiniteLoop: true,
	}
	if !resolver.circChecked {
		resolver.circularReferences = append(resolver.circularReferences, circRef)
		ref.Circular = true
	}
	return nil
}

func (resolver *Resolver) extractNestedRelatives(
	ref *Reference,
	parent, node *yaml.Node,
	state relativeWalkState,
) []*Reference {
	childState := state.descend()
	foundRef, _, _ := resolver.searchReferenceWithContext(ref, ref)
	if foundRef != nil {
		if foundRef.Circular {
			return nil
		}
		return resolver.extractRelativesWithState(foundRef, node, parent, childState)
	}
	return resolver.extractRelativesWithState(ref, node, parent, childState)
}

func (resolver *Resolver) extractRelativeReference(
	ref *Reference,
	node, parent, keyNode *yaml.Node,
	keyIndex int,
	state relativeWalkState,
) (*Reference, bool, bool) {
	if !utils.IsNodeStringValue(node.Content[keyIndex+1]) || utils.IsNodeArray(node) {
		return nil, false, false
	}

	value := strings.ReplaceAll(node.Content[keyIndex+1].Value, "\\\\", "\\")
	if resolver.specIndex != nil && resolver.specIndex.config != nil &&
		resolver.specIndex.config.SkipExternalRefResolution && utils.IsExternalRef(value) {
		return nil, true, true
	}

	definition, fullDef := resolver.buildRelativeLookupDefinitions(ref, value, state.schemaIDBase)
	searchRef := &Reference{
		Definition:     definition,
		FullDefinition: fullDef,
		RawRef:         value,
		SchemaIdBase:   state.schemaIDBase,
		RemoteLocation: ref.RemoteLocation,
		IsRemote:       true,
		Index:          ref.Index,
	}

	locatedRef, _, _ := resolver.searchReferenceWithContext(ref, searchRef)
	if locatedRef == nil {
		_, path := utils.ConvertComponentIdIntoFriendlyPathSearch(value)
		resolver.resolvingErrors = append(resolver.resolvingErrors, &ResolvingError{
			ErrorRef: fmt.Errorf("cannot resolve reference `%s`, it's missing", value),
			Node:     keyNode,
			Path:     path,
		})
		return nil, true, false
	}

	if state.resolve {
		if ok, _, _ := utils.IsNodeRefValue(ref.Node); ok {
			ref.Node.Content = locatedRef.Node.Content
			ref.Resolved = true
		}
	}

	if ref.ParentNodeSchemaType != "" {
		locatedRef.ParentNodeTypes = append(locatedRef.ParentNodeTypes, ref.ParentNodeSchemaType)
	}
	locatedRef.ParentNodeSchemaType = parentArraySchemaType(parent)
	state.foundRelatives[value] = true
	return locatedRef, true, false
}

func parentArraySchemaType(parent *yaml.Node) string {
	if parent == nil {
		return ""
	}
	_, arrayTypeNode := utils.FindKeyNodeTop("type", parent.Content)
	if arrayTypeNode != nil && arrayTypeNode.Value == "array" {
		return "array"
	}
	return ""
}

func shouldExtractPolymorphicRelatives(parent, keyNode *yaml.Node) bool {
	if keyNode == nil || keyNode.Value == "" || keyNode.Value == "$ref" {
		return false
	}
	if keyNode.Value != "allOf" && keyNode.Value != "oneOf" && keyNode.Value != "anyOf" {
		return false
	}
	return !isInsidePropertiesNode(parent)
}

func isInsidePropertiesNode(parent *yaml.Node) bool {
	if parent == nil {
		return false
	}
	for j := 0; j < len(parent.Content); j += 2 {
		if j < len(parent.Content) && parent.Content[j].Value == "properties" {
			return true
		}
	}
	return false
}

func (resolver *Resolver) buildRelativeLookupDefinitions(ref *Reference, value, currentBase string) (string, string) {
	definition := value
	fullDef := ""
	exp := strings.Split(value, "#/")
	if len(exp) == 2 {
		// Reference contains a fragment (e.g. "other.yaml#/components/schemas/Foo").
		definition = fmt.Sprintf("#/%s", exp[1])
		if exp[0] != "" {
			// Has a file/URL prefix before the fragment.
			if strings.HasPrefix(exp[0], "http") {
				// Absolute HTTP URL — use as-is.
				fullDef = value
			} else if strings.HasPrefix(ref.FullDefinition, "http") {
				// Relative file path, but the parent ref is remote — resolve against the URL.
				httpExp := strings.Split(ref.FullDefinition, "#/")
				u, _ := url.Parse(httpExp[0])
				abs, _ := filepath.Abs(utils.CheckPathOverlap(path.Dir(u.Path), exp[0], string(filepath.Separator)))
				u.Path = utils.ReplaceWindowsDriveWithLinuxPath(abs)
				u.Fragment = ""
				fullDef = fmt.Sprintf("%s#/%s", u.String(), exp[1])
			} else {
				// Relative file path with a local parent — resolve against the parent's directory.
				fileDef := strings.Split(ref.FullDefinition, "#/")
				abs := resolver.resolveLocalRefPath(filepath.Dir(fileDef[0]), exp[0])
				fullDef = fmt.Sprintf("%s#/%s", abs, exp[1])
			}
		} else {
			// Fragment-only ref (e.g. "#/definitions/Bar") — resolve against the parent's base location.
			baseLocation := ref.FullDefinition
			if ref.RemoteLocation != "" {
				baseLocation = ref.RemoteLocation
			}
			if strings.HasPrefix(baseLocation, "http") {
				httpExp := strings.Split(baseLocation, "#/")
				u, _ := url.Parse(httpExp[0])
				fullDef = fmt.Sprintf("%s#/%s", u.String(), exp[1])
			} else {
				fileDef := strings.Split(baseLocation, "#/")
				fullDef = fmt.Sprintf("%s#/%s", fileDef[0], exp[1])
			}
		}
	} else if strings.HasPrefix(value, "http") {
		// No fragment, absolute HTTP URL — use as-is.
		fullDef = value
	} else {
		// No fragment, relative file path — resolve against the parent's base location.
		baseLocation := ref.FullDefinition
		if ref.RemoteLocation != "" {
			baseLocation = ref.RemoteLocation
		}
		fileDef := strings.Split(baseLocation, "#/")
		if strings.HasPrefix(fileDef[0], "http") {
			u, _ := url.Parse(fileDef[0])
			absPath, _ := filepath.Abs(utils.CheckPathOverlap(path.Dir(u.Path), exp[0], string(filepath.Separator)))
			u.Path = utils.ReplaceWindowsDriveWithLinuxPath(absPath)
			fullDef = u.String()
		} else {
			fullDef = resolver.resolveLocalRefPath(filepath.Dir(fileDef[0]), exp[0])
		}
	}

	if currentBase != "" {
		fullDef = resolveRefWithSchemaBase(value, currentBase)
	}
	return definition, fullDef
}
