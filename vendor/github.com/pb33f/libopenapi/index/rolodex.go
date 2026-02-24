// Copyright 2023 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package index

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"maps"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pb33f/libopenapi/utils"

	"context"

	"go.yaml.in/yaml/v4"
)

// CanBeIndexed is an interface that allows a file to be indexed.
type CanBeIndexed interface {
	Index(config *SpecIndexConfig) (*SpecIndex, error)
}

// RolodexFile is an interface that represents a file in the rolodex. It combines multiple `fs` interfaces
// like `fs.FileInfo` and `fs.File` into one interface, so the same struct can be used for everything.
type RolodexFile interface {
	GetContent() string
	GetFileExtension() FileExtension
	GetFullPath() string
	GetErrors() []error
	GetContentAsYAMLNode() (*yaml.Node, error)
	GetIndex() *SpecIndex
	// WaitForIndexing blocks until the file's index is ready.
	// This is used to coordinate between concurrent goroutines when one is loading
	// a file and another needs to use its index.
	WaitForIndexing()
	Name() string
	ModTime() time.Time
	IsDir() bool
	Sys() any
	Size() int64
	Mode() os.FileMode
}

// RolodexFS is an interface that represents a RolodexFS, is the same interface as `fs.FS`, except it
// also exposes a GetFiles() signature, to extract all files in the FS.
type RolodexFS interface {
	Open(name string) (fs.File, error)
	GetFiles() map[string]RolodexFile
}

// Rolodex is a file system abstraction that allows for the indexing of multiple file systems
// and the ability to resolve references across those file systems. It is used to hold references to external
// files, and the indexes they hold. The rolodex is the master lookup for all references.
type Rolodex struct {
	localFS                    map[string]fs.FS
	remoteFS                   map[string]fs.FS
	indexed                    bool
	built                      bool
	manualBuilt                bool
	resolved                   bool
	circChecked                bool
	indexConfig                *SpecIndexConfig
	indexingDuration           time.Duration
	indexes                    []*SpecIndex
	indexMap                   map[string]*SpecIndex
	indexLock                  sync.Mutex
	rootIndex                  *SpecIndex
	rootNode                   *yaml.Node
	caughtErrors               []error
	safeCircularReferences     []*CircularReferenceResult
	infiniteCircularReferences []*CircularReferenceResult
	ignoredCircularReferences  []*CircularReferenceResult
	logger                     *slog.Logger
	id                         string // unique ID for the rolodex, can be used to identify it in logs or other contexts.
	globalSchemaIdRegistry     map[string]*SchemaIdEntry
	schemaIdRegistryLock       sync.RWMutex
}

// NewRolodex creates a new rolodex with the provided index configuration.
func NewRolodex(indexConfig *SpecIndexConfig) *Rolodex {
	logger := indexConfig.Logger
	if logger == nil {
		logger = slog.New(
			slog.NewJSONHandler(
				os.Stdout, &slog.HandlerOptions{
					Level: slog.LevelError,
				},
			),
		)
	}

	r := &Rolodex{
		indexConfig: indexConfig,
		id:          utils.GenerateAlphanumericString(6),
		localFS:     make(map[string]fs.FS),
		remoteFS:    make(map[string]fs.FS),
		logger:      logger,
		indexMap:    make(map[string]*SpecIndex),
	}
	indexConfig.Rolodex = r
	return r
}

// RotateId generates a new unique ID for the rolodex.
func (r *Rolodex) RotateId() string {
	r.id = utils.GenerateAlphanumericString(6)
	return r.id
}

// GetId returns the unique ID for the rolodex.
func (r *Rolodex) GetId() string {
	return r.id
}

// GetIgnoredCircularReferences returns a list of circular references that were ignored during the indexing process.
// These can be an array or polymorphic references. Will return an empty slice if no ignored circular references are found.
func (r *Rolodex) GetIgnoredCircularReferences() []*CircularReferenceResult {
	debounced := make(map[string]*CircularReferenceResult)
	var debouncedResults []*CircularReferenceResult
	if r == nil {
		return debouncedResults
	}
	for _, c := range r.ignoredCircularReferences {
		if _, ok := debounced[c.LoopPoint.FullDefinition]; !ok {
			debounced[c.LoopPoint.FullDefinition] = c
		}
	}
	for _, v := range debounced {
		debouncedResults = append(debouncedResults, v)
	}
	return debouncedResults
}

