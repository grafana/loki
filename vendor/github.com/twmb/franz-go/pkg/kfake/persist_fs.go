package kfake

import (
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// fs wraps filesystem operations for testability. The default
// implementation delegates to the os package; tests can inject
// memFS for fault-injection and crash simulation.
type fs interface {
	OpenFile(name string, flag int, perm os.FileMode) (file, error)
	Rename(oldpath, newpath string) error
	Remove(name string) error
	RemoveAll(path string) error
	MkdirAll(path string, perm os.FileMode) error
	ReadDir(name string) ([]os.DirEntry, error)
	ReadFile(name string) ([]byte, error)
	Stat(name string) (os.FileInfo, error)
}

// file wraps a writable, seekable file handle.
type file interface {
	io.Writer
	io.Reader
	io.Closer
	Seek(offset int64, whence int) (int64, error)
	Truncate(size int64) error
	Sync() error
}

// osFS is the real-filesystem implementation of fs.
type osFS struct{}

func (osFS) OpenFile(name string, flag int, perm os.FileMode) (file, error) {
	return os.OpenFile(name, flag, perm)
}
func (osFS) Rename(oldpath, newpath string) error         { return os.Rename(oldpath, newpath) }
func (osFS) Remove(name string) error                     { return os.Remove(name) }
func (osFS) RemoveAll(path string) error                  { return os.RemoveAll(path) }
func (osFS) MkdirAll(path string, perm os.FileMode) error { return os.MkdirAll(path, perm) }
func (osFS) ReadDir(name string) ([]os.DirEntry, error)   { return os.ReadDir(name) }
func (osFS) ReadFile(name string) ([]byte, error)         { return os.ReadFile(name) }
func (osFS) Stat(name string) (os.FileInfo, error)        { return os.Stat(name) }

// memFS is an in-memory filesystem for testing persistence without
// touching the real disk. It supports fault injection via fail flags.
type memFS struct {
	mu    sync.Mutex
	files map[string]*memFileData
	dirs  map[string]bool

	// Fault injection: set these before an operation to simulate errors.
	failNextSync  error
	failNextWrite error
}

// memFileData is the backing storage for a memFS file.
type memFileData struct {
	data []byte
}

func newMemFS() *memFS {
	return &memFS{
		files: make(map[string]*memFileData),
		dirs:  map[string]bool{"/": true},
	}
}

// memFile is an open handle to a memFS file.
type memFile struct {
	fs   *memFS
	name string
	d    *memFileData
	pos  int64
	flag int
}

func (m *memFS) OpenFile(name string, flag int, _ os.FileMode) (file, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	d, exists := m.files[name]
	if flag&os.O_CREATE != 0 && !exists {
		d = &memFileData{}
		m.files[name] = d
	}
	if d == nil {
		return nil, &os.PathError{Op: "open", Path: name, Err: os.ErrNotExist}
	}
	if flag&os.O_TRUNC != 0 {
		d.data = d.data[:0]
	}
	var pos int64
	if flag&os.O_APPEND != 0 {
		pos = int64(len(d.data))
	}
	return &memFile{fs: m, name: name, d: d, pos: pos, flag: flag}, nil
}

func (m *memFS) Rename(oldpath, newpath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.files[oldpath]
	if !ok {
		return &os.PathError{Op: "rename", Path: oldpath, Err: os.ErrNotExist}
	}
	m.files[newpath] = d
	delete(m.files, oldpath)
	return nil
}

func (m *memFS) Remove(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.files[name]; ok {
		delete(m.files, name)
		return nil
	}
	if _, ok := m.dirs[name]; ok {
		delete(m.dirs, name)
		return nil
	}
	return &os.PathError{Op: "remove", Path: name, Err: os.ErrNotExist}
}

func (m *memFS) RemoveAll(path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	prefix := path + "/"
	for k := range m.files {
		if k == path || strings.HasPrefix(k, prefix) {
			delete(m.files, k)
		}
	}
	for k := range m.dirs {
		if k == path || strings.HasPrefix(k, prefix) {
			delete(m.dirs, k)
		}
	}
	return nil
}

func (m *memFS) MkdirAll(path string, _ os.FileMode) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dirs[path] = true
	return nil
}

func (m *memFS) ReadDir(name string) ([]os.DirEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	prefix := name
	if prefix != "/" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	seen := make(map[string]bool)
	var entries []os.DirEntry
	for k, d := range m.files {
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		rest := k[len(prefix):]
		// Only direct children (no further /).
		if slash := strings.IndexByte(rest, '/'); slash >= 0 {
			dirName := rest[:slash]
			if !seen[dirName] {
				seen[dirName] = true
				entries = append(entries, memDirEntry{name: dirName, isDir: true})
			}
			continue
		}
		if rest != "" && !seen[rest] {
			seen[rest] = true
			entries = append(entries, memDirEntry{name: rest, size: int64(len(d.data))})
		}
	}
	for k := range m.dirs {
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		rest := k[len(prefix):]
		if rest != "" && !strings.Contains(rest, "/") && !seen[rest] {
			seen[rest] = true
			entries = append(entries, memDirEntry{name: rest, isDir: true})
		}
	}
	if len(entries) == 0 && !m.dirs[name] {
		return nil, &os.PathError{Op: "readdir", Path: name, Err: os.ErrNotExist}
	}
	return entries, nil
}

