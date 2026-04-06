package scratch

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
)

// filesystemExt is the extension used for scratch files stored on disk.
const filesystemExt = ".lokiscratch"

// Filesystem is an implementation of [Store] that retains data on the
// filesystem.
//
// Filesystem supports a memory fallback mechanism if writing data fails.
type Filesystem struct {
	logger   log.Logger
	path     string // Path on disk to store data.
	fallback *Memory

	mut        sync.RWMutex
	nextHandle *uint64           // nextHandle is a pointer to allow multiple stores to synchronize handles
	files      map[Handle]string // Relative path to file on disk.
}

var _ Store = (*Filesystem)(nil)

// NewFilesystem returns a new Filesystem store. Stored data is persisted to
// disk with a random filename and an extension ".lokiscratch".
//
// To avoid leaking scratch files between process iterations, NewFilesystem will
// remove any existing ".lokiscratch" files in path upon startup.
//
// NewFilesystem creates the path if it does not exist.
func NewFilesystem(logger log.Logger, path string) (*Filesystem, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("failed to stat scratch path %q: %w", path, err)
	}

	// Use absolute paths for better logging quality.
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path for scratch path %q: %w", path, err)
	}

	return newUncheckedFilesystem(logger, path), nil
}

// newUncheckedFilesystem creates a new Filesystem store without checking if the
// path exists.
func newUncheckedFilesystem(logger log.Logger, path string) *Filesystem {
	if logger == nil {
		logger = log.NewNopLogger()
	}

	if err := cleanScratchDirectory(logger, path); err != nil {
		// Cleaning the scratch directory is best-effort. Log a warning but
		// otherwise continue.
		level.Warn(logger).Log("msg", "failed to clean scratch directory", "err", err)
	}

	nextHandle := uint64(1) // Handles start at 1

	return &Filesystem{
		logger: logger,
		path:   path,

		// Our store and fallback must share the same atomic nextHandle to avoid
		// returning the same handle value twice.
		fallback:   newMemoryWithNextHandle(&nextHandle),
		nextHandle: &nextHandle,

		files: make(map[Handle]string),
	}
}

// cleanScratchDirectory removes any existing ".lokiscratch" files in the given
// directory. It does not traverse into subdirectories.
func cleanScratchDirectory(logger log.Logger, scratchPath string) error {
	return filepath.WalkDir(scratchPath, func(path string, d fs.DirEntry, _ error) error {
		if path == scratchPath {
			// Ignore root.
			return nil
		} else if d == nil {
			// Ignore errors (d == nil only if there's an error); process what
			// we can.
			return nil
		} else if d.IsDir() {
			// Ignore subdirectories.
			return fs.SkipDir
		} else if filepath.Ext(path) != filesystemExt {
			// Ignore non-scratch files.
			return nil
		}

		if err := os.Remove(path); err != nil {
			level.Warn(logger).Log("msg", "failed to remove old scratch file", "path", path, "err", err)
		} else {
			level.Debug(logger).Log("msg", "removed old scratch file", "path", path)
		}

		return nil
	})
}

// Put stores the contents of p into scratch space.
//
// Put will be written with a random filename and the extension ".lokiscratch".
// If writing to the filesystem fails, p will instead be stored in-memory.
func (fs *Filesystem) Put(p []byte) Handle {
	handle, err := fs.tryPut(p)
	if err != nil {
		level.Warn(fs.logger).Log("msg", "failed to store scratch data on filesystem, falling back to in-memory storage", "err", err)
		return fs.fallback.Put(p)
	}
	return handle
}

func (fs *Filesystem) tryPut(p []byte) (Handle, error) {
	f, err := os.CreateTemp(fs.path, "*"+filesystemExt)
	if err != nil {
		return InvalidHandle, err
	}
	defer f.Close()

	if _, err := f.Write(p); err != nil {
		// If we couldn't write the file, we'll opportunistically remove it
		// (since it will never be used).
		if err := os.Remove(f.Name()); err != nil {
			level.Warn(fs.logger).Log("msg", "failed to remove unused scratch file", "path", f.Name(), "err", err)
		}
		return InvalidHandle, err
	}

	fs.mut.Lock()
	defer fs.mut.Unlock()

	handle := fs.getNextHandle()
	fs.files[handle] = filepath.Base(f.Name())
	return handle, err
}

// getNextHandle returns the next unique handle. getNextHandle must be called
// while holding m.mut.
func (fs *Filesystem) getNextHandle() Handle {
	handle := Handle(*fs.nextHandle)
	*fs.nextHandle++
	return handle
}

// Read returns a reader for the file identifier by h. Read returns
// [HandleNotFoundError] if h doesn't exist either on disk or in-memory.
//
// Callers must close the reader when done to release resources. Reading may
// fail if the handle is removed while reading data.
func (fs *Filesystem) Read(h Handle) (io.ReadSeekCloser, error) {
	fs.mut.RLock()
	name, ok := fs.files[h]
	fs.mut.RUnlock()

	if !ok {
		return fs.fallback.Read(h)
	}
	return os.Open(filepath.Join(fs.path, name))
}

// Remove removes the handle identified by h. If h is stored on disk, its
// corresponding file will be removed. Remove returns [HandleNotFoundError] if h
// doesn't exist on disk or in-memory.
func (fs *Filesystem) Remove(h Handle) error {
	fs.mut.Lock()
	defer fs.mut.Unlock()

	name, ok := fs.files[h]
	if !ok {
		return fs.fallback.Remove(h)
	}
	fullPath := filepath.Join(fs.path, name)

	// Opportunistically try to remove the file, but we don't want to fail the
	// entire Remove call if there's a disk error.
	if err := os.Remove(fullPath); err != nil && !os.IsNotExist(err) {
		level.Warn(fs.logger).Log("msg", "failed to remove cache file", "path", fullPath, "err", err)
	}

	delete(fs.files, h)
	return nil
}
