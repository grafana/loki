// Copyright 2022-2026 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package v3

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pb33f/libopenapi/datamodel"
	"github.com/pb33f/libopenapi/datamodel/low"
	"github.com/pb33f/libopenapi/datamodel/low/base"
	"github.com/pb33f/libopenapi/index"
	"github.com/pb33f/libopenapi/orderedmap"
	"github.com/pb33f/libopenapi/utils"
	"go.yaml.in/yaml/v4"
)

type documentTopLevelNode struct {
	key   *yaml.Node
	value *yaml.Node
}

type documentTopLevelNodes struct {
	version           documentTopLevelNode
	jsonSchemaDialect documentTopLevelNode
	self              documentTopLevelNode
	info              documentTopLevelNode
	servers           documentTopLevelNode
	tags              documentTopLevelNode
	components        documentTopLevelNode
	security          documentTopLevelNode
	externalDocs      documentTopLevelNode
	paths             documentTopLevelNode
	webhooks          documentTopLevelNode
}

func selectDocumentNode(root *yaml.Node, preferred documentTopLevelNode, label string, topOnly bool) documentTopLevelNode {
	if preferred.value != nil {
		return preferred
	}
	root = utils.NodeAlias(root)
	if root == nil {
		return documentTopLevelNode{}
	}
	utils.CheckForMergeNodes(root)
	if topOnly {
		_, key, value := utils.FindKeyNodeFullTop(label, root.Content)
		return documentTopLevelNode{key: key, value: value}
	}
	_, key, value := utils.FindKeyNodeFull(label, root.Content)
	return documentTopLevelNode{key: key, value: value}
}

func collectDocumentTopLevelNodes(root *yaml.Node) documentTopLevelNodes {
	root = utils.NodeAlias(root)
	var nodes documentTopLevelNodes
	if root == nil {
		return nodes
	}
	utils.CheckForMergeNodes(root)

	content := root.Content
	for i := 0; i+1 < len(content); i += 2 {
		keyNode := utils.NodeAlias(content[i])
		valueNode := utils.NodeAlias(content[i+1])
		switch keyNode.Value {
		case OpenAPILabel:
			if nodes.version.value == nil {
				nodes.version = documentTopLevelNode{key: keyNode, value: valueNode}
			}
		case JSONSchemaDialectLabel:
			if nodes.jsonSchemaDialect.value == nil {
				nodes.jsonSchemaDialect = documentTopLevelNode{key: keyNode, value: valueNode}
			}
		case SelfLabel:
			if nodes.self.value == nil {
				nodes.self = documentTopLevelNode{key: keyNode, value: valueNode}
			}
		case base.InfoLabel:
			if nodes.info.value == nil {
				nodes.info = documentTopLevelNode{key: keyNode, value: valueNode}
			}
		case ServersLabel:
			if nodes.servers.value == nil {
				nodes.servers = documentTopLevelNode{key: keyNode, value: valueNode}
			}
		case base.TagsLabel:
			if nodes.tags.value == nil {
				nodes.tags = documentTopLevelNode{key: keyNode, value: valueNode}
			}
		case ComponentsLabel:
			if nodes.components.value == nil {
				nodes.components = documentTopLevelNode{key: keyNode, value: valueNode}
			}
		case SecurityLabel:
			if nodes.security.value == nil {
				nodes.security = documentTopLevelNode{key: keyNode, value: valueNode}
			}
		case base.ExternalDocsLabel:
			if nodes.externalDocs.value == nil {
				nodes.externalDocs = documentTopLevelNode{key: keyNode, value: valueNode}
			}
		case PathsLabel:
			if nodes.paths.value == nil {
				nodes.paths = documentTopLevelNode{key: keyNode, value: valueNode}
			}
		case WebhooksLabel:
			if nodes.webhooks.value == nil {
				nodes.webhooks = documentTopLevelNode{key: keyNode, value: valueNode}
			}
		}
	}

	return nodes
}

