package local

import (
	"os"
	"sync"
	"time"

	"go.etcd.io/bbolt"
)

type downloadedFile struct {
	mtime  time.Time
	boltdb *bbolt.DB
}

// filesCollection holds info about shipped boltdb index files by other uploaders(ingesters).
// It is used to hold boltdb files created by all the ingesters for same period i.e with same name.
// In the object store files are uploaded as <boltdb-filename>/<uploader-id> to manage files with same name from different ingesters.
// Note: filesCollection does not manage the locks on mutex to allow users of it to
// do batch operations or single operation or operations requiring intermittent locking.
// Note2: It has an err variable which is set when filesCollection is in invalid state due to some issue.
// All operations which try to access/update the files except cleanup returns an error if set.
type filesCollection struct {
	sync.RWMutex
	lastUsedAt time.Time
	files      map[string]*downloadedFile
	err        error
}

func newFilesCollection() *filesCollection {
	return &filesCollection{files: map[string]*downloadedFile{}}
}

func (fc *filesCollection) addFile(fileName string, df *downloadedFile) error {
	if fc.err != nil {
		return fc.err
	}

	fc.files[fileName] = df
	return nil
}

func (fc *filesCollection) getFile(fileName string) (*downloadedFile, error) {
	if fc.err != nil {
		return nil, fc.err
	}

	return fc.files[fileName], nil
}

func (fc *filesCollection) cleanupFile(fileName string) error {
	boltdbFile := fc.files[fileName].boltdb
	filePath := boltdbFile.Path()

	if err := boltdbFile.Close(); err != nil {
		return err
	}

	delete(fc.files, fileName)

	return os.Remove(filePath)
}

func (fc *filesCollection) iter(callback func(uploader string, df *downloadedFile) error) error {
	if fc.err != nil {
		return fc.err
	}

	for uploader, df := range fc.files {
		if err := callback(uploader, df); err != nil {
			return err
		}
	}

	return nil
}

func (fc *filesCollection) cleanupAllFiles() error {
	for uploader, _ := range fc.files {
		if err := fc.cleanupFile(uploader); err != nil {
			return err
		}
	}
	return nil
}

func (fc *filesCollection) updateLastUsedAt() {
	fc.lastUsedAt = time.Now()
}

func (fc *filesCollection) lastUsedTime() time.Time {
	return fc.lastUsedAt
}

func (fc *filesCollection) setErr(err error) {
	fc.err = err
}
