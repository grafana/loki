// Copyright 2018 Google Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package renameio

import (
	"io/ioutil"
	"os"
	"path/filepath"
)

// TempDir checks whether os.TempDir() can be used as a temporary directory for
// later atomically replacing files within dest. If no (os.TempDir() resides on
// a different mount point), dest is returned.
//
// Note that the returned value ceases to be valid once either os.TempDir()
// changes (e.g. on Linux, once the TMPDIR environment variable changes) or the
// file system is unmounted.
func TempDir(dest string) string {
	return tempDir("", filepath.Join(dest, "renameio-TempDir"))
}

func tempDir(dir, dest string) string {
	if dir != "" {
		return dir // caller-specified directory always wins
	}

	// Chose the destination directory as temporary directory so that we
	// definitely can rename the file, for which both temporary and destination
	// file need to point to the same mount point.
	fallback := filepath.Dir(dest)

	// The user might have overridden the os.TempDir() return value by setting
	// the TMPDIR environment variable.
	tmpdir := os.TempDir()

	testsrc, err := ioutil.TempFile(tmpdir, "."+filepath.Base(dest))
	if err != nil {
		return fallback
	}
	cleanup := true
	defer func() {
		if cleanup {
			os.Remove(testsrc.Name())
		}
	}()
	testsrc.Close()

	testdest, err := ioutil.TempFile(filepath.Dir(dest), "."+filepath.Base(dest))
	if err != nil {
		return fallback
	}
	defer os.Remove(testdest.Name())
	testdest.Close()

	if err := os.Rename(testsrc.Name(), testdest.Name()); err != nil {
		return fallback
	}
	cleanup = false // testsrc no longer exists
	return tmpdir
}

// PendingFile is a pending temporary file, waiting to replace the destination
// path in a call to CloseAtomicallyReplace.
type PendingFile struct {
	*os.File

	path   string
	done   bool
	closed bool
}

// Cleanup is a no-op if CloseAtomicallyReplace succeeded, and otherwise closes
// and removes the temporary file.
func (t *PendingFile) Cleanup() error {
	if t.done {
		return nil
	}
	// An error occurred. Close and remove the tempfile. Errors are returned for
	// reporting, there is nothing the caller can recover here.
	var closeErr error
	if !t.closed {
		closeErr = t.Close()
	}
	if err := os.Remove(t.Name()); err != nil {
		return err
	}
	return closeErr
}

// CloseAtomicallyReplace closes the temporary file and atomically replaces
// the destination file with it, i.e., a concurrent open(2) call will either
// open the file previously located at the destination path (if any), or the
// just written file, but the file will always be present.
func (t *PendingFile) CloseAtomicallyReplace() error {
	// Even on an ordered file system (e.g. ext4 with data=ordered) or file
	// systems with write barriers, we cannot skip the fsync(2) call as per
	// Theodore Ts'o (ext2/3/4 lead developer):
	//
	// > data=ordered only guarantees the avoidance of stale data (e.g., the previous
	// > contents of a data block showing up after a crash, where the previous data
	// > could be someone's love letters, medical records, etc.). Without the fsync(2)
	// > a zero-length file is a valid and possible outcome after the rename.
	if err := t.Sync(); err != nil {
		return err
	}
	t.closed = true
	if err := t.Close(); err != nil {
		return err
	}
	if err := os.Rename(t.Name(), t.path); err != nil {
		return err
	}
	t.done = true
	return nil
}

// TempFile wraps ioutil.TempFile for the use case of atomically creating or
// replacing the destination file at path.
//
// If dir is the empty string, TempDir(filepath.Base(path)) is used. If you are
// going to write a large number of files to the same file system, store the
// result of TempDir(filepath.Base(path)) and pass it instead of the empty
// string.
//
// The file's permissions will be 0600 by default. You can change these by
// explicitly calling Chmod on the returned PendingFile.
func TempFile(dir, path string) (*PendingFile, error) {
	f, err := ioutil.TempFile(tempDir(dir, path), "."+filepath.Base(path))
	if err != nil {
		return nil, err
	}

	return &PendingFile{File: f, path: path}, nil
}

// Symlink wraps os.Symlink, replacing an existing symlink with the same name
// atomically (os.Symlink fails when newname already exists, at least on Linux).
func Symlink(oldname, newname string) error {
	// Fast path: if newname does not exist yet, we can skip the whole dance
	// below.
	if err := os.Symlink(oldname, newname); err == nil || !os.IsExist(err) {
		return err
	}

	// We need to use ioutil.TempDir, as we cannot overwrite a ioutil.TempFile,
	// and removing+symlinking creates a TOCTOU race.
	d, err := ioutil.TempDir(filepath.Dir(newname), "."+filepath.Base(newname))
	if err != nil {
		return err
	}
	defer os.RemoveAll(d)

	symlink := filepath.Join(d, "tmp.symlink")
	if err := os.Symlink(oldname, symlink); err != nil {
		return err
	}

	if err := os.Rename(symlink, newname); err != nil {
		return err
	}

	return os.RemoveAll(d)
}
