// Copyright 2023 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package index

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pb33f/libopenapi/datamodel"
	"github.com/pb33f/libopenapi/utils"

	"go.yaml.in/yaml/v4"
)

const (
	YAML FileExtension = iota
	JSON
	JS
	GO
	TS
	CS
	C
	CPP
	PHP
	PY
	HTML
	MD
	JAVA
	RS
	ZIG
	RB
	UNSUPPORTED
)

// FileExtension is the type of file extension.
type FileExtension int

// contentDetectionCache is a simple cache for content type detection results
// to avoid repeated fetches of the same URL
var contentDetectionCache = make(map[string]FileExtension)
var contentDetectionMutex sync.RWMutex

// detectContentType attempts to identify if the data contains JSON or YAML content
// by analyzing patterns in the first ~1KB of data
func detectContentType(data []byte) FileExtension {
	if len(data) == 0 {
		return UNSUPPORTED
	}

	// Trim leading whitespace
	data = bytes.TrimLeft(data, " \t\r\n")
	if len(data) == 0 {
		return UNSUPPORTED
	}

	// Check for JSON patterns
	if data[0] == '{' || data[0] == '[' {
		// Quick validation - count braces/brackets to ensure it's not malformed
		if data[0] == '{' {
			openBraces := 0
			for _, b := range data {
				if b == '{' {
					openBraces++
				} else if b == '}' {
					openBraces--
				}
			}
			if openBraces >= 0 {
				return JSON
			}
		} else if data[0] == '[' {
			openBrackets := 0
			for _, b := range data {
				if b == '[' {
					openBrackets++
				} else if b == ']' {
					openBrackets--
				}
			}
			if openBrackets >= 0 {
				return JSON
			}
		}
	}

	// Check for YAML patterns
	dataStr := string(data)
	// YAML document markers
	if strings.HasPrefix(dataStr, "---") {
		return YAML
	}

	// Look for key-value patterns common in YAML
	lines := strings.Split(dataStr, "\n")
	yamlPatterns := 0
	for i, line := range lines {
		if i > 10 {
			break // Only check first few lines for efficiency
		}
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue // Skip empty lines and comments
		}
		// Look for key: value patterns
		if strings.Contains(line, ":") && !strings.HasPrefix(line, "http") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				key := strings.TrimSpace(parts[0])
				// Ensure it looks like a YAML key (not a URL)
				if key != "" && !strings.Contains(key, " ") && !strings.Contains(key, "/") {
					yamlPatterns++
				}
			}
		}
	}

	// If we found multiple YAML-like patterns, it's probably YAML
	if yamlPatterns >= 2 {
		return YAML
	}

	return UNSUPPORTED
}

// fetchWithRetry fetches content from URL with retry logic
func fetchWithRetry(url string, handler utils.RemoteURLHandler, maxSize int, logger *slog.Logger) ([]byte, error) {
	const maxRetries = 3
	const retryDelay = time.Second

	var lastErr error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		if logger != nil {
			logger.Debug("fetching content for type detection", "url", url, "attempt", attempt)
		}

		resp, err := handler(url)
		if err != nil {
			lastErr = err
			if attempt < maxRetries {
				time.Sleep(retryDelay)
				continue
			}
			break
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
			if attempt < maxRetries {
				time.Sleep(retryDelay)
				continue
			}
			break
		}

		// Create a limited reader to avoid reading huge files
		limitedReader := io.LimitReader(resp.Body, int64(maxSize))
		data, err := io.ReadAll(limitedReader)
		if err != nil {
			lastErr = err
			if attempt < maxRetries {
				time.Sleep(retryDelay)
				continue
			}
			break
		}

		return data, nil
	}

	return nil, fmt.Errorf("failed to fetch after %d attempts: %v", maxRetries, lastErr)
}