// GetSafeCircularReferences returns a list of circular references that were found to be safe during the indexing process.
// These can be an array or polymorphic references. Will return an empty slice if no safe circular references are found.
func (r *Rolodex) GetSafeCircularReferences() []*CircularReferenceResult {
	debounced := make(map[string]*CircularReferenceResult)
	var debouncedResults []*CircularReferenceResult
	if r == nil {
		return debouncedResults
	}

	// if this rolodex has not been manually checked for circular references or resolved,
	// then we need to perform that check now, looking at all indexes and extracting
	// results from the resolvers.
	if !r.circChecked {
		var extracted []*CircularReferenceResult
		for _, idx := range append(r.GetIndexes(), r.GetRootIndex()) {
			if idx != nil {
				res := idx.resolver
				if res != nil {
					extracted = append(extracted, res.GetSafeCircularReferences()...)
				}
			}
		}
		if len(extracted) > 0 {
			r.safeCircularReferences = append(r.safeCircularReferences, extracted...)
		}
	}

	for _, c := range r.safeCircularReferences {
		if _, ok := debounced[c.LoopPoint.FullDefinition]; !ok {
			debounced[c.LoopPoint.FullDefinition] = c
		}
	}
	for _, v := range debounced {
		debouncedResults = append(debouncedResults, v)
	}
	return debouncedResults
}

// SetSafeCircularReferences sets the safe circular references for the rolodex.
func (r *Rolodex) SetSafeCircularReferences(refs []*CircularReferenceResult) {
	r.safeCircularReferences = refs
}

// GetIndexingDuration returns the duration it took to index the rolodex.
func (r *Rolodex) GetIndexingDuration() time.Duration {
	return r.indexingDuration
}

// GetRootIndex returns the root index of the rolodex (the entry point, the main document)
func (r *Rolodex) GetRootIndex() *SpecIndex {
	return r.rootIndex
}

// GetConfig returns the index configuration of the rolodex.
func (r *Rolodex) GetConfig() *SpecIndexConfig {
	return r.indexConfig
}

// GetRootNode returns the root index of the rolodex (the entry point, the main document)
func (r *Rolodex) GetRootNode() *yaml.Node {
	return r.rootNode
}

// GetIndexes returns all the indexes in the rolodex.
func (r *Rolodex) GetIndexes() []*SpecIndex {
	r.indexLock.Lock()
	defer r.indexLock.Unlock()
	return r.indexes
}

// GetCaughtErrors returns all the errors that were caught during the indexing process.
func (r *Rolodex) GetCaughtErrors() []error {
	return r.caughtErrors
}

// AddLocalFS adds a local file system to the rolodex.
func (r *Rolodex) AddLocalFS(baseDir string, fileSystem fs.FS) {
	absBaseDir, _ := filepath.Abs(baseDir)
	if f, ok := fileSystem.(Rolodexable); ok {
		f.SetRolodex(r)
		f.SetLogger(r.logger)
	}
	r.localFS[absBaseDir] = fileSystem
}

// SetRootNode sets the root node of the rolodex (the entry point, the main document)
func (r *Rolodex) SetRootNode(node *yaml.Node) {
	r.rootNode = node
}

// SetRootIndex sets the root index of the rolodex (the entry point, the main document).
func (r *Rolodex) SetRootIndex(rootIndex *SpecIndex) {
	r.rootIndex = rootIndex
}

func (r *Rolodex) AddExternalIndex(idx *SpecIndex, location string) {
	r.indexLock.Lock()
	defer r.indexLock.Unlock()
	for _, ix := range r.indexes {
		if ix.specAbsolutePath == location {
			return // already exists, no need to add again.
		}
	}

	r.indexes = append(r.indexes, idx)
	if r.indexMap[location] == nil {
		r.indexMap[location] = idx
	}

	// Aggregate $id registrations from this index into the global registry
	r.RegisterIdsFromIndex(idx)
}

func (r *Rolodex) AddIndex(idx *SpecIndex) {
	if idx != nil {
		p := idx.specAbsolutePath
		r.AddExternalIndex(idx, p)
	}
}

