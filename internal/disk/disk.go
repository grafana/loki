package disk

import (
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/ViaQ/logerr/kverrors"
	"sigs.k8s.io/kustomize/api/filesys"
)

var _ filesys.FileSystem = Filesystem{}

// Filesystem implements FileSystem using the local filesystem.
type Filesystem struct{}

// Create delegates to os.Create.
func (Filesystem) Create(name string) (filesys.File, error) { return os.Create(name) }

// Mkdir delegates to os.Mkdir.
func (Filesystem) Mkdir(name string) error {
	return os.Mkdir(name, 0777|os.ModeDir)
}

// MkdirAll delegates to os.MkdirAll.
func (Filesystem) MkdirAll(name string) error {
	return os.MkdirAll(name, 0777|os.ModeDir)
}

// RemoveAll delegates to os.RemoveAll.
func (Filesystem) RemoveAll(name string) error {
	return os.RemoveAll(name)
}

// Open delegates to os.Open.
func (Filesystem) Open(name string) (filesys.File, error) { return os.Open(name) }

// CleanedAbs converts the given path into a
// directory and a file name, where the directory
// is represented as a ConfirmedDir and all that implies.
// If the entire path is a directory, the file component
// is an empty string.
func (x Filesystem) CleanedAbs(
	path string) (filesys.ConfirmedDir, string, error) {
	absRoot, err := filepath.Abs(path)
	if err != nil {
		return "", "", kverrors.Wrap(err, "abs path error", "path", path)
	}
	deLinked, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return "", "", kverrors.Wrap(err, "evalsymlink failure", "path", path)
	}
	if x.IsDir(deLinked) {
		return filesys.ConfirmedDir(deLinked), "", nil
	}
	d := filepath.Dir(deLinked)
	if !x.IsDir(d) {
		// Programmer/assumption error.
		return "", "", kverrors.New("first part of path not a directory", "delinked_path", deLinked)
	}
	if d == deLinked {
		// Programmer/assumption error.
		return "", "", kverrors.New("dir should be a subset of deLinked path", "dir", d, "delinked_path", deLinked)
	}
	f := filepath.Base(deLinked)
	if filepath.Join(d, f) != deLinked {
		// Programmer/assumption error.
		return "", "", kverrors.New("should be equal",
			"path", filepath.Join(d, f),
			"delinked_path", deLinked)
	}
	return filesys.ConfirmedDir(d), f, nil
}

// Exists returns true if os.Stat succeeds.
func (Filesystem) Exists(name string) bool {
	_, err := os.Stat(name)
	return err == nil
}

// Glob returns the list of matching files
func (Filesystem) Glob(pattern string) ([]string, error) {
	return filepath.Glob(pattern)
}

// IsDir delegates to os.Stat and FileInfo.IsDir
func (Filesystem) IsDir(name string) bool {
	info, err := os.Stat(name)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// ReadFile delegates to ioutil.ReadFile.
func (Filesystem) ReadFile(name string) ([]byte, error) { return ioutil.ReadFile(name) }

// WriteFile delegates to ioutil.WriteFile with read/write permissions.
func (Filesystem) WriteFile(name string, c []byte) error {
	return ioutil.WriteFile(name, c, 0666)
}

// Walk delegates to filepath.Walk.
func (Filesystem) Walk(path string, walkFn filepath.WalkFunc) error {
	return filepath.Walk(path, walkFn)
}