// detectRemoteContentType fetches a small portion of remote content to determine its type
func detectRemoteContentType(url string, handler utils.RemoteURLHandler, logger *slog.Logger) FileExtension {
	// Check cache first
	contentDetectionMutex.RLock()
	if cached, exists := contentDetectionCache[url]; exists {
		contentDetectionMutex.RUnlock()
		return cached
	}
	contentDetectionMutex.RUnlock()

	// Fetch content with retry logic
	const maxDetectionSize = 2048 // 2KB should be enough for detection
	data, err := fetchWithRetry(url, handler, maxDetectionSize, logger)
	if err != nil {
		if logger != nil {
			logger.Warn("failed to fetch content for type detection", "url", url, "error", err)
		}
		// Cache the failure to avoid repeated attempts
		contentDetectionMutex.Lock()
		contentDetectionCache[url] = UNSUPPORTED
		contentDetectionMutex.Unlock()
		return UNSUPPORTED
	}

	// Detect content type
	detectedType := detectContentType(data)

	// Cache the result
	contentDetectionMutex.Lock()
	contentDetectionCache[url] = detectedType
	contentDetectionMutex.Unlock()

	if logger != nil {
		typeStr := "UNSUPPORTED"
		if detectedType == JSON {
			typeStr = "JSON"
		} else if detectedType == YAML {
			typeStr = "YAML"
		}
		logger.Debug("detected content type", "url", url, "type", typeStr)
	}

	return detectedType
}

// clearContentDetectionCache clears the content detection cache
func clearContentDetectionCache() {
	contentDetectionMutex.Lock()
	contentDetectionCache = make(map[string]FileExtension)
	contentDetectionMutex.Unlock()
}

// RolodexFSWithContext is an interface like fs.FS, but with a context parameter for the Open method.
type RolodexFSWithContext interface {
	OpenWithContext(ctx context.Context, name string) (fs.File, error)
}

// RemoteFS is a file system that indexes remote files. It implements the fs.FS interface. Files are located remotely
// and served via HTTP.
type RemoteFS struct {
	indexConfig       *SpecIndexConfig
	rootURL           string
	rootURLParsed     *url.URL
	RemoteHandlerFunc utils.RemoteURLHandler
	Files             sync.Map
	ProcessingFiles   sync.Map
	FetchTime         int64
	FetchChannel      chan *RemoteFile
	remoteErrors      []error
	logger            *slog.Logger
	extractedFiles    map[string]RolodexFile
	rolodex           *Rolodex
	errMutex          sync.Mutex
}

// RemoteFile is a file that has been indexed by the RemoteFS. It implements the RolodexFile interface.
type RemoteFile struct {
	filename         string
	name             string
	extension        FileExtension
	data             []byte
	fullPath         string
	URL              *url.URL
	lastModified     time.Time
	seekingErrors    []error
	index            atomic.Value // *SpecIndex
	parsed           *yaml.Node
	offset           int64
	indexOnce        sync.Once
	contentLock      sync.Mutex
	indexingComplete chan struct{} // Closed when indexing is complete
}

// GetFileName returns the name of the file.
func (f *RemoteFile) GetFileName() string {
	return f.filename
}

// GetContent returns the content of the file as a string.
func (f *RemoteFile) GetContent() string {
	return string(f.data)
}

// GetContentAsYAMLNode returns the content of the file as a yaml.Node.
func (f *RemoteFile) GetContentAsYAMLNode() (*yaml.Node, error) {
	f.contentLock.Lock()
	idx := f.GetIndex()
	if idx != nil && idx.root != nil {
		f.contentLock.Unlock()
		return idx.GetRootNode(), nil
	}
	if f.data == nil {
		f.contentLock.Unlock()
		return nil, fmt.Errorf("no data to parse for file: %s", f.fullPath)
	}
	var root yaml.Node
	err := yaml.Unmarshal(f.data, &root)

	if err != nil {
		f.contentLock.Unlock()
		return nil, err
	}
	if idx != nil && idx.root == nil {
		idx.root = &root
	}
	if f.parsed == nil {
		f.parsed = &root
	}
	f.contentLock.Unlock()
	return &root, nil
}

// GetFileExtension returns the file extension of the file.
func (f *RemoteFile) GetFileExtension() FileExtension {
	return f.extension
}

// GetLastModified returns the last modified time of the file.
func (f *RemoteFile) GetLastModified() time.Time {
	return f.lastModified
}

// GetErrors returns any errors that occurred while reading the file.
func (f *RemoteFile) GetErrors() []error {
	return f.seekingErrors
}

// GetFullPath returns the full path of the file.
func (f *RemoteFile) GetFullPath() string {
	return f.fullPath
}

// fs.FileInfo interfaces

