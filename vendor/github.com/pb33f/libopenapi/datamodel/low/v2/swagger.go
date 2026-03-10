// Copyright 2022 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

// Package v2 represents all Swagger / OpenAPI 2 low-level models.
//
// Low-level models are more difficult to navigate than higher-level models, however they are packed with all the
// raw AST and node data required to perform any kind of analysis on the underlying data.
//
// Every property is wrapped in a NodeReference or a KeyReference or a ValueReference.
//
// IMPORTANT: As a general rule, Swagger / OpenAPI 2 should be avoided for new projects.
package v2

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/pb33f/libopenapi/datamodel"
	"github.com/pb33f/libopenapi/datamodel/low"
	"github.com/pb33f/libopenapi/datamodel/low/base"
	"github.com/pb33f/libopenapi/index"
	"github.com/pb33f/libopenapi/orderedmap"
	"go.yaml.in/yaml/v4"
)

// processes a property of a Swagger document asynchronously using bool and error channels for signals.
type documentFunction func(ctx context.Context, root *yaml.Node, doc *Swagger, idx *index.SpecIndex, c chan<- struct{}, e chan<- error)

// Swagger represents a high-level Swagger / OpenAPI 2 document. An instance of Swagger is the root of the specification.
type Swagger struct {
	// Swagger is the version of Swagger / OpenAPI being used, extracted from the 'swagger: 2.x' definition.
	Swagger low.ValueReference[string]

	// Info represents a specification Info definition.
	// Provides metadata about the API. The metadata can be used by the clients if needed.
	// - https://swagger.io/specification/v2/#infoObject
	Info low.NodeReference[*base.Info]

	// Host is The host (name or ip) serving the API. This MUST be the host only and does not include the scheme nor
	// sub-paths. It MAY include a port. If the host is not included, the host serving the documentation is to be used
	// (including the port). The host does not support path templating.
	Host low.NodeReference[string]

	// BasePath is The base path on which the API is served, which is relative to the host. If it is not included,
	// the API is served directly under the host. The value MUST start with a leading slash (/).
	// The basePath does not support path templating.
	BasePath low.NodeReference[string]

	// Schemes represents the transfer protocol of the API. Requirements MUST be from the list: "http", "https", "ws", "wss".
	// If the schemes is not included, the default scheme to be used is the one used to access
	// the Swagger definition itself.
	Schemes low.NodeReference[[]low.ValueReference[string]]

	// Consumes is a list of MIME types the APIs can consume. This is global to all APIs but can be overridden on
	// specific API calls. Value MUST be as described under Mime Types.
	Consumes low.NodeReference[[]low.ValueReference[string]]

	// Produces is a list of MIME types the APIs can produce. This is global to all APIs but can be overridden on
	// specific API calls. Value MUST be as described under Mime Types.
	Produces low.NodeReference[[]low.ValueReference[string]]

	// Paths are the paths and operations for the API. Perhaps the most important part of the specification.
	//  - https://swagger.io/specification/v2/#pathsObject
	Paths low.NodeReference[*Paths]

	// Definitions is an object to hold data types produced and consumed by operations. It's composed of Schema instances
	//  - https://swagger.io/specification/v2/#definitionsObject
	Definitions low.NodeReference[*Definitions]

	// SecurityDefinitions represents security scheme definitions that can be used across the specification.
	//  - https://swagger.io/specification/v2/#securityDefinitionsObject
	SecurityDefinitions low.NodeReference[*SecurityDefinitions]

	// Parameters is an object to hold parameters that can be used across operations.
	// This property does not define global parameters for all operations.
	//  - https://swagger.io/specification/v2/#parametersDefinitionsObject
	Parameters low.NodeReference[*ParameterDefinitions]

	// Responses is an object to hold responses that can be used across operations.
	// This property does not define global responses for all operations.
	//  - https://swagger.io/specification/v2/#responsesDefinitionsObject
	Responses low.NodeReference[*ResponsesDefinitions]

	// Security is a declaration of which security schemes are applied for the API as a whole. The list of values
	// describes alternative security schemes that can be used (that is, there is a logical OR between the security
	// requirements). Individual operations can override this definition.
	//  - https://swagger.io/specification/v2/#securityRequirementObject
	Security low.NodeReference[[]low.ValueReference[*base.SecurityRequirement]]

	// Tags are A list of tags used by the specification with additional metadata.
	// The order of the tags can be used to reflect on their order by the parsing tools. Not all tags that are used
	// by the Operation Object must be declared. The tags that are not declared may be organized randomly or based
	// on the tools' logic. Each tag name in the list MUST be unique.
	//  - https://swagger.io/specification/v2/#tagObject
	Tags low.NodeReference[[]low.ValueReference[*base.Tag]]

	// ExternalDocs is an instance of base.ExternalDoc for.. well, obvious really, innit mate?
	ExternalDocs low.NodeReference[*base.ExternalDoc]

	// Extensions contains all custom extensions defined for the top-level document.
	Extensions *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]]

	// Index is a reference to the index.SpecIndex that was created for the document and used
	// as a guide when building out the Document. Ideal if further processing is required on the model and
	// the original details are required to continue the work.
	//
	// This property is not a part of the OpenAPI schema, this is custom to libopenapi.
	Index *index.SpecIndex

	// SpecInfo is a reference to the datamodel.SpecInfo instance created when the specification was read.
	//
	// This property is not a part of the OpenAPI schema, this is custom to libopenapi.
	SpecInfo *datamodel.SpecInfo

	// Rolodex is a reference to the index.Rolodex instance created when the specification was read.
	// The rolodex is used to look up references from file systems (local or remote)
	Rolodex *index.Rolodex
}

