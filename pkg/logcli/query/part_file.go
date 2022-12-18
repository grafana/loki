package query

import (
	"errors"
	"fmt"
	"os"
)

// PartFile partially complete file.
// Expected usage:
//  1. Create the temp file: CreateTemp
//  2. Write the data to the file
//  3. When you're done, call Complete and the temp file will be renamed
type PartFile struct {
	fd           *os.File
	completeName string
}

// NewPartFile creates a new partial file, setting the filename which will be used
// when the file is closed with the Complete function.
func NewPartFile(filename string) *PartFile {
	return &PartFile{
		fd:           nil,
		completeName: filename,
	}
}

// Exists checks if the completed file exists.
func (f *PartFile) Exists() (bool, error) {
	if _, err := os.Stat(f.completeName); err == nil {
		// No error means file exits, and we can stat it.
		return true, nil
	} else if errors.Is(err, os.ErrNotExist) {
		// File does not exist.
		return false, nil
	} else {
		// Unclear if file exists or not, we cannot stat it.
		return false, fmt.Errorf("failed to check if part file exists: %s: %s", f.completeName, err)
	}
}

// CreateTemp creates the temp file to store the data before Complete is called.
func (f *PartFile) CreateTemp() error {
	tmpName := f.completeName + ".tmp"

	fd, err := os.Create(tmpName)
	if err != nil {
		return fmt.Errorf("Failed to create part file: %s: %s", tmpName, err)
	}

	f.fd = fd
	return nil
}

// Write to the temporary file.
func (f *PartFile) Write(b []byte) (int, error) {
	return f.fd.Write(b)
}

// Close closes the temporary file.
// Double close is handled gracefully without error so that Close can be deferred for errors,
// and is also called when Complete is called.
func (f *PartFile) Close() error {
	// Prevent double close
	if f.fd == nil {
		return nil
	}

	filename := f.fd.Name()

	if err := f.fd.Sync(); err != nil {
		return fmt.Errorf("failed to fsync part file: %s: %s", filename, err)
	}

	if err := f.fd.Close(); err != nil {
		return fmt.Errorf("filed to close part file: %s: %s", filename, err)
	}

	f.fd = nil
	return nil
}

// Complete closes the temporary file, and renames it to the complete file name.
func (f *PartFile) Complete() error {
	tmpFileName := f.fd.Name()

	if err := f.Close(); err != nil {
		return fmt.Errorf("failed to close part file: %s: %s", tmpFileName, err)
	}

	if err := os.Rename(tmpFileName, f.completeName); err != nil {
		return fmt.Errorf("failed to rename part file: %s: %s", tmpFileName, err)
	}

	return nil
}
