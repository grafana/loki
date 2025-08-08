package dataobj

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/bufpool"
)

var errSectionHandleNotFound = errors.New("section handle not found")

// sectionHandle is a unique identifier for a section of data stored in scratch
// space. sectionHandles are only unique within a single [sectionScratchStore].
type sectionHandle uint64

// sectionInfo represents metadata about a section of data stored in scratch space.
type sectionInfo struct {
	Type SectionType // Type of the section.

	DataSize     int // Size of the section's data region in bytes.
	MetadataSize int // Size of the section's metadata region in bytes.
}

// sectionScratchStore is an abstraction for temporarily storing sections of
// data while building an object.
type sectionScratchStore interface {
	// Put buffers a new section, returning a handle to it. Implementations of
	// Put are not permitted to retain the data or metadata slices beyond the
	// duration of the Put call.
	Put(typ SectionType, data, metadata []byte) sectionHandle

	// Info returns metadata about a buffered section.
	Info(handle sectionHandle) (sectionInfo, error)

	// ReadData returns a reader for the section's data region.
	ReadData(handle sectionHandle) (io.ReadSeekCloser, error)

	// ReadMetadata returns a reader for the section's metadata region.
	ReadMetadata(handle sectionHandle) (io.ReadSeekCloser, error)

	// Remove removes a buffered section from the store.
	Remove(handle sectionHandle) error
}

// memoryScratchStore is an implementation of [sectionScratchStore] where buffered
// sections are kept in memory.
type memoryScratchStore struct {
	nextHandle uint64

	infos     map[sectionHandle]sectionInfo
	datas     map[sectionHandle]*bytes.Buffer
	metadatas map[sectionHandle]*bytes.Buffer
}

func newMemoryScratchStore() *memoryScratchStore {
	return &memoryScratchStore{
		infos:     make(map[sectionHandle]sectionInfo),
		datas:     make(map[sectionHandle]*bytes.Buffer),
		metadatas: make(map[sectionHandle]*bytes.Buffer),
	}
}

func (store *memoryScratchStore) Put(typ SectionType, data, metadata []byte) sectionHandle {
	handle := sectionHandle(store.nextHandle)
	store.nextHandle++

	var (
		dataBuf     = bufpool.Get(len(data))
		metadataBuf = bufpool.Get(len(metadata))
	)

	_, _ = dataBuf.Write(data)
	_, _ = metadataBuf.Write(metadata)

	store.infos[handle] = sectionInfo{
		Type: typ,

		DataSize:     len(data),
		MetadataSize: len(metadata),
	}
	store.datas[handle] = dataBuf
	store.metadatas[handle] = metadataBuf
	return handle
}

func (store *memoryScratchStore) Info(handle sectionHandle) (sectionInfo, error) {
	info, ok := store.infos[handle]
	if !ok {
		return sectionInfo{}, errSectionHandleNotFound
	}
	return info, nil
}

func (store *memoryScratchStore) ReadData(handle sectionHandle) (io.ReadSeekCloser, error) {
	dataBuf, ok := store.datas[handle]
	if !ok {
		return nil, errSectionHandleNotFound
	}

	r := bytes.NewReader(dataBuf.Bytes())
	return nopReadSeekerCloser{r}, nil
}

func (store *memoryScratchStore) ReadMetadata(handle sectionHandle) (io.ReadSeekCloser, error) {
	metadataBuf, ok := store.metadatas[handle]
	if !ok {
		return nil, errSectionHandleNotFound
	}

	r := bytes.NewReader(metadataBuf.Bytes())
	return nopReadSeekerCloser{r}, nil
}

func (store *memoryScratchStore) Remove(handle sectionHandle) error {
	dataBuf, ok := store.datas[handle]
	if !ok {
		return errSectionHandleNotFound
	}

	metadataBuf, ok := store.metadatas[handle]
	if !ok {
		return errSectionHandleNotFound
	}

	bufpool.Put(dataBuf)
	bufpool.Put(metadataBuf)

	delete(store.infos, handle)
	delete(store.datas, handle)
	delete(store.metadatas, handle)

	return nil
}

type nopReadSeekerCloser struct{ io.ReadSeeker }

func (nopReadSeekerCloser) Close() error { return nil }