// CreateDocument will create a new Document instance from the provided SpecInfo.
//
// Deprecated: Use CreateDocumentFromConfig instead. This function will be removed in a later version, it
// defaults to allowing file and remote references, and does not support relative file references.
func CreateDocument(info *datamodel.SpecInfo) (*Document, error) {
	return createDocument(info, datamodel.NewDocumentConfiguration())
}

// CreateDocumentFromConfig Create a new document from the provided SpecInfo and DocumentConfiguration pointer.
func CreateDocumentFromConfig(info *datamodel.SpecInfo, config *datamodel.DocumentConfiguration) (*Document, error) {
	return createDocument(info, config)
}

func createDocument(info *datamodel.SpecInfo, config *datamodel.DocumentConfiguration) (*Document, error) {
	rootNode := utils.NodeAlias(info.RootNode.Content[0])
	topNodes := collectDocumentTopLevelNodes(rootNode)
	versionNodeRef := selectDocumentNode(rootNode, topNodes.version, OpenAPILabel, false)
	labelNode, versionNode := versionNodeRef.key, versionNodeRef.value
	var version low.NodeReference[string]
	if versionNode == nil {
		return nil, errors.New("no openapi version/tag found, cannot create document")
	}
	version = low.NodeReference[string]{Value: versionNode.Value, KeyNode: labelNode, ValueNode: versionNode}
	doc := Document{Version: version}
	doc.Nodes = low.ExtractNodes(nil, rootNode)
	// create an index config and shadow the document configuration.
	idxConfig := index.CreateClosedAPIIndexConfig()
	idxConfig.SpecInfo = info
	idxConfig.UseSchemaQuickHash = config.UseSchemaQuickHash
	idxConfig.ExcludeExtensionRefs = config.ExcludeExtensionRefs
	idxConfig.IgnoreArrayCircularReferences = config.IgnoreArrayCircularReferences
	idxConfig.IgnorePolymorphicCircularReferences = config.IgnorePolymorphicCircularReferences
	idxConfig.AllowUnknownExtensionContentDetection = config.AllowUnknownExtensionContentDetection
	idxConfig.TransformSiblingRefs = config.TransformSiblingRefs
	idxConfig.SkipExternalRefResolution = config.SkipExternalRefResolution
	idxConfig.ResolveNestedRefsWithDocumentContext = config.ResolveNestedRefsWithDocumentContext
	idxConfig.AvoidCircularReferenceCheck = true

	// handle $self field for OpenAPI 3.2+ documents
	baseURL := config.BaseURL
	if info.Self != "" {
		selfURL, err := url.Parse(info.Self)
		if err != nil {
			// log error but continue with original config
			if config.Logger != nil {
				config.Logger.Error("$self field contains invalid URL", "self", info.Self, "error", err)
			}
			// store error in spec info for later retrieval
			if info.Error == nil {
				info.Error = fmt.Errorf("$self field contains invalid URL: %w", err)
			}
		} else if strings.HasPrefix(info.Self, "http") {
			// validate http/https URLs
			if config.BaseURL != nil {
				// conflict detected
				if config.Logger != nil {
					config.Logger.Error("BaseURL and $self have been set and conflict, defaulting to BaseURL",
						"baseURL", config.BaseURL.String(), "self", info.Self)
				}
				// use config BaseURL (programmatic control trumps document)
			} else {
				// use $self as BaseURL
				baseURL = selfURL
			}
		} else {
			// for non-http URLs (like file:// or custom schemes), use as-is if no conflict
			if config.BaseURL != nil {
				if config.Logger != nil {
					config.Logger.Error("BaseURL and $self have been set and conflict, defaulting to BaseURL",
						"baseURL", config.BaseURL.String(), "self", info.Self)
				}
			} else {
				baseURL = selfURL
			}
		}
	}

	idxConfig.BaseURL = urlWithoutTrailingSlash(baseURL)
	idxConfig.BasePath = config.BasePath
	idxConfig.SpecFilePath = config.SpecFilePath
	idxConfig.Logger = config.Logger
	extract := config.ExtractRefsSequentially
	idxConfig.ExtractRefsSequentially = extract
	rolodex := index.NewRolodex(idxConfig)
	rolodex.SetRootNode(info.RootNode)
	doc.Rolodex = rolodex

	// If basePath is provided, add a local filesystem to the rolodex.
	if idxConfig.BasePath != "" || config.AllowFileReferences {
		var cwd string
		cwd, _ = filepath.Abs(config.BasePath)
		// if a supplied local filesystem is provided, add it to the rolodex.
		if config.LocalFS != nil {
			var localFS index.RolodexFS
			if fs, ok := config.LocalFS.(index.RolodexFS); ok {
				localFS = fs
			} else {
				// wrap a plain fs.FS so it can be indexed.
				localFSConf := index.LocalFSConfig{
					BaseDirectory: cwd,
					IndexConfig:   idxConfig,
					FileFilters:   config.FileFilter,
					DirFS:         config.LocalFS,
				}

				localFS, _ = index.NewLocalFSWithConfig(&localFSConf)
				idxConfig.AllowFileLookup = true
			}

			rolodex.AddLocalFS(cwd, localFS)
		} else {

			// create a local filesystem
			localFSConf := index.LocalFSConfig{
				BaseDirectory: cwd,
				IndexConfig:   idxConfig,
				FileFilters:   config.FileFilter,
			}

			fileFS, _ := index.NewLocalFSWithConfig(&localFSConf)
			idxConfig.AllowFileLookup = true

			// add the filesystem to the rolodex
			rolodex.AddLocalFS(cwd, fileFS)
		}
	}
	// Only create a remote filesystem when the caller explicitly allows remote references.
	if config.AllowRemoteReferences {

		// create a remote filesystem
		remoteFS, _ := index.NewRemoteFSWithConfig(idxConfig)
		if config.RemoteURLHandler != nil {
			remoteFS.RemoteHandlerFunc = config.RemoteURLHandler
		}
		idxConfig.AllowRemoteLookup = true

		// add to the rolodex
		u := "default"
		if config.BaseURL != nil {
			u = config.BaseURL.String()
		}
		rolodex.AddRemoteFS(u, remoteFS)
	}

	// index the rolodex
	var errs []error

	// index all the things.
	if config.Logger != nil {
		config.Logger.Debug("indexing rolodex")
	}
	now := time.Now()
	_ = rolodex.IndexTheRolodex(context.Background())
	done := time.Duration(time.Since(now).Milliseconds())
	if config.Logger != nil {
		config.Logger.Debug("rolodex indexed", "ms", done)
	}
	// check for circular references
	if config.Logger != nil {
		config.Logger.Debug("checking for circular references")
	}
	now = time.Now()
	if !config.SkipCircularReferenceCheck {
		rolodex.CheckForCircularReferences()
	}
	done = time.Duration(time.Since(now).Milliseconds())
	if config.Logger != nil {
		if !config.SkipCircularReferenceCheck {
			config.Logger.Debug("circular check completed", "ms", done)
		}
	}
	// extract errors
	roloErrs := rolodex.GetCaughtErrors()
	if roloErrs != nil {
		errs = append(errs, roloErrs...)
	}

	// set root index.
	doc.Index = rolodex.GetRootIndex()
	var wg sync.WaitGroup

	var cacheMap sync.Map
	modelContext := base.ModelContext{SchemaCache: &cacheMap}
	ctx := context.WithValue(context.Background(), "modelCtx", &modelContext)

	doc.Extensions = low.ExtractExtensions(rootNode)
	low.ExtractExtensionNodes(ctx, doc.Extensions, doc.Nodes)

	// if set, extract jsonSchemaDialect (3.1)
	dialectRef := selectDocumentNode(rootNode, topNodes.jsonSchemaDialect, JSONSchemaDialectLabel, false)
	dialectLabel, dialectNode := dialectRef.key, dialectRef.value
	if dialectNode != nil {
		doc.JsonSchemaDialect = low.NodeReference[string]{
			Value: dialectNode.Value, KeyNode: dialectLabel, ValueNode: dialectNode,
		}
	}

	// if set, extract $self (3.2)
	selfRef := selectDocumentNode(rootNode, topNodes.self, SelfLabel, false)
	selfLabel, selfNode := selfRef.key, selfRef.value
	if selfNode != nil {
		doc.Self = low.NodeReference[string]{
			Value: selfNode.Value, KeyNode: selfLabel, ValueNode: selfNode,
		}
	}

	extractionFuncs := []func(ctx context.Context, root *yaml.Node, n documentTopLevelNodes, d *Document, idx *index.SpecIndex) error{
		extractInfo,
		extractServers,
		extractTags,
		extractComponents,
		extractSecurity,
		extractExternalDocs,
		extractPaths,
		extractWebhooks,
	}

	wg.Add(len(extractionFuncs))
	var errsMu sync.Mutex
	if config.Logger != nil {
		config.Logger.Debug("running extractions")
	}
	now = time.Now()
	for _, f := range extractionFuncs {
		go func(runFunc func(ctx context.Context, root *yaml.Node, n documentTopLevelNodes, d *Document, idx *index.SpecIndex) error) {
			defer wg.Done()
			if er := runFunc(ctx, rootNode, topNodes, &doc, rolodex.GetRootIndex()); er != nil {
				errsMu.Lock()
				errs = append(errs, er)
				errsMu.Unlock()
			}
		}(f)
	}
	wg.Wait()
	done = time.Duration(time.Since(now).Milliseconds())
	if config.Logger != nil {
		config.Logger.Debug("extractions complete", "time", done)
	}
	return &doc, errors.Join(errs...)
}

