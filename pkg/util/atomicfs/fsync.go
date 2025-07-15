// SPDX-License-Identifier: AGPL-3.0-only

package atomicfs

import (
	"io"
	"os"
	"path/filepath"

	"github.com/grafana/dskit/multierror"
)

// Create creates a new file at a temporary path that will be renamed to the
// supplied path on close from a temporary file in the same directory, ensuring
// all data and the containing directory have been fsynced to disk.
func Create(path string) (*File, error) {
	// We rename from a temporary file in the same directory to because rename
	// can only operate on two files that are on the same filesystem. Creating
	// a temporary file in the same directory is an easy way to guarantee that.
	final := filepath.Clean(path)
	tmp := tempPath(final)

	file, err := os.Create(tmp)
	if err != nil {
		return nil, err
	}

	return &File{
		File:      file,
		finalPath: final,
	}, nil
}

// tempPath returns a path for the temporary version of a file. This function exists
// to ensure the logic here stays in sync with unit tests that check for this file being
// cleaned up.
func tempPath(final string) string {
	return final + ".tmp"
}

// File is a wrapper around an os.File instance that uses a temporary file for writes
// that is renamed to its final path when Close is called. The Close method will also
// ensure that all data from the file has been fsynced as well as the containing
// directory. If the temporary file cannot be renamed or fsynced on Close, it is
// removed.
type File struct {
	*os.File
	finalPath string
}

func (a *File) Close() error {
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(a.File.Name())
		}
	}()

	merr := multierror.New()
	merr.Add(a.File.Sync())
	merr.Add(a.File.Close())
	if err := merr.Err(); err != nil {
		return err
	}

	if err := os.Rename(a.File.Name(), a.finalPath); err != nil {
		return err
	}

	cleanup = false
	// After writing the file and calling fsync on it, fsync the containing directory
	// to ensure the directory entry is persisted to disk.
	//
	// From https://man7.org/linux/man-pages/man2/fsync.2.html
	// > Calling fsync() does not necessarily ensure that the entry in the
	// > directory containing the file has also reached disk.  For that an
	// > explicit fsync() on a file descriptor for the directory is also
	// > needed.
	dir, err := os.Open(filepath.Dir(a.finalPath))
	if err != nil {
		return err
	}

	merr.Add(dir.Sync())
	merr.Add(dir.Close())
	return merr.Err()
}

// CreateFile safely writes the contents of data to filePath, ensuring that all data
// has been fsynced as well as the containing directory of the file.
func CreateFile(filePath string, data io.Reader) error {
	f, err := Create(filePath)
	if err != nil {
		return err
	}

	_, err = io.Copy(f, data)
	merr := multierror.New(err)
	merr.Add(f.Close())
	return merr.Err()
}