// diskScratchStore is an implementation of [sectionScratchStore] where buffered
// sections are kept on ephemeral disk.
//
// The path given to diskScratchStore should be an epehemeral directory; while
// diskScratchStore will clean up files for sections after they're removed,
// files can still leak if the process is terminated unexpectedly.
//
// diskScratchStore supports a memory fallback mechanism if writing a section
// fails.
type diskScratchStore struct {
	logger log.Logger

	nextHandle uint64

	// fallback is used as a memory fallback if writing a section fails. We
	// synchronize nextHandle when using.
	fallback *memoryScratchStore

	path string // Path on disk to store sections.

	infos         map[sectionHandle]sectionInfo
	dataFiles     map[sectionHandle]string // Data filename per section handle.
	metadataFiles map[sectionHandle]string // Metadata filename per section handle.
}

func newDiskScratchStore(logger log.Logger, path string) (*diskScratchStore, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("failed to stat path %q: %w", path, err)
	}

	return &diskScratchStore{
		logger: logger,

		fallback: newMemoryScratchStore(),

		path: path,

		infos:         make(map[sectionHandle]sectionInfo),
		dataFiles:     make(map[sectionHandle]string),
		metadataFiles: make(map[sectionHandle]string),
	}, nil
}

func (store *diskScratchStore) Put(typ SectionType, data, metadata []byte) sectionHandle {
	handle, err := store.tryPut(typ, data, metadata)
	if err != nil {
		level.Warn(store.logger).Log("msg", "failed to put section on disk; falling back to in-memory", "err", err)
		return store.putFallback(typ, data, metadata)
	}
	return handle
}

func (store *diskScratchStore) tryPut(typ SectionType, data, metadata []byte) (handle sectionHandle, err error) {
	handle = sectionHandle(store.nextHandle)
	store.nextHandle++

	var (
		dataFilename     = generateRegionFilename(data, "data")
		metadataFilename = generateRegionFilename(metadata, "metadata")

		dataFilePath     = filepath.Join(store.path, dataFilename)
		metadataFilePath = filepath.Join(store.path, metadataFilename)
	)

	if err := writeRegionFile(store.logger, dataFilePath, data); err != nil {
		return handle, fmt.Errorf("writing data file %q: %w", dataFilePath, err)
	}

	if err := writeRegionFile(store.logger, metadataFilePath, metadata); err != nil {
		return handle, fmt.Errorf("writing metadata file %q: %w", metadataFilePath, err)
	}

	store.infos[handle] = sectionInfo{
		Type: typ,

		DataSize:     len(data),
		MetadataSize: len(metadata),
	}

	store.dataFiles[handle] = dataFilename
	store.metadataFiles[handle] = metadataFilename
	return handle, nil
}

func generateRegionFilename(bb []byte, ext string) string {
	sum := sha256.Sum224(bb)
	sumString := hex.EncodeToString(sum[:])
	return sumString + "." + ext
}

func writeRegionFile(logger log.Logger, path string, content []byte) error {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0660)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = file.Write(content)
	if err != nil {
		// If we couldn't write the file, we can opportunistically remove it
		// (since it won't be used).
		if err := os.Remove(path); err != nil {
			level.Warn(logger).Log("msg", "failed to remove unused file", "err", err)
		}
	}
	return err
}

func (store *diskScratchStore) putFallback(typ SectionType, data, metadata []byte) sectionHandle {
	// Synchronize next handles between stores to make sure we don't return the
	// same handle twice.
	store.fallback.nextHandle = store.nextHandle
	defer func() { store.nextHandle = store.fallback.nextHandle }()

	return store.fallback.Put(typ, data, metadata)
}

func (store *diskScratchStore) Info(handle sectionHandle) (sectionInfo, error) {
	info, ok := store.infos[handle]
	if !ok {
		return store.fallback.Info(handle)
	}
	return info, nil
}

func (store *diskScratchStore) ReadData(handle sectionHandle) (io.ReadSeekCloser, error) {
	filename, ok := store.dataFiles[handle]
	if !ok {
		return store.fallback.ReadData(handle)
	}
	return os.Open(filepath.Join(store.path, filename))
}