func (m *memFS) ReadFile(name string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.files[name]
	if !ok {
		return nil, &os.PathError{Op: "read", Path: name, Err: os.ErrNotExist}
	}
	out := make([]byte, len(d.data))
	copy(out, d.data)
	return out, nil
}

func (m *memFS) Stat(name string) (os.FileInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if d, ok := m.files[name]; ok {
		return memFileInfo{name: name, size: int64(len(d.data))}, nil
	}
	if m.dirs[name] {
		return memFileInfo{name: name, isDir: true}, nil
	}
	return nil, &os.PathError{Op: "stat", Path: name, Err: os.ErrNotExist}
}

// memFile methods

func (f *memFile) Write(b []byte) (int, error) {
	f.fs.mu.Lock()
	defer f.fs.mu.Unlock()
	if err := f.fs.failNextWrite; err != nil {
		f.fs.failNextWrite = nil
		return 0, err
	}
	// Match POSIX O_APPEND: writes always go to the end.
	if f.flag&os.O_APPEND != 0 {
		f.pos = int64(len(f.d.data))
	}
	end := f.pos + int64(len(b))
	if end > int64(len(f.d.data)) {
		if end > int64(cap(f.d.data)) {
			newCap := max(int64(cap(f.d.data))*2, end)
			grown := make([]byte, end, newCap)
			copy(grown, f.d.data)
			f.d.data = grown
		} else {
			f.d.data = f.d.data[:end]
		}
	}
	copy(f.d.data[f.pos:], b)
	f.pos = end
	return len(b), nil
}

func (f *memFile) Read(b []byte) (int, error) {
	f.fs.mu.Lock()
	defer f.fs.mu.Unlock()
	if f.pos >= int64(len(f.d.data)) {
		return 0, io.EOF
	}
	n := copy(b, f.d.data[f.pos:])
	f.pos += int64(n)
	return n, nil
}

func (f *memFile) Seek(offset int64, whence int) (int64, error) {
	f.fs.mu.Lock()
	defer f.fs.mu.Unlock()
	switch whence {
	case io.SeekStart:
		f.pos = offset
	case io.SeekCurrent:
		f.pos += offset
	case io.SeekEnd:
		f.pos = int64(len(f.d.data)) + offset
	}
	return f.pos, nil
}

func (f *memFile) Truncate(size int64) error {
	f.fs.mu.Lock()
	defer f.fs.mu.Unlock()
	if size < int64(len(f.d.data)) {
		f.d.data = f.d.data[:size]
	} else if size > int64(len(f.d.data)) {
		if size > int64(cap(f.d.data)) {
			newCap := max(int64(cap(f.d.data))*2, size)
			grown := make([]byte, size, newCap)
			copy(grown, f.d.data)
			f.d.data = grown
		} else {
			// Zero the new bytes (POSIX truncate extension fills with zeros).
			prev := len(f.d.data)
			f.d.data = f.d.data[:size]
			clear(f.d.data[prev:])
		}
	}
	return nil
}

func (f *memFile) Sync() error {
	f.fs.mu.Lock()
	defer f.fs.mu.Unlock()
	if err := f.fs.failNextSync; err != nil {
		f.fs.failNextSync = nil
		return err
	}
	return nil
}

func (*memFile) Close() error { return nil }

// memDirEntry implements os.DirEntry for memFS.
type memDirEntry struct {
	name  string
	isDir bool
	size  int64
}

func (e memDirEntry) Name() string    { return e.name }
func (e memDirEntry) IsDir() bool     { return e.isDir }
func (memDirEntry) Type() os.FileMode { return 0 }
func (e memDirEntry) Info() (os.FileInfo, error) {
	return memFileInfo{name: e.name, isDir: e.isDir, size: e.size}, nil
}

// memFileInfo implements os.FileInfo for memFS.
type memFileInfo struct {
	name  string
	size  int64
	isDir bool
}

func (i memFileInfo) Name() string     { return i.name }
func (i memFileInfo) Size() int64      { return i.size }
func (memFileInfo) Mode() os.FileMode  { return 0o644 }
func (memFileInfo) ModTime() time.Time { return time.Time{} }
func (i memFileInfo) IsDir() bool      { return i.isDir }
func (memFileInfo) Sys() any           { return nil }