// Name returns the name of the file.
func (f *RemoteFile) Name() string {
	return f.name
}

// Size returns the size of the file.
func (f *RemoteFile) Size() int64 {
	return int64(len(f.data))
}

// Mode returns the file mode bits for the file.
func (f *RemoteFile) Mode() fs.FileMode {
	return fs.FileMode(0)
}

// ModTime returns the modification time of the file.
func (f *RemoteFile) ModTime() time.Time {
	return f.lastModified
}

// IsDir returns true if the file is a directory.
func (f *RemoteFile) IsDir() bool {
	return false
}

// fs.File interfaces

// Sys returns the underlying data source (always returns nil)
func (f *RemoteFile) Sys() interface{} {
	return nil
}

// Close closes the file (doesn't do anything, returns no error)
func (f *RemoteFile) Close() error {
	return nil
}

// Stat returns the FileInfo for the file.
func (f *RemoteFile) Stat() (fs.FileInfo, error) {
	return f, nil
}

// Read reads the file. Makes it compatible with io.Reader.
func (f *RemoteFile) Read(b []byte) (int, error) {
	if f.offset >= int64(len(f.data)) {
		return 0, io.EOF
	}
	if f.offset < 0 {
		return 0, &fs.PathError{Op: "read", Path: f.name, Err: fs.ErrInvalid}
	}
	n := copy(b, f.data[f.offset:])
	f.offset += int64(n)
	return n, nil
}

// Index indexes the file and returns a *SpecIndex, any errors are returned as well.
func (f *RemoteFile) Index(ctx context.Context, config *SpecIndexConfig) (*SpecIndex, error) {
	var result *SpecIndex
	var resultErr error
	f.indexOnce.Do(func() {

		content := f.data
		// first, we must parse the content of the file,
		// the check is bypassed, so as long as it's readable, we're good.
		info, _ := datamodel.ExtractSpecInfoWithDocumentCheck(content, true)
		if config.SpecInfo == nil {
			config.SpecInfo = info
		}
		index := NewSpecIndexWithConfigAndContext(ctx, info.RootNode, config)
		index.specAbsolutePath = config.SpecAbsolutePath

		if info.RootNode == nil && index.root == nil {
			resultErr = fmt.Errorf("nothing was extracted from the file '%s'", f.fullPath)
		} else {
			result = index
			f.index.Store(index)
		}
	})
	if v := f.index.Load(); v != nil {
		result = v.(*SpecIndex)
	}
	return result, resultErr
}

// GetIndex returns the index for the file.
func (f *RemoteFile) GetIndex() *SpecIndex {
	if v := f.index.Load(); v != nil {
		return v.(*SpecIndex)
	}
	return nil
}

// WaitForIndexing blocks until the file's index is ready.
// This is used to coordinate between concurrent goroutines when one is loading
// a file and another needs to use its index.
func (f *RemoteFile) WaitForIndexing() {
	if f.indexingComplete != nil {
		<-f.indexingComplete
	}
}

// signalIndexingComplete marks the file as ready for use.
// This should be called after indexing completes.
func (f *RemoteFile) signalIndexingComplete() {
	if f.indexingComplete != nil {
		select {
		case <-f.indexingComplete:
			// Already closed, do nothing
		default:
			close(f.indexingComplete)
		}
	}
}

// NewRemoteFSWithConfig creates a new RemoteFS using the supplied SpecIndexConfig.
func NewRemoteFSWithConfig(specIndexConfig *SpecIndexConfig) (*RemoteFS, error) {
	if specIndexConfig == nil {
		return nil, errors.New("no spec index config provided")
	}
	remoteRootURL := specIndexConfig.BaseURL
	log := specIndexConfig.Logger
	if log == nil {
		log = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelError,
		}))
	}

	rfs := &RemoteFS{
		indexConfig:   specIndexConfig,
		logger:        log,
		rootURLParsed: remoteRootURL,
		FetchChannel:  make(chan *RemoteFile),
	}
	if remoteRootURL != nil {
		rfs.rootURL = remoteRootURL.String()
	}
	if specIndexConfig.RemoteURLHandler != nil {
		rfs.RemoteHandlerFunc = specIndexConfig.RemoteURLHandler
	} else {
		// default http client
		client := &http.Client{
			Timeout: time.Second * 120,
		}
		rfs.RemoteHandlerFunc = func(url string) (*http.Response, error) {
			return client.Get(url)
		}
	}
	return rfs, nil
}

