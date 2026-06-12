// Copyright 2022-2026 Dave Shanley / Quobix
// SPDX-License-Identifier: MIT

package index

import (
	"sort"

	"go.yaml.in/yaml/v4"
)

// SetCircularReferences sets the circular reference results for this index.
func (index *SpecIndex) SetCircularReferences(refs []*CircularReferenceResult) {
	index.circularReferences = refs
}

// GetCircularReferences returns all circular references found during resolution.
func (index *SpecIndex) GetCircularReferences() []*CircularReferenceResult {
	return index.circularReferences
}

// GetTagCircularReferences returns circular references found in tag parent hierarchies.
func (index *SpecIndex) GetTagCircularReferences() []*CircularReferenceResult {
	return index.tagCircularReferences
}

// SetIgnoredPolymorphicCircularReferences sets circular references that were ignored because they
// involve polymorphic keywords (allOf, oneOf, anyOf).
func (index *SpecIndex) SetIgnoredPolymorphicCircularReferences(refs []*CircularReferenceResult) {
	index.polyCircularReferences = refs
}

// SetIgnoredArrayCircularReferences sets circular references that were ignored because they
// involve array items.
func (index *SpecIndex) SetIgnoredArrayCircularReferences(refs []*CircularReferenceResult) {
	index.arrayCircularReferences = refs
}

// GetIgnoredPolymorphicCircularReferences returns circular references that were ignored because
// they involve polymorphic keywords.
func (index *SpecIndex) GetIgnoredPolymorphicCircularReferences() []*CircularReferenceResult {
	return index.polyCircularReferences
}

// GetIgnoredArrayCircularReferences returns circular references that were ignored because they
// involve array items.
func (index *SpecIndex) GetIgnoredArrayCircularReferences() []*CircularReferenceResult {
	return index.arrayCircularReferences
}

// GetPathsNode returns the raw YAML node for the top-level "paths" object.
func (index *SpecIndex) GetPathsNode() *yaml.Node {
	return index.pathsNode
}

// GetDiscoveredReferences returns all deduplicated references found during extraction,
// keyed by their full definition path.
func (index *SpecIndex) GetDiscoveredReferences() map[string]*Reference {
	return index.allRefs
}

// GetPolyReferences returns all polymorphic references (allOf, oneOf, anyOf) keyed by definition.
func (index *SpecIndex) GetPolyReferences() map[string]*Reference {
	return index.polymorphicRefs
}

// GetPolyAllOfReferences returns all references found under allOf keywords.
func (index *SpecIndex) GetPolyAllOfReferences() []*Reference {
	return index.polymorphicAllOfRefs
}

// GetPolyAnyOfReferences returns all references found under anyOf keywords.
func (index *SpecIndex) GetPolyAnyOfReferences() []*Reference {
	return index.polymorphicAnyOfRefs
}

// GetPolyOneOfReferences returns all references found under oneOf keywords.
func (index *SpecIndex) GetPolyOneOfReferences() []*Reference {
	return index.polymorphicOneOfRefs
}

// GetAllCombinedReferences returns a merged map of all standard and polymorphic references.
func (index *SpecIndex) GetAllCombinedReferences() map[string]*Reference {
	combined := make(map[string]*Reference)
	for k, ref := range index.allRefs {
		combined[k] = ref
	}
	for k, ref := range index.polymorphicRefs {
		combined[k] = ref
	}
	return combined
}

// GetRefsByLine returns a map of reference definition to the set of line numbers where it appears.
func (index *SpecIndex) GetRefsByLine() map[string]map[int]bool {
	return index.refsByLine
}

// GetLinesWithReferences returns a set of line numbers that contain at least one reference.
func (index *SpecIndex) GetLinesWithReferences() map[int]bool {
	return index.linesWithRefs
}

// GetMappedReferences returns all resolved component references keyed by definition path.
func (index *SpecIndex) GetMappedReferences() map[string]*Reference {
	return index.allMappedRefs
}

// SetMappedReferences replaces the mapped references for this index.
func (index *SpecIndex) SetMappedReferences(mappedRefs map[string]*Reference) {
	index.allMappedRefs = mappedRefs
}

// GetRawReferencesSequenced returns all raw references in the order they were scanned.
func (index *SpecIndex) GetRawReferencesSequenced() []*Reference {
	return index.rawSequencedRefs
}

// GetExtensionRefsSequenced returns only references that appear under x-* extension paths,
// in scan order.
func (index *SpecIndex) GetExtensionRefsSequenced() []*Reference {
	var extensionRefs []*Reference
	for _, ref := range index.rawSequencedRefs {
		if ref.IsExtensionRef {
			extensionRefs = append(extensionRefs, ref)
		}
	}
	return extensionRefs
}

// GetMappedReferencesSequenced returns all resolved component references in deterministic order.
func (index *SpecIndex) GetMappedReferencesSequenced() []*ReferenceMapped {
	return index.allMappedRefsSequenced
}