func extractInfo(ctx context.Context, root *yaml.Node, nodes documentTopLevelNodes, doc *Document, idx *index.SpecIndex) error {
	nodeRef := selectDocumentNode(root, nodes.info, base.InfoLabel, true)
	ln, vn := nodeRef.key, nodeRef.value
	if vn != nil {
		ir := base.Info{}
		_ = low.BuildModel(vn, &ir)
		_ = ir.Build(ctx, ln, vn, idx)
		nr := low.NodeReference[*base.Info]{Value: &ir, ValueNode: vn, KeyNode: ln}
		doc.Info = nr
	}
	return nil
}

func extractSecurity(ctx context.Context, root *yaml.Node, nodes documentTopLevelNodes, doc *Document, idx *index.SpecIndex) error {
	sec, ln, vn, err := low.ExtractArray[*base.SecurityRequirement](ctx, SecurityLabel, root, idx)
	if err != nil {
		return err
	}
	if vn != nil && ln != nil {
		doc.Security = low.NodeReference[[]low.ValueReference[*base.SecurityRequirement]]{Value: sec, KeyNode: ln, ValueNode: vn}
	}
	return nil
}

func extractExternalDocs(ctx context.Context, root *yaml.Node, nodes documentTopLevelNodes, doc *Document, idx *index.SpecIndex) error {
	extDocs, err := low.ExtractObject[*base.ExternalDoc](ctx, base.ExternalDocsLabel, root, idx)
	if err != nil {
		return err
	}
	doc.ExternalDocs = extDocs
	return nil
}