// AddRemoteFS adds a remote file system to the rolodex.
func (r *Rolodex) AddRemoteFS(baseURL string, fileSystem fs.FS) {
	if f, ok := fileSystem.(*RemoteFS); ok {
		f.rolodex = r
		f.logger = r.logger
	}
	r.remoteFS[baseURL] = fileSystem
}

// IndexTheRolodex indexes the rolodex, building out the indexes for each file, and then building the root index.
func (r *Rolodex) IndexTheRolodex(ctx context.Context) error {
	if r.indexed {
		return nil
	}

	var caughtErrors []error

	var indexBuildQueue []*SpecIndex

	indexRolodexFile := func(
		location string, fs fs.FS,
		doneChan chan struct{},
		errChan chan error,
		indexChan chan *SpecIndex,
	) {
		var wg sync.WaitGroup

		indexFileFunc := func(idxFile CanBeIndexed, fullPath string) {
			defer wg.Done()

			// copy config and set the
			copiedConfig := *r.indexConfig
			copiedConfig.Rolodex = r
			copiedConfig.SpecAbsolutePath = fullPath
			copiedConfig.AvoidBuildIndex = true // we will build out everything in two steps.
			idx, err := idxFile.Index(&copiedConfig)

			if err == nil { // Index() does not throw an error anymore.
				// for each index, we need a resolver
				resolver := NewResolver(idx)

				// check if the config has been set to ignore circular references in arrays and polymorphic schemas
				if copiedConfig.IgnoreArrayCircularReferences {
					resolver.IgnoreArrayCircularReferences()
				}
				if copiedConfig.IgnorePolymorphicCircularReferences {
					resolver.IgnorePolymorphicCircularReferences()
				}
				indexChan <- idx
			}
		}

		if lfs, ok := fs.(RolodexFS); ok {
			wait := false

			for _, f := range lfs.GetFiles() {
				if idxFile, ko := f.(CanBeIndexed); ko {
					wg.Add(1)
					wait = true
					go indexFileFunc(idxFile, f.GetFullPath())
				}
			}
			if wait {
				wg.Wait()
			}
			doneChan <- struct{}{}
			return
		} else {
			errChan <- errors.New("rolodex file system is not a RolodexFS")
			doneChan <- struct{}{}
		}
	}

	indexingCompleted := 0
	totalToIndex := len(r.localFS) + len(r.remoteFS)
	doneChan := make(chan struct{})
	errChan := make(chan error)
	indexChan := make(chan *SpecIndex)

	// run through every file system and index every file, fan out as many goroutines as possible.
	started := time.Now()
	for k, v := range r.localFS {
		go indexRolodexFile(k, v, doneChan, errChan, indexChan)
	}
	for k, v := range r.remoteFS {
		go indexRolodexFile(k, v, doneChan, errChan, indexChan)
	}

	for indexingCompleted < totalToIndex {
		select {
		case <-doneChan:
			indexingCompleted++
		case err := <-errChan:
			indexingCompleted++
			caughtErrors = append(caughtErrors, err)
		case idx := <-indexChan:
			indexBuildQueue = append(indexBuildQueue, idx)
		}
	}

	// now that we have indexed all the files, we can build the index.
	sort.Slice(
		indexBuildQueue, func(i, j int) bool {
			return indexBuildQueue[i].specAbsolutePath < indexBuildQueue[j].specAbsolutePath
		},
	)

	r.indexes = indexBuildQueue

	for _, idx := range indexBuildQueue {
		idx.BuildIndex()
		if r.indexConfig.AvoidCircularReferenceCheck {
			continue
		}
		errs := idx.resolver.CheckForCircularReferences()
		for e := range errs {
			caughtErrors = append(caughtErrors, errs[e])
		}
		if len(idx.resolver.GetIgnoredCircularPolyReferences()) > 0 {
			r.ignoredCircularReferences = append(
				r.ignoredCircularReferences, idx.resolver.GetIgnoredCircularPolyReferences()...,
			)
		}
		if len(idx.resolver.GetIgnoredCircularArrayReferences()) > 0 {
			r.ignoredCircularReferences = append(
				r.ignoredCircularReferences, idx.resolver.GetIgnoredCircularArrayReferences()...,
			)
		}
	}

	// indexed and built every supporting file, we can build the root index (our entry point)
	if r.rootNode != nil {
		r.indexConfig.Rolodex = r

		// if there is a base path but no SpecFilePath, then we need to set the root spec config to point to a theoretical root.yaml
		// which does not exist, but is used to formulate the absolute path to root references correctly.
		if r.indexConfig.BasePath != "" && r.indexConfig.BaseURL == nil {
			basePath := r.indexConfig.BasePath
			if !filepath.IsAbs(basePath) {
				basePath, _ = filepath.Abs(basePath)
			}

			if len(r.localFS) > 0 || len(r.remoteFS) > 0 {
				if r.indexConfig.SpecFilePath != "" {
					// Compute the absolute path to the spec file.
					// - If SpecFilePath is already absolute, use it directly.
					// - If SpecFilePath is relative, it needs careful handling to avoid path doubling.
					//
					// The original code used filepath.Base() which incorrectly stripped directory
					// segments like /myproject/api-spec/ from nested paths.
					//
					// Handle cases:
					// 1. SpecFilePath = "test_data/nested/doc.yaml", BasePath = "/abs/test_data/nested"
					//    -> Should NOT double to /abs/test_data/nested/test_data/nested/doc.yaml
					// 2. SpecFilePath = "subdir/doc.yaml", BasePath = "/abs/test_data"
					//    -> Should produce /abs/test_data/subdir/doc.yaml
					if filepath.IsAbs(r.indexConfig.SpecFilePath) {
						r.indexConfig.SpecAbsolutePath = r.indexConfig.SpecFilePath
					} else {
						specPath := r.indexConfig.SpecFilePath
						// Check if SpecFilePath starts with the relative basePath or its original value
						// This handles cases where SpecFilePath = "test_data/file.yaml" and
						// BasePath was originally "test_data" (now absolute)
						origBasePath := r.indexConfig.BasePath

						// Normalize paths to use OS-specific separators for Windows compatibility
						// On Windows, paths may use / but os.PathSeparator is \, causing mismatches
						normalizedSpecPath := filepath.FromSlash(specPath)
						normalizedOrigBasePath := filepath.FromSlash(origBasePath)

						if strings.HasPrefix(normalizedSpecPath, normalizedOrigBasePath+string(os.PathSeparator)) {
							// SpecFilePath includes the original basePath, make it absolute directly
							r.indexConfig.SpecAbsolutePath, _ = filepath.Abs(normalizedSpecPath)
						} else if strings.HasPrefix(normalizedSpecPath, "..") {
							// SpecFilePath starts with ".." (parent directory), resolve it from cwd
							// Using filepath.Join with basePath would incorrectly double paths
							// e.g., basePath="/Users/foo/bar" + "../bar/file.yaml" would give
							// "/Users/foo/bar/bar/file.yaml" instead of "/Users/foo/bar/file.yaml"
							r.indexConfig.SpecAbsolutePath, _ = filepath.Abs(normalizedSpecPath)
						} else {
							// SpecFilePath is relative to basePath, join them
							r.indexConfig.SpecAbsolutePath = filepath.Join(basePath, normalizedSpecPath)
						}
					}
				} else {
					r.indexConfig.SetTheoreticalRoot()
				}
			}
		}

		// Here we take the root node and also build the index for it.
		// This involves extracting references.
		index := NewSpecIndexWithConfigAndContext(ctx, r.rootNode, r.indexConfig)
		resolver := NewResolver(index)

		if r.indexConfig.IgnoreArrayCircularReferences {
			resolver.IgnoreArrayCircularReferences()
		}
		if r.indexConfig.IgnorePolymorphicCircularReferences {
			resolver.IgnorePolymorphicCircularReferences()
		}
		r.rootIndex = index
		r.logger.Debug("[rolodex] starting root index build")
		index.BuildIndex()
		r.logger.Debug("[rolodex] root index build completed")

		if !r.indexConfig.AvoidCircularReferenceCheck {
			resolvingErrors := resolver.CheckForCircularReferences()
			r.circChecked = true
			for e := range resolvingErrors {
				caughtErrors = append(caughtErrors, resolvingErrors[e])
			}
			if len(resolver.GetIgnoredCircularPolyReferences()) > 0 {
				r.ignoredCircularReferences = append(
					r.ignoredCircularReferences, resolver.GetIgnoredCircularPolyReferences()...,
				)
			}
			if len(resolver.GetIgnoredCircularArrayReferences()) > 0 {
				r.ignoredCircularReferences = append(
					r.ignoredCircularReferences, resolver.GetIgnoredCircularArrayReferences()...,
				)
			}
		}

		if len(index.refErrors) > 0 {
			caughtErrors = append(caughtErrors, index.refErrors...)
		}
	}
	r.indexingDuration = time.Since(started)
	r.indexed = true
	r.caughtErrors = caughtErrors
	r.built = true
	return errors.Join(caughtErrors...)
}

