// Copyright 2023 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package index

import (
	"context"
	"fmt"
	"hash/maphash"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/pb33f/libopenapi/utils"
	"go.yaml.in/yaml/v4"
)

func (index *SpecIndex) extractDefinitionsAndSchemas(schemasNode *yaml.Node, pathPrefix string) {
	var name string
	var keyNode *yaml.Node
	for i, schema := range schemasNode.Content {
		if i%2 == 0 {
			name = schema.Value
			keyNode = schema
			continue
		}

		def := fmt.Sprintf("%s%s", pathPrefix, name)
		fullDef := fmt.Sprintf("%s%s", index.specAbsolutePath, def)

		ref := &Reference{
			FullDefinition:        fullDef,
			Definition:            def,
			Name:                  name,
			KeyNode:               keyNode,
			Node:                  schema,
			Path:                  fmt.Sprintf("$.components.schemas['%s']", name),
			ParentNode:            schemasNode,
			RequiredRefProperties: extractDefinitionRequiredRefProperties(schemasNode, map[string][]string{}, fullDef, index),
		}
		index.allComponentSchemaDefinitions.Store(def, ref)
	}
}

// extractDefinitionRequiredRefProperties goes through the direct properties of a schema and extracts the map of required definitions from within it
func extractDefinitionRequiredRefProperties(schemaNode *yaml.Node, reqRefProps map[string][]string, fulldef string, idx *SpecIndex) map[string][]string {
	if schemaNode == nil {
		return reqRefProps
	}

	// If the node we're looking at is a direct ref to another model without any properties, mark it as required, but still continue to look for required properties
	isRef, _, defPath := utils.IsNodeRefValue(schemaNode)
	if isRef {
		if _, ok := reqRefProps[defPath]; !ok {
			reqRefProps[defPath] = []string{}
		}
	}

	// Check for a required parameters list, and return if none exists, as any properties will be optional
	_, requiredSeqNode := utils.FindKeyNodeTop("required", schemaNode.Content)
	if requiredSeqNode == nil {
		return reqRefProps
	}

	_, propertiesMapNode := utils.FindKeyNodeTop("properties", schemaNode.Content)
	if propertiesMapNode == nil {
		// TODO: Log a warning on the resolver, because if you have required properties, but no actual properties, something is wrong
		return reqRefProps
	}

	name := ""
	for i, param := range propertiesMapNode.Content {
		if i%2 == 0 {
			name = param.Value
			continue
		}

		// Check to see if the current property is directly embedded within the current schema, and handle its properties if so
		_, paramPropertiesMapNode := utils.FindKeyNodeTop("properties", param.Content)
		if paramPropertiesMapNode != nil {
			reqRefProps = extractDefinitionRequiredRefProperties(param, reqRefProps, fulldef, idx)
		}

		// Check to see if the current property is polymorphic, and dive into that model if so
		for _, key := range []string{"allOf", "oneOf", "anyOf"} {
			_, ofNode := utils.FindKeyNodeTop(key, param.Content)
			if ofNode != nil {
				for _, ofNodeItem := range ofNode.Content {
					reqRefProps = extractRequiredReferenceProperties(fulldef, ofNodeItem, name, reqRefProps)
				}
			}
		}
	}

	// Run through each of the required properties and extract _their_ required references
	for _, requiredPropertyNode := range requiredSeqNode.Content {
		_, requiredPropDefNode := utils.FindKeyNodeTop(requiredPropertyNode.Value, propertiesMapNode.Content)
		if requiredPropDefNode == nil {
			continue
		}

		reqRefProps = extractRequiredReferenceProperties(fulldef, requiredPropDefNode, requiredPropertyNode.Value, reqRefProps)
	}

	return reqRefProps
}