// GetOperationParameterReferences returns parameters keyed by path, then HTTP method, then parameter name.
func (index *SpecIndex) GetOperationParameterReferences() map[string]map[string]map[string][]*Reference {
	return index.paramOpRefs
}

// GetAllSchemas returns all schemas (component, inline, and reference) sorted by line number.
func (index *SpecIndex) GetAllSchemas() []*Reference {
	componentSchemas := index.GetAllComponentSchemas()
	inlineSchemas := index.GetAllInlineSchemas()
	refSchemas := index.GetAllReferenceSchemas()
	combined := make([]*Reference, len(inlineSchemas)+len(componentSchemas)+len(refSchemas))
	i := 0
	for x := range inlineSchemas {
		combined[i] = inlineSchemas[x]
		i++
	}
	for x := range componentSchemas {
		combined[i] = componentSchemas[x]
		i++
	}
	for x := range refSchemas {
		combined[i] = refSchemas[x]
		i++
	}
	sort.Slice(combined, func(i, j int) bool {
		return combined[i].Node.Line < combined[j].Node.Line
	})
	return combined
}

// GetAllInlineSchemaObjects returns all inline schema definitions that are objects.
func (index *SpecIndex) GetAllInlineSchemaObjects() []*Reference {
	return index.allInlineSchemaObjectDefinitions
}

// GetAllInlineSchemas returns all inline schema definitions found during extraction.
func (index *SpecIndex) GetAllInlineSchemas() []*Reference {
	return index.allInlineSchemaDefinitions
}

// GetAllReferenceSchemas returns all schema definitions that are $ref references.
func (index *SpecIndex) GetAllReferenceSchemas() []*Reference {
	return index.allRefSchemaDefinitions
}

// GetAllComponentSchemas returns all component schema definitions, converting from the
// internal sync.Map on first access and caching the result.
func (index *SpecIndex) GetAllComponentSchemas() map[string]*Reference {
	if index == nil {
		return nil
	}
	index.allComponentSchemasLock.RLock()
	if index.allComponentSchemas != nil {
		defer index.allComponentSchemasLock.RUnlock()
		return index.allComponentSchemas
	}
	index.allComponentSchemasLock.RUnlock()

	index.allComponentSchemasLock.Lock()
	defer index.allComponentSchemasLock.Unlock()
	if index.allComponentSchemas == nil {
		index.allComponentSchemas = syncMapToMap[string, *Reference](index.allComponentSchemaDefinitions)
	}
	return index.allComponentSchemas
}

// GetAllSecuritySchemes returns all security scheme definitions from the components section.
func (index *SpecIndex) GetAllSecuritySchemes() map[string]*Reference {
	return syncMapToMap[string, *Reference](index.allSecuritySchemes)
}

// GetAllHeaders returns all header definitions from the components section.
func (index *SpecIndex) GetAllHeaders() map[string]*Reference {
	return index.allHeaders
}

// GetAllExternalDocuments returns all external document references found in the specification.
func (index *SpecIndex) GetAllExternalDocuments() map[string]*Reference {
	return index.allExternalDocuments
}

// GetAllExamples returns all example definitions from the components section.
func (index *SpecIndex) GetAllExamples() map[string]*Reference {
	return index.allExamples
}

// GetAllDescriptions returns all description nodes found during indexing.
func (index *SpecIndex) GetAllDescriptions() []*DescriptionReference {
	return index.allDescriptions
}

// GetAllEnums returns all enum definitions found during indexing.
func (index *SpecIndex) GetAllEnums() []*EnumReference {
	return index.allEnums
}

// GetAllObjectsWithProperties returns all objects that have a "properties" keyword.
func (index *SpecIndex) GetAllObjectsWithProperties() []*ObjectReference {
	return index.allObjectsWithProperties
}

// GetAllSummaries returns all summary nodes found during indexing.
func (index *SpecIndex) GetAllSummaries() []*DescriptionReference {
	return index.allSummaries
}

// GetAllRequestBodies returns all request body definitions from the components section.
func (index *SpecIndex) GetAllRequestBodies() map[string]*Reference {
	return index.allRequestBodies
}

// GetAllLinks returns all link definitions from the components section.
func (index *SpecIndex) GetAllLinks() map[string]*Reference {
	return index.allLinks
}

// GetAllParameters returns all parameter definitions from the components section.
func (index *SpecIndex) GetAllParameters() map[string]*Reference {
	return index.allParameters
}

// GetAllResponses returns all response definitions from the components section.
func (index *SpecIndex) GetAllResponses() map[string]*Reference {
	return index.allResponses
}

// GetAllCallbacks returns all callback definitions from the components section.
func (index *SpecIndex) GetAllCallbacks() map[string]*Reference {
	return index.allCallbacks
}

// GetAllComponentPathItems returns all path item definitions from the components section.
func (index *SpecIndex) GetAllComponentPathItems() map[string]*Reference {
	return index.allComponentPathItems
}

// GetInlineOperationDuplicateParameters returns parameters with duplicate names found inline in operations.
func (index *SpecIndex) GetInlineOperationDuplicateParameters() map[string][]*Reference {
	return index.paramInlineDuplicateNames
}