// NewRemoteFSWithRootURL creates a new RemoteFS using the supplied root URL.
func NewRemoteFSWithRootURL(rootURL string) (*RemoteFS, error) {
	remoteRootURL, err := url.Parse(rootURL)
	if err != nil {
		return nil, err
	}
	config := CreateOpenAPIIndexConfig()
	config.BaseURL = remoteRootURL
	return NewRemoteFSWithConfig(config)
}

// SetRemoteHandlerFunc sets the remote handler function.
func (i *RemoteFS) SetRemoteHandlerFunc(handlerFunc utils.RemoteURLHandler) {
	i.RemoteHandlerFunc = handlerFunc
}

// SetIndexConfig sets the index configuration.
func (i *RemoteFS) SetIndexConfig(config *SpecIndexConfig) {
	i.indexConfig = config
}

// GetFiles returns the files that have been indexed.
func (i *RemoteFS) GetFiles() map[string]RolodexFile {
	files := make(map[string]RolodexFile)
	i.Files.Range(func(key, value interface{}) bool {
		files[key.(string)] = value.(*RemoteFile)
		return true
	})
	i.extractedFiles = files
	return files
}

// GetErrors returns any errors that occurred during the indexing process.
func (i *RemoteFS) GetErrors() []error {
	return i.remoteErrors
}

type waiterRemote struct {
	f         string
	done      bool
	file      *RemoteFile
	listeners int
	error     error
	mu        sync.Mutex
}