// extractRequiredReferenceProperties returns a map of definition names to the property or properties which reference it within a node
func extractRequiredReferenceProperties(fulldef string, requiredPropDefNode *yaml.Node, propName string, reqRefProps map[string][]string) map[string][]string {
	isRef, _, refName := utils.IsNodeRefValue(requiredPropDefNode)
	if !isRef {
		_, defItems := utils.FindKeyNodeTop("items", requiredPropDefNode.Content)
		if defItems != nil {
			isRef, _, refName = utils.IsNodeRefValue(defItems)
		}
	}

	if /* still */ !isRef {
		return reqRefProps
	}

	defPath := fulldef

	if strings.HasPrefix(refName, "http") || filepath.IsAbs(refName) {
		defPath = refName
	} else {
		exp := strings.Split(fulldef, "#/")
		if len(exp) == 2 {
			if exp[0] != "" {
				if strings.HasPrefix(exp[0], "http") {
					u, _ := url.Parse(exp[0])
					r := strings.Split(refName, "#/")
					if len(r) == 2 {
						var abs string
						if r[0] == "" {
							abs = u.Path
						} else {
							abs, _ = filepath.Abs(utils.CheckPathOverlap(filepath.Dir(u.Path), r[0],
								string(os.PathSeparator)))
						}

						u.Path = utils.ReplaceWindowsDriveWithLinuxPath(abs)
						u.Fragment = ""
						defPath = fmt.Sprintf("%s#/%s", u.String(), r[1])
					} else {
						u.Path = utils.ReplaceWindowsDriveWithLinuxPath(utils.CheckPathOverlap(filepath.Dir(u.Path),
							r[0], string(os.PathSeparator)))
						u.Fragment = ""
						defPath = u.String()
					}
				} else {
					r := strings.Split(refName, "#/")
					if len(r) == 2 {
						var abs string
						if r[0] == "" {
							abs, _ = filepath.Abs(exp[0])
						} else {
							abs, _ = filepath.Abs(utils.CheckPathOverlap(filepath.Dir(exp[0]), r[0],
								string(os.PathSeparator)))

							// abs, _ = filepath.Abs(filepath.Join(filepath.Dir(exp[0]), r[0],
							//	string('J')))
						}

						defPath = fmt.Sprintf("%s#/%s", abs, r[1])
					} else {
						defPath, _ = filepath.Abs(utils.CheckPathOverlap(filepath.Dir(exp[0]),
							r[0], string(os.PathSeparator)))
					}
				}
			} else {
				defPath = refName
			}
		} else {
			if strings.HasPrefix(exp[0], "http") {
				u, _ := url.Parse(exp[0])
				r := strings.Split(refName, "#/")
				if len(r) == 2 {
					abs, _ := filepath.Abs(utils.CheckPathOverlap(filepath.Dir(u.Path), r[0], string(os.PathSeparator)))
					u.Path = utils.ReplaceWindowsDriveWithLinuxPath(abs)
					u.Fragment = ""
					defPath = fmt.Sprintf("%s#/%s", u.String(), r[1])
				} else {
					u.Path = utils.ReplaceWindowsDriveWithLinuxPath(utils.CheckPathOverlap(filepath.Dir(u.Path),
						r[0], string(os.PathSeparator)))
					u.Fragment = ""
					defPath = u.String()
				}
			} else {
				defPath, _ = filepath.Abs(utils.CheckPathOverlap(filepath.Dir(exp[0]), refName, string(os.PathSeparator)))
			}
		}
	}

	if _, ok := reqRefProps[defPath]; !ok {
		reqRefProps[defPath] = []string{}
	}
	reqRefProps[defPath] = append(reqRefProps[defPath], propName)

	return reqRefProps
}

func (index *SpecIndex) extractComponentParameters(paramsNode *yaml.Node, pathPrefix string) {
	var name string
	var keyNode *yaml.Node
	for i, param := range paramsNode.Content {
		if i%2 == 0 {
			name = param.Value
			keyNode = param
			continue
		}
		def := fmt.Sprintf("%s%s", pathPrefix, name)
		ref := &Reference{
			Definition: def,
			Name:       name,
			Node:       param,
			KeyNode:    keyNode,
		}
		index.allParameters[def] = ref
	}
}

func (index *SpecIndex) extractComponentRequestBodies(requestBodiesNode *yaml.Node, pathPrefix string) {
	var name string
	var keyNode *yaml.Node
	for i, reqBod := range requestBodiesNode.Content {
		if i%2 == 0 {
			name = reqBod.Value
			keyNode = reqBod
			continue
		}
		def := fmt.Sprintf("%s%s", pathPrefix, name)
		ref := &Reference{
			Definition: def,
			Name:       name,
			Node:       reqBod,
			KeyNode:    keyNode,
		}
		index.allRequestBodies[def] = ref
	}
}