// CheckForCircularReferences checks for circular references in the rolodex.
func (r *Rolodex) CheckForCircularReferences() {
	if !r.circChecked {
		if r.rootIndex != nil && r.rootIndex.resolver != nil {
			resolvingErrors := r.rootIndex.resolver.CheckForCircularReferences()
			for e := range resolvingErrors {
				r.caughtErrors = append(r.caughtErrors, resolvingErrors[e])
			}
			if len(r.rootIndex.resolver.ignoredPolyReferences) > 0 {
				r.ignoredCircularReferences = append(
					r.ignoredCircularReferences, r.rootIndex.resolver.ignoredPolyReferences...,
				)
			}
			if len(r.rootIndex.resolver.ignoredArrayReferences) > 0 {
				r.ignoredCircularReferences = append(
					r.ignoredCircularReferences, r.rootIndex.resolver.ignoredArrayReferences...,
				)
			}
			r.safeCircularReferences = append(
				r.safeCircularReferences, r.rootIndex.resolver.GetSafeCircularReferences()...,
			)
			r.infiniteCircularReferences = append(
				r.infiniteCircularReferences, r.rootIndex.resolver.GetInfiniteCircularReferences()...,
			)
		}
		r.circChecked = true
	}
}

// Resolve resolves references in the rolodex.
func (r *Rolodex) Resolve() {
	var resolvers []*Resolver
	if r.rootIndex != nil && r.rootIndex.resolver != nil {
		resolvers = append(resolvers, r.rootIndex.resolver)
	}
	for _, idx := range r.indexes {
		if idx.resolver != nil {
			resolvers = append(resolvers, idx.resolver)
		}
	}
	for _, res := range resolvers {
		resolvingErrors := res.Resolve()
		for e := range resolvingErrors {
			r.caughtErrors = append(r.caughtErrors, resolvingErrors[e])
		}
		if r.rootIndex != nil && len(r.rootIndex.resolver.ignoredPolyReferences) > 0 {
			r.ignoredCircularReferences = append(r.ignoredCircularReferences, res.ignoredPolyReferences...)
		}
		if r.rootIndex != nil && len(r.rootIndex.resolver.ignoredArrayReferences) > 0 {
			r.ignoredCircularReferences = append(r.ignoredCircularReferences, res.ignoredArrayReferences...)
		}
		r.safeCircularReferences = append(r.safeCircularReferences, res.GetSafeCircularReferences()...)
		r.infiniteCircularReferences = append(r.infiniteCircularReferences, res.GetInfiniteCircularReferences()...)
	}

	// resolve pending nodes
	for _, res := range resolvers {
		res.ResolvePendingNodes()
	}
	r.resolved = true
}

