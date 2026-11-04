package index

import (
	"context"

	"github.com/pb33f/libopenapi/datamodel"
)

type ownedRolodexFile struct {
	file   *LocalFile
	err    error
	parsed chan struct{}
}

// ownIndexedFile isolates a cached file from another document's mutable index.
// A pending file exposes its parsed nodes without waiting for indexing, so
// mutually referencing files cannot deadlock during recursive lookups.
func (r *Rolodex) ownIndexedFile(source RolodexFile) (*LocalFile, error) {
	location := source.GetFullPath()
	r.indexLock.Lock()
	owned := r.ownedFiles[location]
	if owned != nil {
		r.indexLock.Unlock()
		<-owned.parsed
		return owned.file, owned.err
	}
	owned = &ownedRolodexFile{parsed: make(chan struct{})}
	if r.ownedFiles == nil {
		r.ownedFiles = make(map[string]*ownedRolodexFile)
	}
	r.ownedFiles[location] = owned
	r.indexLock.Unlock()

	data := []byte(source.GetContent())
	info, err := datamodel.ExtractSpecInfoWithDocumentCheckSync(data, true)
	if err != nil {
		owned.err = err
		close(owned.parsed)
		return nil, err
	}
	file := &LocalFile{
		filename: source.Name(), name: source.Name(), fullPath: location,
		extension: source.GetFileExtension(), lastModified: source.ModTime(),
		data: data, parsed: info.RootNode, indexingComplete: make(chan struct{}),
	}
	owned.file = file
	// Parsing cannot follow references. Release its waiters before indexing,
	// where cross-file lookups must be able to return the pending tree.
	close(owned.parsed)

	file.indexOnce.Do(func() {
		cfg := *r.indexConfig
		cfg.Rolodex = r
		cfg.SpecAbsolutePath = location
		cfg.SpecInfo = info
		cfg.AvoidBuildIndex = true
		ctx := AddIndexingFile(context.Background(), location)
		idx := NewSpecIndexWithConfigAndContext(ctx, info.RootNode, &cfg)
		resolver := NewResolver(idx)
		resolver.IgnoreArray = cfg.IgnoreArrayCircularReferences
		resolver.IgnorePoly = cfg.IgnorePolymorphicCircularReferences
		idx.BuildIndex()
		file.index.Store(idx)
		r.AddIndex(idx)
	})
	file.signalIndexingComplete()
	return file, nil
}