func (index *SpecIndex) extractComponentResponses(responsesNode *yaml.Node, pathPrefix string) {
	var name string
	var keyNode *yaml.Node
	for i, response := range responsesNode.Content {
		if i%2 == 0 {
			name = response.Value
			keyNode = response
			continue
		}
		def := fmt.Sprintf("%s%s", pathPrefix, name)
		ref := &Reference{
			Definition: def,
			Name:       name,
			Node:       response,
			KeyNode:    keyNode,
		}
		index.allResponses[def] = ref
	}
}

func (index *SpecIndex) extractComponentHeaders(headersNode *yaml.Node, pathPrefix string) {
	var name string
	var keyNode *yaml.Node
	for i, header := range headersNode.Content {
		if i%2 == 0 {
			name = header.Value
			keyNode = header
			continue
		}
		def := fmt.Sprintf("%s%s", pathPrefix, name)
		ref := &Reference{
			Definition: def,
			Name:       name,
			Node:       header,
			KeyNode:    keyNode,
		}
		index.allHeaders[def] = ref
	}
}

func (index *SpecIndex) extractComponentCallbacks(callbacksNode *yaml.Node, pathPrefix string) {
	var name string
	var keyNode *yaml.Node
	for i, callback := range callbacksNode.Content {
		if i%2 == 0 {
			name = callback.Value
			keyNode = callback
			continue
		}
		def := fmt.Sprintf("%s%s", pathPrefix, name)
		ref := &Reference{
			Definition: def,
			Name:       name,
			Node:       callback,
			KeyNode:    keyNode,
		}
		index.allCallbacks[def] = ref
	}
}

func (index *SpecIndex) extractComponentPathItems(pathItemsNode *yaml.Node, pathPrefix string) {
	var name string
	var keyNode *yaml.Node
	for i, pathItemName := range pathItemsNode.Content {
		if i%2 == 0 {
			name = pathItemName.Value
			keyNode = pathItemName
			continue
		}
		def := fmt.Sprintf("%s%s", pathPrefix, name)
		ref := &Reference{
			Definition: def,
			Name:       name,
			Node:       pathItemName,
			KeyNode:    keyNode,
		}
		index.allComponentPathItems[def] = ref
	}
}

func (index *SpecIndex) extractComponentLinks(linksNode *yaml.Node, pathPrefix string) {
	var name string
	var keyNode *yaml.Node
	for i, link := range linksNode.Content {
		if i%2 == 0 {
			name = link.Value
			keyNode = link
			continue
		}
		def := fmt.Sprintf("%s%s", pathPrefix, name)
		ref := &Reference{
			Definition: def,
			Name:       name,
			Node:       link,
			KeyNode:    keyNode,
		}
		index.allLinks[def] = ref
	}
}

func (index *SpecIndex) extractComponentExamples(examplesNode *yaml.Node, pathPrefix string) {
	var name string
	var keyNode *yaml.Node
	for i, example := range examplesNode.Content {
		if i%2 == 0 {
			name = example.Value
			keyNode = example
			continue
		}
		def := fmt.Sprintf("%s%s", pathPrefix, name)
		ref := &Reference{
			Definition: def,
			Name:       name,
			Node:       example,
			KeyNode:    keyNode,
		}
		index.allExamples[def] = ref
	}
}

func (index *SpecIndex) extractComponentSecuritySchemes(securitySchemesNode *yaml.Node, pathPrefix string) {
	var name string
	var keyNode *yaml.Node
	for i, schema := range securitySchemesNode.Content {
		if i%2 == 0 {
			name = schema.Value
			keyNode = schema
			continue
		}
		def := fmt.Sprintf("%s%s", pathPrefix, name)
		fullDef := fmt.Sprintf("%s%s", index.specAbsolutePath, def)

		ref := &Reference{
			FullDefinition:        fullDef,
			Definition:            def,
			Name:                  name,
			Node:                  schema,
			KeyNode:               keyNode,
			Path:                  fmt.Sprintf("$.components.securitySchemes.%s", name),
			ParentNode:            securitySchemesNode,
			RequiredRefProperties: extractDefinitionRequiredRefProperties(securitySchemesNode, map[string][]string{}, fullDef, index),
		}
		index.allSecuritySchemes.Store(def, ref)
	}
}

func (index *SpecIndex) countUniqueInlineDuplicates() int {
	if index.componentsInlineParamUniqueCount > 0 {
		return index.componentsInlineParamUniqueCount
	}
	unique := 0
	for _, p := range index.paramInlineDuplicateNames {
		if len(p) == 1 {
			unique++
		}
	}
	index.componentsInlineParamUniqueCount = unique
	return unique
}