// BuildIndexes builds the indexes in the rolodex, this is generally not required unless manually building a rolodex.
func (r *Rolodex) BuildIndexes() {
	if r.manualBuilt {
		return
	}
	for _, idx := range r.indexes {
		idx.BuildIndex()
	}
	if r.rootIndex != nil {
		r.rootIndex.BuildIndex()
	}
	r.manualBuilt = true
}

// GetAllReferences  returns all references found in the root and all other indices
func (r *Rolodex) GetAllReferences() map[string]*Reference {
	allRefs := make(map[string]*Reference)
	for _, idx := range append(r.GetIndexes(), r.GetRootIndex()) {
		if idx == nil {
			continue
		}
		refs := idx.GetAllReferences()
		maps.Copy(allRefs, refs)
	}
	return allRefs
}

// GetAllMappedReferences returns all mapped references found in the root and all other indices
func (r *Rolodex) GetAllMappedReferences() map[string]*Reference {
	mappedRefs := make(map[string]*Reference)
	for _, idx := range append(r.GetIndexes(), r.GetRootIndex()) {
		if idx == nil {
			continue
		}
		refs := idx.GetMappedReferences()
		maps.Copy(mappedRefs, refs)
	}
	return mappedRefs
}

// OpenWithContext opens a file in the rolodex, and returns a RolodexFile - providing a context.
// The method supports both custom file systems (like LocalFS) and standard fs.FS implementations.
// For standard fs.FS implementations, paths are automatically converted to relative paths as required
// by the fs.FS interface specification (which mandates relative, slash-separated paths).
func (r *Rolodex) OpenWithContext(ctx context.Context, location string) (RolodexFile, error) {
	if r == nil {
		return nil, fmt.Errorf("rolodex has not been initialized, cannot open file '%s'", location)
	}

	if len(r.localFS) <= 0 && len(r.remoteFS) <= 0 {
		return nil, fmt.Errorf(
			"rolodex has no file systems configured, cannot open '%s'. Add a BaseURL or BasePath to your configuration so the rolodex knows how to resolve references",
			location,
		)
	}

	var errorStack []error
	var localFile *LocalFile
	var remoteFile *RemoteFile
	fileLookup := location
	isUrl := false
	if strings.HasPrefix(location, "http") {
		isUrl = true
	}

	if !isUrl {
		if len(r.localFS) <= 0 {
			r.logger.Warn("[rolodex] no local file systems configured, cannot open local file", "location", location)
			return nil, fmt.Errorf(
				"the rolodex has no local file systems configured, cannot open local file '%s'", location,
			)
		}

		for k, v := range r.localFS {

			// check if this is a URL or an abs/rel reference.
			if !filepath.IsAbs(location) {
				fileLookup, _ = filepath.Abs(utils.CheckPathOverlap(k, location, string(os.PathSeparator)))
			}

			// For generic fs.FS implementations, we need to use relative paths
			// The fs.FS interface requires paths to be relative and slash-separated.
			// This ensures compatibility with standard Go file systems like embed.FS,
			// fstest.MapFS, afero.NewIOFS, and others that strictly follow the fs.FS contract.
			var pathForOpen string

			// Check if this is our custom LocalFS which supports absolute paths
			if _, isLocalFS := v.(*LocalFS); isLocalFS {
				// LocalFS can handle absolute paths directly for backward compatibility
				pathForOpen = fileLookup
			} else {
				// For standard fs.FS implementations, convert to relative path
				// Calculate relative path from base directory k to target fileLookup
				relPath, _ := filepath.Rel(k, fileLookup)
				pathForOpen = filepath.ToSlash(relPath)
			}

			f, err := openFile(ctx, pathForOpen, v)

			if err != nil {
				// If the first attempt failed and we haven't already tried with the original location,
				// try a lookup with the original location path
				if pathForOpen != location {
					f, err = openFile(ctx, location, v)
				}

				if err != nil {
					errorStack = append(errorStack, err)
					continue
				}
			}
			// check if this is a native rolodex FS, then the work is done.
			if lf, ko := interface{}(f).(*LocalFile); ko {
				localFile = lf
				break
			}

			if lf, ko := f.(RolodexFile); ko {
				var atm atomic.Value
				atm.Store(lf.GetIndex())
				var parsed *yaml.Node
				var parseErrors []error
				if p, e := lf.GetContentAsYAMLNode(); e == nil {
					parsed = p
				} else {
					parseErrors = append(parseErrors, e)
				}
				parseErrors = append(parseErrors, lf.GetErrors()...)

				localFile = &LocalFile{
					filename:      lf.Name(),
					name:          lf.Name(),
					extension:     ExtractFileType(lf.Name()),
					data:          []byte(lf.GetContent()),
					fullPath:      lf.GetFullPath(),
					lastModified:  lf.ModTime(),
					index:         atm,
					readingErrors: parseErrors,
					parsed:        parsed,
				}
				// If there were errors processing the file content, we should return them
				if len(parseErrors) > 0 {
					errorStack = append(errorStack, parseErrors...)
				}
				break
			}

			// not a native FS, so we need to read the file and create a local file.
			bytes, rErr := io.ReadAll(f)
			if rErr != nil {
				errorStack = append(errorStack, rErr)
				continue
			}
			s, sErr := f.Stat()
			if sErr != nil {
				errorStack = append(errorStack, sErr)
				continue
			}
			if len(bytes) > 0 {
				var atm atomic.Value
				idx := r.rootIndex
				atm.Store(idx)

				localFile = &LocalFile{
					filename:     filepath.Base(fileLookup),
					name:         filepath.Base(fileLookup),
					extension:    ExtractFileType(fileLookup),
					data:         bytes,
					fullPath:     fileLookup,
					lastModified: s.ModTime(),
					index:        atm,
				}
				break
			}

		}

	} else {

		if !r.indexConfig.AllowRemoteLookup {
			return nil, fmt.Errorf(
				"remote lookup for '%s' not allowed, please set the index configuration to "+
					"AllowRemoteLookup to true", fileLookup,
			)
		}

		for _, v := range r.remoteFS {

			var f fs.File
			var err error
			if fscw, ok := v.(RolodexFSWithContext); ok {
				f, err = fscw.OpenWithContext(ctx, fileLookup)
			} else {
				f, err = v.Open(fileLookup)
			}

			if err != nil {
				r.logger.Warn("[rolodex] errors opening remote file", "location", fileLookup, "error", err)
			}
			if f != nil {
				if rf, ok := interface{}(f).(*RemoteFile); ok {
					remoteFile = rf
					break
				} else {

					bytes, rErr := io.ReadAll(f)
					if rErr != nil {
						errorStack = append(errorStack, rErr)
						continue
					}
					s, sErr := f.Stat()
					if sErr != nil {
						errorStack = append(errorStack, sErr)
						continue
					}
					if len(bytes) > 0 {
						var atm atomic.Value
						atm.Store(r.rootIndex)
						remoteFile = &RemoteFile{
							filename:     filepath.Base(fileLookup),
							name:         filepath.Base(fileLookup),
							extension:    ExtractFileType(fileLookup),
							data:         bytes,
							fullPath:     fileLookup,
							lastModified: s.ModTime(),
							index:        atm,
						}
						break
					}
				}
			}
		}
	}

	if localFile != nil {
		// Check if the localFile has any reading errors that should be returned
		var fileErrors []error
		fileErrors = localFile.readingErrors
		return &rolodexFile{
			rolodex:   r,
			location:  localFile.fullPath,
			localFile: localFile,
		}, errors.Join(fileErrors...)
	}

	if remoteFile != nil {
		// Check if the remoteFile has any seeking errors that should be returned
		var fileErrors []error
		fileErrors = remoteFile.seekingErrors
		return &rolodexFile{
			rolodex:    r,
			location:   remoteFile.fullPath,
			remoteFile: remoteFile,
		}, errors.Join(fileErrors...)
	}

	return nil, errors.Join(errorStack...)
}