func (i *RemoteFS) OpenWithContext(ctx context.Context, remoteURL string) (fs.File, error) {
	if i.indexConfig != nil && !i.indexConfig.AllowRemoteLookup {
		return nil, fmt.Errorf("remote lookup for '%s' is not allowed, please set "+
			"AllowRemoteLookup to true as part of the index configuration", remoteURL)
	}

	if !strings.HasPrefix(remoteURL, "http") {
		if i.logger != nil {
			i.logger.Debug("[rolodex remote loader] not a remote file, ignoring", "file", remoteURL)
		}
		return nil, fmt.Errorf("not a remote file: %s", remoteURL)
	}

	remoteParsedURL, err := url.Parse(remoteURL)
	if err != nil {
		return nil, err
	}
	remoteParsedURLOriginal, _ := url.Parse(remoteURL)

	// try path first
	if r, ok := i.Files.Load(remoteParsedURL.Path); ok {
		return r.(*RemoteFile), nil
	}

	fileExt := ExtractFileType(remoteParsedURL.Path)

	// Handle unsupported file extensions with content detection if enabled
	if fileExt == UNSUPPORTED {
		if i.indexConfig != nil && i.indexConfig.AllowUnknownExtensionContentDetection {
			if i.logger != nil {
				i.logger.Debug("[rolodex remote loader] attempting content detection for unknown file extension", "url", remoteParsedURL.String())
			}
			// Attempt to detect content type
			fileExt = detectRemoteContentType(remoteParsedURL.String(), i.RemoteHandlerFunc, i.logger)
			if fileExt == UNSUPPORTED {
				// Clear cache entry on completion to keep memory usage low
				defer func() {
					contentDetectionMutex.Lock()
					delete(contentDetectionCache, remoteParsedURL.String())
					contentDetectionMutex.Unlock()
				}()

				i.remoteErrors = append(i.remoteErrors, fs.ErrInvalid)
				if i.logger != nil {
					i.logger.Warn("[rolodex remote loader] content detection failed, unsupported content type", "url", remoteParsedURL.String())
				}
				return nil, &fs.PathError{Op: "open", Path: remoteURL, Err: fs.ErrInvalid}
			}
			if i.logger != nil {
				typeStr := "UNSUPPORTED"
				if fileExt == JSON {
					typeStr = "JSON"
				} else if fileExt == YAML {
					typeStr = "YAML"
				}
				i.logger.Debug("[rolodex remote loader] content detection successful", "url", remoteParsedURL.String(), "detectedType", typeStr)
			}
		} else {
			// Content detection disabled, treat as unsupported
			i.remoteErrors = append(i.remoteErrors, fs.ErrInvalid)
			if i.logger != nil {
				i.logger.Warn("[rolodex remote loader] unknown file extension and content detection disabled", "file", remoteURL, "remoteURL", remoteParsedURL.String())
			}
			return nil, &fs.PathError{Op: "open", Path: remoteURL, Err: fs.ErrInvalid}
		}
	}

	// Use LoadOrStore to atomically check if someone is already processing this file.
	// This prevents the race condition where two goroutines both see "not processing"
	// and both start processing the same file.
	processingWaiter := &waiterRemote{f: remoteParsedURL.Path}
	processingWaiter.mu.Lock()

	if existing, loaded := i.ProcessingFiles.LoadOrStore(remoteParsedURL.Path, processingWaiter); loaded {
		// Someone else is already processing this file, wait for them
		processingWaiter.mu.Unlock() // Release our unused waiter's lock
		wait := existing.(*waiterRemote)

		wait.mu.Lock()
		i.logger.Debug("[rolodex remote loader] waiting for existing fetch to complete", "file", remoteURL,
			"remoteURL", remoteParsedURL.String())
		f := wait.file
		e := wait.error
		i.logger.Debug("[rolodex remote loader]: waiting done, remote completed, returning file", "file",
			remoteParsedURL.String(), "listeners", wait.listeners)
		wait.mu.Unlock()
		return f, e
	}

	// We successfully stored our waiter, so we're responsible for processing this file

	// if the remote URL is absolute (http:// or https://), and we have a rootURL defined, we need to override
	// the host being defined by this URL, and use the rootURL instead, but keep the path.
	if i.rootURLParsed != nil {
		remoteParsedURL.Host = i.rootURLParsed.Host
		remoteParsedURL.Scheme = i.rootURLParsed.Scheme
		// this has been disabled, because I don't think it has value, it causes more problems than it solves currently.
		// if !strings.HasPrefix(remoteParsedURL.Path, "/") {
		//	remoteParsedURL.Path = filepath.Join(i.rootURLParsed.Path, remoteParsedURL.Path)
		//	remoteParsedURL.Path = strings.ReplaceAll(remoteParsedURL.Path, "\\", "/")
		// }
	}

	if remoteParsedURL.Scheme == "" {

		processingWaiter.done = true
		i.ProcessingFiles.Delete(remoteParsedURL.Path)
		processingWaiter.mu.Unlock()
		return nil, nil // not a remote file, nothing wrong with that - just we can't keep looking here partner.
	}

	i.logger.Debug("[rolodex remote loader] loading remote file", "file", remoteURL, "remoteURL", remoteParsedURL.String())

	response, clientErr := i.RemoteHandlerFunc(remoteParsedURL.String())
	if clientErr != nil {

		i.errMutex.Lock()
		i.remoteErrors = append(i.remoteErrors, clientErr)
		i.errMutex.Unlock()

		// remove from processing
		processingWaiter.done = true
		i.ProcessingFiles.Delete(remoteParsedURL.Path)
		processingWaiter.mu.Unlock()

		if response != nil {
			i.logger.Error("client error", "error", clientErr, "status", response.StatusCode)
		} else {
			i.logger.Error("client error", "error", clientErr.Error())
		}
		return nil, clientErr
	}
	if response == nil {
		// remove from processing
		processingWaiter.done = true
		i.ProcessingFiles.Delete(remoteParsedURL.Path)
		processingWaiter.mu.Unlock()
		return nil, fmt.Errorf("empty response from remote URL: %s", remoteParsedURL.String())
	}
	responseBytes, readError := io.ReadAll(response.Body)
	if readError != nil {

		// remove from processing
		processingWaiter.error = readError
		processingWaiter.done = true
		i.ProcessingFiles.Delete(remoteParsedURL.Path)
		processingWaiter.mu.Unlock()
		return nil, fmt.Errorf("error reading bytes from remote file '%s': [%s]",
			remoteParsedURL.String(), readError.Error())
	}

	if response.StatusCode >= 400 {

		// remove from processing
		processingWaiter.error = fmt.Errorf("remote file '%s' returned status code %d", remoteParsedURL.String(), response.StatusCode)
		processingWaiter.done = true
		i.ProcessingFiles.Delete(remoteParsedURL.Path)
		i.logger.Error("unable to fetch remote document",
			"file", remoteParsedURL.Path, "status", response.StatusCode, "resp", string(responseBytes))
		processingWaiter.mu.Unlock()
		return nil, fmt.Errorf("unable to fetch remote document '%s' (error %d)", remoteParsedURL.String(),
			response.StatusCode)
	}

	absolutePath := remoteParsedURL.Path

	// extract last modified from response
	lastModified := response.Header.Get("Last-Modified")

	// parse the last modified date into a time object
	lastModifiedTime, parseErr := time.Parse(time.RFC1123, lastModified)

	if parseErr != nil {
		// can't extract last modified, so use now
		lastModifiedTime = time.Now()
	}

	filename := filepath.Base(remoteParsedURL.Path)

	remoteFile := &RemoteFile{
		filename:         filename,
		name:             remoteParsedURL.Path,
		extension:        fileExt,
		data:             responseBytes,
		fullPath:         remoteParsedURL.String(),
		URL:              remoteParsedURL,
		lastModified:     lastModifiedTime,
		indexingComplete: make(chan struct{}),
	}

	copiedCfg := *i.indexConfig

	newBase := fmt.Sprintf("%s://%s%s", remoteParsedURLOriginal.Scheme, remoteParsedURLOriginal.Host,
		filepath.Dir(remoteParsedURL.Path))
	newBaseURL, _ := url.Parse(newBase)

	if newBaseURL != nil {
		copiedCfg.BaseURL = newBaseURL
	}
	copiedCfg.SpecAbsolutePath = remoteParsedURL.String()
	// Force sequential extraction for child indexes to avoid cascading async issues.
	// When the main index uses async mode, spawning async operations for each remote
	// file creates complex timing dependencies that can result in missing references.
	copiedCfg.ExtractRefsSequentially = true

	if len(remoteFile.data) > 0 {
		i.logger.Debug("[rolodex remote loaded] successfully loaded file", "file", absolutePath)
	}

	// Store in Files and release the waiter BEFORE indexing to prevent deadlocks.
	// If file A needs file B and file B needs file A, holding the lock during indexing
	// would cause a deadlock. The indexOnce in Index handles concurrent access safely.
	i.Files.Store(absolutePath, remoteFile)

	// remove from processing
	processingWaiter.file = remoteFile
	processingWaiter.done = true
	i.ProcessingFiles.Delete(remoteParsedURL.Path)
	processingWaiter.mu.Unlock()

	// Add this file to the context's indexing set to prevent deadlocks
	// when circular references cause the same file to be looked up recursively.
	// We add multiple forms of the URL to handle different lookup patterns:
	// - absolutePath: the path portion (e.g., /second.yaml)
	// - remoteParsedURL.String(): the normalized URL (after host override)
	// - remoteParsedURLOriginal.String(): the ORIGINAL URL before host normalization
	// The original URL is critical because lookupRolodex uses the original reference URL
	// when checking IsFileBeingIndexed, not the normalized one.
	indexingCtx := AddIndexingFile(ctx, absolutePath)
	indexingCtx = AddIndexingFile(indexingCtx, remoteParsedURL.String())
	indexingCtx = AddIndexingFile(indexingCtx, remoteParsedURLOriginal.String())

	// Now index the file AFTER releasing the lock
	idx, idxError := remoteFile.Index(indexingCtx, &copiedCfg)

	if idxError != nil && idx == nil {
		i.errMutex.Lock()
		i.remoteErrors = append(i.remoteErrors, idxError)
		i.errMutex.Unlock()
	} else {

		// for each index, we need a resolver
		resolver := NewResolver(idx)
		idx.resolver = resolver
		idx.BuildIndex()
		if i.rolodex != nil {
			i.rolodex.AddExternalIndex(idx, remoteParsedURL.String())
		}
	}

	// Signal that indexing is complete - other goroutines waiting for this file can proceed
	remoteFile.signalIndexingComplete()

	return remoteFile, errors.Join(i.remoteErrors...)
}

// Open opens a file, returning it or an error. If the file is not found, the error is of type *PathError.
func (i *RemoteFS) Open(remoteURL string) (fs.File, error) {
	return i.OpenWithContext(context.Background(), remoteURL)
}