func extractComponents(ctx context.Context, root *yaml.Node, nodes documentTopLevelNodes, doc *Document, idx *index.SpecIndex) error {
	nodeRef := selectDocumentNode(root, nodes.components, ComponentsLabel, true)
	ln, vn := nodeRef.key, nodeRef.value
	if vn != nil {
		ir := Components{}
		_ = low.BuildModel(vn, &ir)
		err := ir.Build(ctx, vn, idx)
		if err != nil {
			return err
		}
		nr := low.NodeReference[*Components]{Value: &ir, ValueNode: vn, KeyNode: ln}
		doc.Components = nr
	}
	return nil
}

func extractServers(ctx context.Context, root *yaml.Node, nodes documentTopLevelNodes, doc *Document, idx *index.SpecIndex) error {
	nodeRef := selectDocumentNode(root, nodes.servers, ServersLabel, false)
	ln, vn := nodeRef.key, nodeRef.value
	if vn != nil {
		if utils.IsNodeArray(vn) {
			var servers []low.ValueReference[*Server]
			for _, srvN := range vn.Content {
				if utils.IsNodeMap(srvN) {
					srvr := Server{}
					_ = low.BuildModel(srvN, &srvr)
					_ = srvr.Build(ctx, ln, srvN, idx)
					servers = append(servers, low.ValueReference[*Server]{
						Value:     &srvr,
						ValueNode: srvN,
					})
				}
			}
			doc.Servers = low.NodeReference[[]low.ValueReference[*Server]]{
				Value:     servers,
				KeyNode:   ln,
				ValueNode: vn,
			}
		}
	}
	return nil
}