func seekRefEnd(ctx context.Context, index *SpecIndex, refName string) *Reference {
	ref, _, nCtx := index.SearchIndexForReferenceWithContext(ctx, refName)
	if ref != nil {
		if ok, _, v := utils.IsNodeRefValue(ref.Node); ok {
			return seekRefEnd(nCtx, ref.Index, v)
		}
	}
	return ref
}

// formatParameterPath creates a consistent JSON path for parameter error messages
func formatParameterPath(pathValue, method string, index int) string {
	if method == "top" {
		return fmt.Sprintf("$.paths['%s'].parameters[%d]", pathValue, index)
	}
	return fmt.Sprintf("$.paths['%s'].%s.parameters[%d]", pathValue, method, index)
}

func (index *SpecIndex) scanOperationParams(params []*yaml.Node, keyNode, pathItemNode *yaml.Node, method string) {
	for i, param := range params {
		// param is ref
		if len(param.Content) > 0 && param.Content[0].Value == "$ref" {

			paramRefName := param.Content[1].Value
			paramRef := index.allMappedRefs[paramRefName]
			if paramRef == nil {
				// could be in the rolodex
				searchInIndex := findIndex(index, param.Content[1])
				ctx := context.WithValue(context.Background(), CurrentPathKey, searchInIndex.specAbsolutePath)
				ctx = context.WithValue(ctx, RootIndexKey, searchInIndex)
				ref := seekRefEnd(ctx, searchInIndex, paramRefName)
				if ref != nil {
					paramRef = ref
					if strings.Contains(paramRefName, "%") {
						paramRefName, _ = url.QueryUnescape(paramRefName)
					}
				}
			}

			if index.paramOpRefs[pathItemNode.Value] == nil {
				index.paramOpRefs[pathItemNode.Value] = make(map[string]map[string][]*Reference)
				index.paramOpRefs[pathItemNode.Value][method] = make(map[string][]*Reference)

			}
			// if we know the path, but it's a new method
			if index.paramOpRefs[pathItemNode.Value][method] == nil {
				index.paramOpRefs[pathItemNode.Value][method] = make(map[string][]*Reference)
			}

			// if this is a duplicate, add an error and ignore it
			if index.paramOpRefs[pathItemNode.Value][method][paramRefName] != nil {
				path := formatParameterPath(pathItemNode.Value, method, i)

				index.operationParamErrors = append(index.operationParamErrors, &IndexingError{
					Err: fmt.Errorf("the `%s` operation parameter at path `%s`, "+
						"index %d has a duplicate ref `%s`", strings.ToUpper(method), pathItemNode.Value, i, paramRefName),
					Node: param,
					Path: path,
				})
			} else {
				if paramRef != nil {
					index.paramOpRefs[pathItemNode.Value][method][paramRefName] = append(index.paramOpRefs[pathItemNode.Value][method][paramRefName], paramRef)
				}
			}

			continue

		} else {

			// param is inline.
			_, vn := utils.FindKeyNode("name", param.Content)

			path := formatParameterPath(pathItemNode.Value, method, i)

			if vn == nil {
				index.operationParamErrors = append(index.operationParamErrors, &IndexingError{
					Err: fmt.Errorf("the `%s` operation parameter at path `%s`, index %d has no `name` value",
						strings.ToUpper(method), pathItemNode.Value, i),
					Node: param,
					Path: path,
				})
				continue
			}

			ref := &Reference{
				Definition: vn.Value,
				Name:       vn.Value,
				Node:       param,
				KeyNode:    keyNode,
				Path:       path,
			}

			// cache the 'in' value for performance optimization (fix for issue #379)
			_, inNode := utils.FindKeyNodeTop("in", param.Content)
			if inNode != nil {
				ref.In = inNode.Value
			}
			if index.paramOpRefs[pathItemNode.Value] == nil {
				index.paramOpRefs[pathItemNode.Value] = make(map[string]map[string][]*Reference)
				index.paramOpRefs[pathItemNode.Value][method] = make(map[string][]*Reference)
			}

			// if we know the path but this is a new method.
			if index.paramOpRefs[pathItemNode.Value][method] == nil {
				index.paramOpRefs[pathItemNode.Value][method] = make(map[string][]*Reference)
			}

			// Fix for issue #379: Ensure consistent parameter counting regardless of ordering
			// https://github.com/pb33f/libopenapi/issues/379
			// check if this parameter name already exists, and detect duplicates in the same location
			if len(index.paramOpRefs[pathItemNode.Value][method][ref.Name]) > 0 {

				checkNodes := index.paramOpRefs[pathItemNode.Value][method][ref.Name]

				// check if there's a duplicate with the same 'in' type (query, path, header, cookie)
				hasDuplicateInSameLocation := false
				for _, checkNode := range checkNodes {
					// both must have 'in' values and they must match to be a duplicate
					if ref.In != "" && checkNode.In != "" && checkNode.In == ref.In {
						// found a duplicate parameter with same name and location
						hasDuplicateInSameLocation = true

						index.operationParamErrors = append(index.operationParamErrors, &IndexingError{
							Err: fmt.Errorf("the `%s` operation parameter at path `%s`, "+
								"index %d has a duplicate name `%s` and `in` type", strings.ToUpper(method), pathItemNode.Value, i, vn.Value),
							Node: param,
							Path: path,
						})
						break // no need to check further once duplicate found
					}
				}

				// only add the parameter if it's not a duplicate in the same location
				if !hasDuplicateInSameLocation {
					index.paramOpRefs[pathItemNode.Value][method][ref.Name] = append(index.paramOpRefs[pathItemNode.Value][method][ref.Name], ref)
				}
			} else {
				// First parameter with this name, add it
				index.paramOpRefs[pathItemNode.Value][method][ref.Name] = append(index.paramOpRefs[pathItemNode.Value][method][ref.Name], ref)
			}
			continue
		}
	}
}