// FindExtension locates an extension from the root of the Swagger document.
func (s *Swagger) FindExtension(ext string) *low.ValueReference[*yaml.Node] {
	return low.FindItemInOrderedMap(ext, s.Extensions)
}

// GetExtensions returns all Swagger/Top level extensions and satisfies the low.HasExtensions interface.
func (s *Swagger) GetExtensions() *orderedmap.Map[low.KeyReference[string], low.ValueReference[*yaml.Node]] {
	return s.Extensions
}

// CreateDocumentFromConfig will create a new Swagger document from the provided SpecInfo and DocumentConfiguration.
func CreateDocumentFromConfig(info *datamodel.SpecInfo,
	configuration *datamodel.DocumentConfiguration,
) (*Swagger, error) {
	return createDocument(info, configuration)
}

func createDocument(info *datamodel.SpecInfo, config *datamodel.DocumentConfiguration) (*Swagger, error) {
	doc := Swagger{Swagger: low.ValueReference[string]{Value: info.Version, ValueNode: info.RootNode}}
	doc.Extensions = low.ExtractExtensions(info.RootNode.Content[0])

	// create an index config and shadow the document configuration.
	idxConfig := index.CreateClosedAPIIndexConfig()
	idxConfig.SpecInfo = info
	idxConfig.IgnoreArrayCircularReferences = config.IgnoreArrayCircularReferences
	idxConfig.IgnorePolymorphicCircularReferences = config.IgnorePolymorphicCircularReferences
	idxConfig.AllowUnknownExtensionContentDetection = config.AllowUnknownExtensionContentDetection
	idxConfig.SkipExternalRefResolution = config.SkipExternalRefResolution
	idxConfig.AvoidCircularReferenceCheck = true
	idxConfig.BaseURL = config.BaseURL
	idxConfig.BasePath = config.BasePath
	idxConfig.Logger = config.Logger
	idxConfig.ExcludeExtensionRefs = config.ExcludeExtensionRefs
	rolodex := index.NewRolodex(idxConfig)
	rolodex.SetRootNode(info.RootNode)
	doc.Rolodex = rolodex

	// If basePath is provided, add a local filesystem to the rolodex.
	if idxConfig.BasePath != "" {
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
	if idxConfig.BaseURL != nil {

		// create a remote filesystem
		remoteFS, _ := index.NewRemoteFSWithConfig(idxConfig)
		if config.RemoteURLHandler != nil {
			remoteFS.RemoteHandlerFunc = config.RemoteURLHandler
		}
		idxConfig.AllowRemoteLookup = true

		// add to the rolodex
		rolodex.AddRemoteFS(config.BaseURL.String(), remoteFS)

	}

	doc.Rolodex = rolodex

	var errs []error

	// index all the things!
	_ = rolodex.IndexTheRolodex(context.Background())

	// check for circular references
	if !config.SkipCircularReferenceCheck {
		rolodex.CheckForCircularReferences()
	}

	// extract errors
	roloErrs := rolodex.GetCaughtErrors()
	if roloErrs != nil {
		errs = append(errs, roloErrs...)
	}

	// set the index on the document.
	doc.Index = rolodex.GetRootIndex()
	doc.SpecInfo = info

	// build out swagger scalar variables.
	_ = low.BuildModel(info.RootNode.Content[0], &doc)

	ctx := context.Background()

	// extract externalDocs
	extDocs, err := low.ExtractObject[*base.ExternalDoc](ctx, base.ExternalDocsLabel, info.RootNode, rolodex.GetRootIndex())
	if err != nil {
		errs = append(errs, err)
	}

	doc.ExternalDocs = extDocs

	extractionFuncs := []documentFunction{
		extractInfo,
		extractPaths,
		extractDefinitions,
		extractParamDefinitions,
		extractResponsesDefinitions,
		extractSecurityDefinitions,
		extractTags,
		extractSecurity,
	}
	doneChan := make(chan struct{})
	errChan := make(chan error)
	for i := range extractionFuncs {
		go extractionFuncs[i](ctx, info.RootNode.Content[0], &doc, rolodex.GetRootIndex(), doneChan, errChan)
	}
	completedExtractions := 0
	for completedExtractions < len(extractionFuncs) {
		select {
		case <-doneChan:
			completedExtractions++
		case e := <-errChan:
			completedExtractions++
			errs = append(errs, e)
		}
	}

	return &doc, errors.Join(errs...)
}

func (s *Swagger) GetExternalDocs() *low.NodeReference[any] {
	return &low.NodeReference[any]{
		KeyNode:   s.ExternalDocs.KeyNode,
		ValueNode: s.ExternalDocs.ValueNode,
		Value:     s.ExternalDocs.Value,
	}
}

func extractInfo(ctx context.Context, root *yaml.Node, doc *Swagger, idx *index.SpecIndex, c chan<- struct{}, e chan<- error) {
	info, err := low.ExtractObject[*base.Info](ctx, base.InfoLabel, root, idx)
	if err != nil {
		e <- err
		return
	}
	doc.Info = info
	c <- struct{}{}
}

func extractPaths(ctx context.Context, root *yaml.Node, doc *Swagger, idx *index.SpecIndex, c chan<- struct{}, e chan<- error) {
	paths, err := low.ExtractObject[*Paths](ctx, PathsLabel, root, idx)
	if err != nil {
		e <- err
		return
	}
	doc.Paths = paths
	c <- struct{}{}
}

func extractDefinitions(ctx context.Context, root *yaml.Node, doc *Swagger, idx *index.SpecIndex, c chan<- struct{}, e chan<- error) {
	def, err := low.ExtractObject[*Definitions](ctx, DefinitionsLabel, root, idx)
	if err != nil {
		e <- err
		return
	}
	doc.Definitions = def
	c <- struct{}{}
}

func extractParamDefinitions(ctx context.Context, root *yaml.Node, doc *Swagger, idx *index.SpecIndex, c chan<- struct{}, e chan<- error) {
	param, err := low.ExtractObject[*ParameterDefinitions](ctx, ParametersLabel, root, idx)
	if err != nil {
		e <- err
		return
	}
	doc.Parameters = param
	c <- struct{}{}
}

func extractResponsesDefinitions(ctx context.Context, root *yaml.Node, doc *Swagger, idx *index.SpecIndex, c chan<- struct{}, e chan<- error) {
	resp, err := low.ExtractObject[*ResponsesDefinitions](ctx, ResponsesLabel, root, idx)
	if err != nil {
		e <- err
		return
	}
	doc.Responses = resp
	c <- struct{}{}
}

func extractSecurityDefinitions(ctx context.Context, root *yaml.Node, doc *Swagger, idx *index.SpecIndex, c chan<- struct{}, e chan<- error) {
	sec, err := low.ExtractObject[*SecurityDefinitions](ctx, SecurityDefinitionsLabel, root, idx)
	if err != nil {
		e <- err
		return
	}
	doc.SecurityDefinitions = sec
	c <- struct{}{}
}

func extractTags(ctx context.Context, root *yaml.Node, doc *Swagger, idx *index.SpecIndex, c chan<- struct{}, e chan<- error) {
	tags, ln, vn, err := low.ExtractArray[*base.Tag](ctx, base.TagsLabel, root, idx)
	if err != nil {
		e <- err
		return
	}
	doc.Tags = low.NodeReference[[]low.ValueReference[*base.Tag]]{
		Value:     tags,
		KeyNode:   ln,
		ValueNode: vn,
	}
	c <- struct{}{}
}

func extractSecurity(ctx context.Context, root *yaml.Node, doc *Swagger, idx *index.SpecIndex, c chan<- struct{}, e chan<- error) {
	sec, ln, vn, err := low.ExtractArray[*base.SecurityRequirement](ctx, SecurityLabel, root, idx)
	if err != nil {
		e <- err
		return
	}
	doc.Security = low.NodeReference[[]low.ValueReference[*base.SecurityRequirement]]{
		Value:     sec,
		KeyNode:   ln,
		ValueNode: vn,
	}
	c <- struct{}{}
}