func extractTags(ctx context.Context, root *yaml.Node, nodes documentTopLevelNodes, doc *Document, idx *index.SpecIndex) error {
	nodeRef := selectDocumentNode(root, nodes.tags, base.TagsLabel, false)
	ln, vn := nodeRef.key, nodeRef.value
	if vn != nil {
		if utils.IsNodeArray(vn) {
			var tags []low.ValueReference[*base.Tag]
			for _, tagN := range vn.Content {
				if utils.IsNodeMap(tagN) {
					tag := base.Tag{}
					_ = low.BuildModel(tagN, &tag)
					if err := tag.Build(ctx, ln, tagN, idx); err != nil {
						return err
					}
					tags = append(tags, low.ValueReference[*base.Tag]{
						Value:     &tag,
						ValueNode: tagN,
					})
				}
			}
			doc.Tags = low.NodeReference[[]low.ValueReference[*base.Tag]]{
				Value:     tags,
				KeyNode:   ln,
				ValueNode: vn,
			}
		}
	}
	return nil
}

func extractPaths(ctx context.Context, root *yaml.Node, nodes documentTopLevelNodes, doc *Document, idx *index.SpecIndex) error {
	nodeRef := selectDocumentNode(root, nodes.paths, PathsLabel, false)
	ln, vn := nodeRef.key, nodeRef.value
	if vn != nil {
		ir := Paths{}
		err := ir.Build(ctx, ln, vn, idx)
		if err != nil {
			return err
		}
		nr := low.NodeReference[*Paths]{Value: &ir, ValueNode: vn, KeyNode: ln}
		doc.Paths = nr
	}
	return nil
}

func extractWebhooks(ctx context.Context, root *yaml.Node, nodes documentTopLevelNodes, doc *Document, idx *index.SpecIndex) error {
	hooks, hooksL, hooksN, err := low.ExtractMap[*PathItem](ctx, WebhooksLabel, root, idx)
	if err != nil {
		return err
	}
	if hooksN != nil && hooksL != nil {
		doc.Webhooks = low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[*PathItem]]]{Value: hooks, KeyNode: hooksL, ValueNode: hooksN}
		for k, v := range hooks.FromOldest() {
			v.Value.Nodes.Store(k.KeyNode.Line, k.KeyNode)
		}
	}
	return nil
}

func urlWithoutTrailingSlash(u *url.URL) *url.URL {
	if u == nil {
		return nil
	}

	u.Path, _ = strings.CutSuffix(u.Path, "/")

	return u
}