func findIndex(index *SpecIndex, i *yaml.Node) *SpecIndex {
	rolodex := index.GetRolodex()
	if rolodex == nil {
		return index
	}
	allIndexes := rolodex.GetIndexes()
	for _, searchIndex := range allIndexes {
		nodeMap := searchIndex.GetNodeMap()
		line, ok := nodeMap[i.Line]
		if !ok {
			continue
		}
		node, ok := line[i.Column]
		if !ok {
			continue
		}
		if node == i {
			return searchIndex
		}
	}
	return index
}

func runIndexFunction(funcs []func() int, wg *sync.WaitGroup) {
	for _, cFunc := range funcs {
		go func(wg *sync.WaitGroup, cf func() int) {
			cf()
			wg.Done()
		}(wg, cFunc)
	}
}

func GenerateCleanSpecConfigBaseURL(baseURL *url.URL, dir string, includeFile bool) string {
	cleanedPath := baseURL.Path // not cleaned yet!

	// create a slice of path segments from existing path
	pathSegs := strings.Split(cleanedPath, "/")
	dirSegs := strings.Split(dir, "/")

	var cleanedSegs []string
	if !includeFile {
		dirSegs = dirSegs[:len(dirSegs)-1]
	}

	// relative paths are a pain in the ass, damn you digital ocean, use a single spec, and break them
	// down into services, please don't blast apart specs into a billion shards.
	if strings.Contains(dir, "../") {
		for s := range dirSegs {
			if dirSegs[s] == ".." {
				// chop off the last segment of the base path.
				if len(pathSegs) > 0 {
					pathSegs = pathSegs[:len(pathSegs)-1]
				}
			} else {
				cleanedSegs = append(cleanedSegs, dirSegs[s])
			}
		}
		cleanedPath = fmt.Sprintf("%s/%s", strings.Join(pathSegs, "/"), strings.Join(cleanedSegs, "/"))
	} else {
		if !strings.HasPrefix(dir, "http") {
			if len(pathSegs) > 1 || len(dirSegs) > 1 {
				cleanedPath = fmt.Sprintf("%s/%s", strings.Join(pathSegs, "/"), strings.Join(dirSegs, "/"))
			}
		} else {
			cleanedPath = strings.Join(dirSegs, "/")
		}
	}
	var p string
	if baseURL.Scheme != "" && !strings.HasPrefix(dir, "http") {
		p = fmt.Sprintf("%s://%s%s", baseURL.Scheme, baseURL.Host, cleanedPath)
	} else {
		if !strings.Contains(cleanedPath, "/") {
			p = ""
		} else {
			p = cleanedPath
		}
	}
	return strings.TrimSuffix(p, "/")
}