func openFile(ctx context.Context, location string, v fs.FS) (fs.File, error) {
	var err error
	var f fs.File
	if fscw, ok := v.(RolodexFSWithContext); ok {
		f, err = fscw.OpenWithContext(ctx, location)
	} else {
		f, err = v.Open(location)
	}
	return f, err
}

// Open opens a file in the rolodex, and returns a RolodexFile.
func (r *Rolodex) Open(location string) (RolodexFile, error) {
	return r.OpenWithContext(context.Background(), location)
}

var suffixes = []string{"B", "KB", "MB", "GB", "TB"}

func Round(val float64, roundOn float64, places int) (newVal float64) {
	var round float64
	pow := math.Pow(10, float64(places))
	digit := pow * val
	_, div := math.Modf(digit)
	if div >= roundOn {
		round = math.Ceil(digit)
	} else {
		round = math.Floor(digit)
	}
	newVal = round / pow
	return
}

func HumanFileSize(size float64) string {
	base := math.Log(size) / math.Log(1024)
	getSize := Round(math.Pow(1024, base-math.Floor(base)), .5, 2)
	getSuffix := suffixes[int(math.Floor(base))]
	return strconv.FormatFloat(getSize, 'f', -1, 64) + " " + string(getSuffix)
}

func (r *Rolodex) RolodexFileSizeAsString() string {
	size := r.RolodexFileSize()
	return HumanFileSize(float64(size))
}