func (store *diskScratchStore) ReadMetadata(handle sectionHandle) (io.ReadSeekCloser, error) {
	filename, ok := store.metadataFiles[handle]
	if !ok {
		return store.fallback.ReadMetadata(handle)
	}
	return os.Open(filepath.Join(store.path, filename))
}

func (store *diskScratchStore) Remove(handle sectionHandle) error {
	if _, ok := store.infos[handle]; !ok {
		return store.fallback.Remove(handle)
	}

	var (
		dataFilename     = store.dataFiles[handle]
		metadataFilename = store.metadataFiles[handle]
	)

	// We'll try to remove the files, but we don't want to fail the entire
	// remove if we can't remove one of them.
	if err := os.Remove(filepath.Join(store.path, dataFilename)); err != nil {
		level.Warn(store.logger).Log("msg", "failed to remove data file", "err", err)
	}
	if err := os.Remove(filepath.Join(store.path, metadataFilename)); err != nil {
		level.Warn(store.logger).Log("msg", "failed to remove metadata file", "err", err)
	}

	// Stop tracking the handle.
	delete(store.infos, handle)
	delete(store.dataFiles, handle)
	delete(store.metadataFiles, handle)

	return nil
}

type observableScratchStore struct {
	metrics *builderMetrics
	inner   sectionScratchStore

	handles map[sectionHandle]sectionInfo
}

func newObservableScratchStore(metrics *builderMetrics, inner sectionScratchStore) *observableScratchStore {
	return &observableScratchStore{
		inner:   inner,
		metrics: metrics,
		handles: make(map[sectionHandle]sectionInfo),
	}
}

func (store *observableScratchStore) Put(typ SectionType, data, metadata []byte) sectionHandle {
	start := time.Now()
	result := store.inner.Put(typ, data, metadata)
	duration := time.Since(start)

	store.handles[result] = sectionInfo{
		Type:         typ,
		DataSize:     len(data),
		MetadataSize: len(metadata),
	}

	store.metrics.scratchStoreSections.Add(1)
	store.metrics.scratchStoreBytes.WithLabelValues("data").Add(float64(len(data)))
	store.metrics.scratchStoreBytes.WithLabelValues("metadata").Add(float64(len(metadata)))
	store.metrics.scratchStoreRequestsSeconds.WithLabelValues("Put", "success").Observe(duration.Seconds())
	return result
}

func (store *observableScratchStore) Info(handle sectionHandle) (sectionInfo, error) {
	start := time.Now()
	info, err := store.inner.Info(handle)
	duration := time.Since(start)

	store.metrics.scratchStoreRequestsSeconds.WithLabelValues("Info", errorToResult(err)).Observe(duration.Seconds())

	return info, err
}

func errorToResult(err error) string {
	if err == nil {
		return "success"
	}
	return "failure"
}

func (store *observableScratchStore) ReadData(handle sectionHandle) (io.ReadSeekCloser, error) {
	start := time.Now()
	reader, err := store.inner.ReadData(handle)
	duration := time.Since(start)

	store.metrics.scratchStoreRequestsSeconds.WithLabelValues("ReadData", errorToResult(err)).Observe(duration.Seconds())

	return reader, err
}

func (store *observableScratchStore) ReadMetadata(handle sectionHandle) (io.ReadSeekCloser, error) {
	start := time.Now()
	reader, err := store.inner.ReadMetadata(handle)
	duration := time.Since(start)

	store.metrics.scratchStoreRequestsSeconds.WithLabelValues("ReadMetadata", errorToResult(err)).Observe(duration.Seconds())

	return reader, err
}

func (store *observableScratchStore) Remove(handle sectionHandle) error {
	start := time.Now()
	err := store.inner.Remove(handle)
	duration := time.Since(start)

	if info, ok := store.handles[handle]; ok {
		store.metrics.scratchStoreSections.Sub(1)
		store.metrics.scratchStoreBytes.WithLabelValues("data").Sub(float64(info.DataSize))
		store.metrics.scratchStoreBytes.WithLabelValues("metadata").Sub(float64(info.MetadataSize))

		delete(store.handles, handle)
	}

	store.metrics.scratchStoreRequestsSeconds.WithLabelValues("Remove", errorToResult(err)).Observe(duration.Seconds())

	return err
}