func syncMapToMap[K comparable, V any](sm *sync.Map) map[K]V {
	if sm == nil {
		return nil
	}

	m := make(map[K]V)

	sm.Range(func(key, value interface{}) bool {
		m[key.(K)] = value.(V)
		return true
	})

	return m
}

// ClearHashCache clears the hash cache - useful for testing and memory management
func ClearHashCache() {
	nodeHashCache.Range(func(key, value interface{}) bool {
		nodeHashCache.Delete(key)
		return true
	})
}

// hasherPool pools maphash.Hash instances to avoid allocations.
// maphash is ~15x faster than SHA256 and has native WriteString support.
var hasherPool = sync.Pool{
	New: func() interface{} {
		h := &maphash.Hash{}
		h.SetSeed(globalHashSeed) // ensure consistent hashes across pooled instances
		return h
	},
}

// stackPool pools node pointer slices for HashNode traversal.
// avoids allocating ~1KB per HashNode call.
var stackPool = sync.Pool{
	New: func() interface{} {
		s := make([]*yaml.Node, 0, 128)
		return &s
	},
}

// visitedPool pools visited maps for circular reference detection.
// avoids allocating ~2KB per HashNode call.
var visitedPool = sync.Pool{
	New: func() interface{} {
		return make(map[*yaml.Node]struct{}, 64)
	},
}

// nodeHashCache caches hash results by node pointer for repeated lookups.
// yaml.Node pointers are stable for the document lifetime.
var nodeHashCache = sync.Map{} // *yaml.Node -> string

// hashCacheThreshold determines when to cache hash results.
// lowered from 200 to 20 for more aggressive caching of repeated patterns.
const hashCacheThreshold = 20

// globalHashSeed ensures all maphash instances produce consistent results.
// maphash uses random seeds by default; we need deterministic hashes for caching.
var globalHashSeed maphash.Seed

// emptyNodeHash is the hash of a nil node (computed once at init).
var emptyNodeHash string

func init() {
	globalHashSeed = maphash.MakeSeed()
	var h maphash.Hash
	h.SetSeed(globalHashSeed)
	emptyNodeHash = strconv.FormatUint(h.Sum64(), 16)
}

// writeIntToHash writes a non-negative integer to the hash without heap allocations.
// Uses a stack-allocated buffer. Line/Column values are always non-negative.
func writeIntToHash(h *maphash.Hash, n int) {
	if n == 0 {
		h.WriteByte('0')
		return
	}
	// max int64 is 19 digits, 20 is safe
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	h.Write(buf[i:])
}

// HashNode returns a fast hash string of the node and its children.
// Uses maphash (same algorithm as Go maps) with WriteString for zero allocations.
// Iterative traversal avoids recursion overhead.
func HashNode(n *yaml.Node) string {
	if n == nil {
		return emptyNodeHash
	}

	// check cache first (by pointer - yaml.Node pointers are stable)
	if cached, ok := nodeHashCache.Load(n); ok {
		return cached.(string)
	}

	// get hasher from pool
	h := hasherPool.Get().(*maphash.Hash)
	h.Reset()
	defer hasherPool.Put(h)

	// get stack from pool, reset length but keep capacity
	stackPtr := stackPool.Get().(*[]*yaml.Node)
	stack := (*stackPtr)[:0]
	stack = append(stack, n)
	defer func() {
		*stackPtr = stack[:0]
		stackPool.Put(stackPtr)
	}()

	// get visited map from pool, clear entries
	visited := visitedPool.Get().(map[*yaml.Node]struct{})
	clear(visited)
	defer func() {
		clear(visited)
		visitedPool.Put(visited)
	}()

	for len(stack) > 0 {
		// pop from stack
		node := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		if node == nil {
			continue
		}

		// skip already visited nodes (handles circular references)
		if _, seen := visited[node]; seen {
			continue
		}
		visited[node] = struct{}{}

		// hash node content - WriteString for strings, writeIntToHash for ints (zero allocations)
		h.WriteString(node.Tag)
		writeIntToHash(h, node.Line)
		writeIntToHash(h, node.Column)
		h.WriteString(node.Value)

		// push children in reverse order for correct traversal order
		for i := len(node.Content) - 1; i >= 0; i-- {
			stack = append(stack, node.Content[i])
		}
	}

	result := strconv.FormatUint(h.Sum64(), 16)

	// cache result for nodes with children (likely to be looked up again)
	if len(n.Content) >= hashCacheThreshold {
		nodeHashCache.Store(n, result)
	}

	return result
}