// GetReferencesWithSiblings returns references that have sibling properties alongside the $ref keyword.
func (index *SpecIndex) GetReferencesWithSiblings() map[string]Reference {
	return index.refsWithSiblings
}

// GetAllReferences returns all deduplicated references found during extraction.
func (index *SpecIndex) GetAllReferences() map[string]*Reference {
	return index.allRefs
}

// GetAllSequencedReferences returns all raw references in scan order.
func (index *SpecIndex) GetAllSequencedReferences() []*Reference {
	return index.rawSequencedRefs
}

// GetSchemasNode returns the raw YAML node for the components/schemas (or definitions) section.
func (index *SpecIndex) GetSchemasNode() *yaml.Node {
	return index.schemasNode
}

// GetParametersNode returns the raw YAML node for the components/parameters section.
func (index *SpecIndex) GetParametersNode() *yaml.Node {
	return index.parametersNode
}

// GetReferenceIndexErrors returns any errors that occurred during reference extraction.
func (index *SpecIndex) GetReferenceIndexErrors() []error {
	return index.refErrors
}

// GetOperationParametersIndexErrors returns any errors found when scanning operation parameters.
func (index *SpecIndex) GetOperationParametersIndexErrors() []error {
	return index.operationParamErrors
}

// GetAllPaths returns all path items keyed by path, then HTTP method.
func (index *SpecIndex) GetAllPaths() map[string]map[string]*Reference {
	return index.pathRefs
}

// GetOperationTags returns tags keyed by path, then HTTP method.
func (index *SpecIndex) GetOperationTags() map[string]map[string][]*Reference {
	return index.operationTagsRefs
}

// GetAllParametersFromOperations returns all parameters keyed by path, HTTP method, then parameter name.
func (index *SpecIndex) GetAllParametersFromOperations() map[string]map[string]map[string][]*Reference {
	return index.paramOpRefs
}

// GetRootSecurityReferences returns references from the top-level security requirement array.
func (index *SpecIndex) GetRootSecurityReferences() []*Reference {
	return index.rootSecurity
}

// GetSecurityRequirementReferences returns security requirements keyed by security scheme name.
func (index *SpecIndex) GetSecurityRequirementReferences() map[string]map[string][]*Reference {
	return index.securityRequirementRefs
}

// GetRootSecurityNode returns the raw YAML node for the top-level "security" array.
func (index *SpecIndex) GetRootSecurityNode() *yaml.Node {
	return index.rootSecurityNode
}

// GetRootServersNode returns the raw YAML node for the top-level "servers" array.
func (index *SpecIndex) GetRootServersNode() *yaml.Node {
	return index.rootServersNode
}

// GetAllRootServers returns all server references from the top-level "servers" array.
func (index *SpecIndex) GetAllRootServers() []*Reference {
	return index.serversRefs
}

// GetAllOperationsServers returns server references keyed by path, then HTTP method.
func (index *SpecIndex) GetAllOperationsServers() map[string]map[string][]*Reference {
	return index.opServersRefs
}

// SetAllowCircularReferenceResolving sets whether circular references should be resolved
// instead of returning an error.
func (index *SpecIndex) SetAllowCircularReferenceResolving(allow bool) {
	index.allowCircularReferences = allow
}

// AllowCircularReferenceResolving returns whether circular reference resolving is enabled.
func (index *SpecIndex) AllowCircularReferenceResolving() bool {
	return index.allowCircularReferences
}

func (index *SpecIndex) checkPolymorphicNode(name string) (bool, string) {
	switch name {
	case "anyOf":
		return true, "anyOf"
	case "allOf":
		return true, "allOf"
	case "oneOf":
		return true, "oneOf"
	}
	return false, ""
}

// RegisterSchemaId registers a JSON Schema $id entry in this index's local registry.
func (index *SpecIndex) RegisterSchemaId(entry *SchemaIdEntry) error {
	index.schemaIdRegistryLock.Lock()
	defer index.schemaIdRegistryLock.Unlock()
	if index.schemaIdRegistry == nil {
		index.schemaIdRegistry = make(map[string]*SchemaIdEntry)
	}
	_, err := registerSchemaIdToRegistry(index.schemaIdRegistry, entry, index.logger, "local index")
	return err
}

// GetSchemaById looks up a schema by its resolved $id URI in this index's local registry.
func (index *SpecIndex) GetSchemaById(uri string) *SchemaIdEntry {
	index.schemaIdRegistryLock.RLock()
	defer index.schemaIdRegistryLock.RUnlock()
	if index.schemaIdRegistry == nil {
		return nil
	}
	return index.schemaIdRegistry[uri]
}

// GetAllSchemaIds returns a copy of all $id entries registered in this index.
func (index *SpecIndex) GetAllSchemaIds() map[string]*SchemaIdEntry {
	index.schemaIdRegistryLock.RLock()
	defer index.schemaIdRegistryLock.RUnlock()
	return copySchemaIdRegistry(index.schemaIdRegistry)
}