func (r *Rolodex) RolodexTotalFiles() int {
	// look through each file system and count the files
	var total int
	for _, v := range r.localFS {
		if lfs, ok := v.(RolodexFS); ok {
			total += len(lfs.GetFiles())
		}
	}
	for _, v := range r.remoteFS {
		if lfs, ok := v.(RolodexFS); ok {
			total += len(lfs.GetFiles())
		}
	}
	return total
}

func (r *Rolodex) RolodexFileSize() int64 {
	var size int64
	for _, v := range r.localFS {
		if lfs, ok := v.(RolodexFS); ok {
			for _, f := range lfs.GetFiles() {
				size += f.Size()
			}
		}
	}
	for _, v := range r.remoteFS {
		if lfs, ok := v.(RolodexFS); ok {
			for _, f := range lfs.GetFiles() {
				size += f.Size()
			}
		}
	}
	return size
}

// GetFullLineCount returns the total number of lines from all files in the Rolodex
func (r *Rolodex) GetFullLineCount() int64 {
	var lineCount int64
	for _, v := range r.localFS {
		if lfs, ok := v.(RolodexFS); ok {
			for _, f := range lfs.GetFiles() {
				lineCount += int64(strings.Count(f.GetContent(), "\n")) + 1
			}
		}
	}
	for _, v := range r.remoteFS {
		if lfs, ok := v.(RolodexFS); ok {
			for _, f := range lfs.GetFiles() {
				lineCount += int64(strings.Count(f.GetContent(), "\n")) + 1
			}
		}
	}
	// add in root count
	if r.indexConfig != nil && r.indexConfig.SpecInfo != nil {
		lineCount += int64(strings.Count(string(*r.indexConfig.SpecInfo.SpecBytes), "\n")) + 1
	}
	return lineCount
}

