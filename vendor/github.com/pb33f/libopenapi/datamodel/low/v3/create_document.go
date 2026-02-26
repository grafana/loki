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
)

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
	_, labelNode, versionNode := utils.FindKeyNodeFull(OpenAPILabel, info.RootNode.Content)
	var version low.NodeReference[string]
	if versionNode == nil {
		return nil, errors.New("no openapi version/tag found, cannot create document")
	}
	version = low.NodeReference[string]{Value: versionNode.Value, KeyNode: labelNode, ValueNode: versionNode}
	doc := Document{Version: version}
	doc.Nodes = low.ExtractNodes(nil, info.RootNode.Content[0])
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
	// if base url is provided, add a remote filesystem to the rolodex.
	if idxConfig.BaseURL != nil || config.AllowRemoteReferences {

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

	doc.Extensions = low.ExtractExtensions(info.RootNode.Content[0])
	low.ExtractExtensionNodes(ctx, doc.Extensions, doc.Nodes)

	// if set, extract jsonSchemaDialect (3.1)
	_, dialectLabel, dialectNode := utils.FindKeyNodeFull(JSONSchemaDialectLabel, info.RootNode.Content)
	if dialectNode != nil {
		doc.JsonSchemaDialect = low.NodeReference[string]{
			Value: dialectNode.Value, KeyNode: dialectLabel, ValueNode: dialectNode,
		}
	}

	// if set, extract $self (3.2)
	_, selfLabel, selfNode := utils.FindKeyNodeFull(SelfLabel, info.RootNode.Content)
	if selfNode != nil {
		doc.Self = low.NodeReference[string]{
			Value: selfNode.Value, KeyNode: selfLabel, ValueNode: selfNode,
		}
	}

	runExtraction := func(ctx context.Context, info *datamodel.SpecInfo, doc *Document, idx *index.SpecIndex,
		runFunc func(ctx context.Context, i *datamodel.SpecInfo, d *Document, idx *index.SpecIndex) error,
		ers *[]error,
		wg *sync.WaitGroup,
	) {
		if er := runFunc(ctx, info, doc, idx); er != nil {
			*ers = append(*ers, er)
		}
		wg.Done()
	}
	extractionFuncs := []func(ctx context.Context, i *datamodel.SpecInfo, d *Document, idx *index.SpecIndex) error{
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
	if config.Logger != nil {
		config.Logger.Debug("running extractions")
	}
	now = time.Now()
	for _, f := range extractionFuncs {
		runExtraction(ctx, info, &doc, rolodex.GetRootIndex(), f, &errs, &wg)
	}
	wg.Wait()
	done = time.Duration(time.Since(now).Milliseconds())
	if config.Logger != nil {
		config.Logger.Debug("extractions complete", "time", done)
	}
	return &doc, errors.Join(errs...)
}

func extractInfo(ctx context.Context, info *datamodel.SpecInfo, doc *Document, idx *index.SpecIndex) error {
	_, ln, vn := utils.FindKeyNodeFullTop(base.InfoLabel, info.RootNode.Content[0].Content)
	if vn != nil {
		ir := base.Info{}
		_ = low.BuildModel(vn, &ir)
		_ = ir.Build(ctx, ln, vn, idx)
		nr := low.NodeReference[*base.Info]{Value: &ir, ValueNode: vn, KeyNode: ln}
		doc.Info = nr
	}
	return nil
}

func extractSecurity(ctx context.Context, info *datamodel.SpecInfo, doc *Document, idx *index.SpecIndex) error {
	sec, ln, vn, err := low.ExtractArray[*base.SecurityRequirement](ctx, SecurityLabel, info.RootNode.Content[0], idx)
	if err != nil {
		return err
	}
	if vn != nil && ln != nil {
		doc.Security = low.NodeReference[[]low.ValueReference[*base.SecurityRequirement]]{
			Value:     sec,
			KeyNode:   ln,
			ValueNode: vn,
		}
	}
	return nil
}

func extractExternalDocs(ctx context.Context, info *datamodel.SpecInfo, doc *Document, idx *index.SpecIndex) error {
	extDocs, dErr := low.ExtractObject[*base.ExternalDoc](ctx, base.ExternalDocsLabel, info.RootNode.Content[0], idx)
	if dErr != nil {
		return dErr
	}
	doc.ExternalDocs = extDocs
	return nil
}

func extractComponents(ctx context.Context, info *datamodel.SpecInfo, doc *Document, idx *index.SpecIndex) error {
	_, ln, vn := utils.FindKeyNodeFullTop(ComponentsLabel, info.RootNode.Content[0].Content)
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

func extractServers(ctx context.Context, info *datamodel.SpecInfo, doc *Document, idx *index.SpecIndex) error {
	_, ln, vn := utils.FindKeyNodeFull(ServersLabel, info.RootNode.Content[0].Content)
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

func extractTags(ctx context.Context, info *datamodel.SpecInfo, doc *Document, idx *index.SpecIndex) error {
	_, ln, vn := utils.FindKeyNodeFull(base.TagsLabel, info.RootNode.Content[0].Content)
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

func extractPaths(ctx context.Context, info *datamodel.SpecInfo, doc *Document, idx *index.SpecIndex) error {
	_, ln, vn := utils.FindKeyNodeFull(PathsLabel, info.RootNode.Content[0].Content)
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

func extractWebhooks(ctx context.Context, info *datamodel.SpecInfo, doc *Document, idx *index.SpecIndex) error {
	hooks, hooksL, hooksN, eErr := low.ExtractMap[*PathItem](ctx, WebhooksLabel, info.RootNode, idx)
	if eErr != nil {
		return eErr
	}
	if hooks != nil {
		doc.Webhooks = low.NodeReference[*orderedmap.Map[low.KeyReference[string], low.ValueReference[*PathItem]]]{
			Value:     hooks,
			KeyNode:   hooksL,
			ValueNode: hooksN,
		}
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