func (r *Rolodex) ClearIndexCaches() {
	if r.rootIndex != nil {
		r.rootIndex.GetHighCache().Clear()
	}
	for _, idx := range r.indexes {
		idx.GetHighCache().Clear()
	}
}

// RegisterGlobalSchemaId registers a schema $id in the Rolodex global registry.
// Returns an error if the $id is invalid.
func (r *Rolodex) RegisterGlobalSchemaId(entry *SchemaIdEntry) error {
	if r == nil {
		return fmt.Errorf("cannot register $id on nil Rolodex")
	}

	r.schemaIdRegistryLock.Lock()
	defer r.schemaIdRegistryLock.Unlock()

	if r.globalSchemaIdRegistry == nil {
		r.globalSchemaIdRegistry = make(map[string]*SchemaIdEntry)
	}

	_, err := registerSchemaIdToRegistry(r.globalSchemaIdRegistry, entry, r.logger, "global registry")
	return err
}

// LookupSchemaById looks up a schema by its $id URI across all indexes.
func (r *Rolodex) LookupSchemaById(uri string) *SchemaIdEntry {
	if r == nil {
		return nil
	}

	r.schemaIdRegistryLock.RLock()
	defer r.schemaIdRegistryLock.RUnlock()

	if r.globalSchemaIdRegistry == nil {
		return nil
	}
	return r.globalSchemaIdRegistry[uri]
}

// GetAllGlobalSchemaIds returns a copy of all registered $id entries across all indexes.
func (r *Rolodex) GetAllGlobalSchemaIds() map[string]*SchemaIdEntry {
	if r == nil {
		return make(map[string]*SchemaIdEntry)
	}

	r.schemaIdRegistryLock.RLock()
	defer r.schemaIdRegistryLock.RUnlock()
	return copySchemaIdRegistry(r.globalSchemaIdRegistry)
}

// RegisterIdsFromIndex aggregates all $id registrations from an index into the global registry.
// Called after each index is built to populate the Rolodex global registry.
func (r *Rolodex) RegisterIdsFromIndex(idx *SpecIndex) {
	if r == nil || idx == nil {
		return
	}

	entries := idx.GetAllSchemaIds()
	for _, entry := range entries {
		_ = r.RegisterGlobalSchemaId(entry)
	}
}
