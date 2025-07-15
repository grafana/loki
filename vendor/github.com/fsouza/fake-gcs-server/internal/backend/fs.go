// Copyright 2018 Francisco Souza. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package backend

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fsouza/fake-gcs-server/internal/checksum"
	"github.com/pkg/xattr"
)

// storageFS is an implementation of the backend storage that stores data on disk
//
// The layout is the following:
//
// - rootDir
//
//	|- bucket1
//	\- bucket2
//	  |- object1
//	  \- object2
//
// Bucket and object names are url path escaped, so there's no special meaning of forward slashes.
type storageFS struct {
	rootDir string
	mtx     sync.RWMutex
	mh      metadataHandler
}

// NewStorageFS creates an instance of the filesystem-backed storage backend.
func NewStorageFS(objects []StreamingObject, rootDir string) (Storage, error) {
	if !strings.HasSuffix(rootDir, "/") {
		rootDir += "/"
	}
	err := os.MkdirAll(rootDir, 0o700)
	if err != nil {
		return nil, err
	}

	var mh metadataHandler = metadataFile{}
	// Use xattr for metadata if rootDir supports it.
	if xattr.XATTR_SUPPORTED {
		xattrHandler := metadataXattr{}
		var xerr *xattr.Error
		_, err = xattrHandler.read(rootDir)
		if err == nil || (errors.As(err, &xerr) && xerr.Err == xattr.ENOATTR) {
			mh = xattrHandler
		}
	}

	s := &storageFS{rootDir: rootDir, mh: mh}
	for _, o := range objects {
		obj, err := s.CreateObject(o, NoConditions{})
		if err != nil {
			return nil, err
		}
		obj.Close()
	}
	return s, nil
}

// CreateBucket creates a bucket in the fs backend. A bucket is a folder in the
// root directory.
func (s *storageFS) CreateBucket(name string, bucketAttrs BucketAttrs) error {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	return s.createBucket(name, bucketAttrs)
}

func (s *storageFS) createBucket(name string, bucketAttrs BucketAttrs) error {
	if bucketAttrs.VersioningEnabled {
		return errors.New("not implemented: fs storage type does not support versioning yet")
	}
	path := filepath.Join(s.rootDir, url.PathEscape(name))
	err := os.MkdirAll(path, 0o700)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(bucketAttrs)
	if err != nil {
		return err
	}
	return writeFile(path+bucketMetadataSuffix, encoded, 0o600)
}

// ListBuckets returns a list of buckets from the list of directories in the
// root directory.
func (s *storageFS) ListBuckets() ([]Bucket, error) {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	infos, err := os.ReadDir(s.rootDir)
	if err != nil {
		return nil, err
	}
	buckets := []Bucket{}
	for _, info := range infos {
		if info.IsDir() {
			unescaped, err := url.PathUnescape(info.Name())
			if err != nil {
				return nil, fmt.Errorf("failed to unescape object name %s: %w", info.Name(), err)
			}
			fileInfo, err := info.Info()
			if err != nil {
				return nil, fmt.Errorf("failed to get file info for %s: %w", info.Name(), err)
			}
			buckets = append(buckets, Bucket{Name: unescaped, TimeCreated: timespecToTime(createTimeFromFileInfo(fileInfo))})
		}
	}
	return buckets, nil
}

func timespecToTime(ts syscall.Timespec) time.Time {
	return time.Unix(int64(ts.Sec), int64(ts.Nsec))
}

func (s *storageFS) UpdateBucket(bucketName string, attrsToUpdate BucketAttrs) error {
	if attrsToUpdate.VersioningEnabled {
		return errors.New("not implemented: fs storage type does not support versioning yet")
	}
	encoded, err := json.Marshal(attrsToUpdate)
	if err != nil {
		return err
	}
	path := filepath.Join(s.rootDir, url.PathEscape(bucketName))
	return writeFile(path+bucketMetadataSuffix, encoded, 0o600)
}

// GetBucket returns information about the given bucket, or an error if it
// doesn't exist.
func (s *storageFS) GetBucket(name string) (Bucket, error) {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	path := filepath.Join(s.rootDir, url.PathEscape(name))
	dirInfo, err := os.Stat(path)
	if err != nil {
		return Bucket{}, err
	}
	attrs, err := getBucketAttributes(path)
	if err != nil {
		return Bucket{}, err
	}
	return Bucket{Name: name, VersioningEnabled: false, TimeCreated: timespecToTime(createTimeFromFileInfo(dirInfo)), DefaultEventBasedHold: attrs.DefaultEventBasedHold}, err
}

func getBucketAttributes(path string) (BucketAttrs, error) {
	content, err := os.ReadFile(path + bucketMetadataSuffix)
	if err != nil {
		if os.IsNotExist(err) {
			return BucketAttrs{}, nil
		}
		return BucketAttrs{}, err
	}
	var attrs BucketAttrs
	err = json.Unmarshal(content, &attrs)
	if err != nil {
		return BucketAttrs{}, err
	}
	return attrs, nil
}

// DeleteBucket removes the bucket from the backend.
func (s *storageFS) DeleteBucket(name string) error {
	objs, err := s.ListObjects(name, "", false)
	if err != nil {
		return BucketNotFound
	}
	if len(objs) > 0 {
		return BucketNotEmpty
	}

	s.mtx.Lock()
	defer s.mtx.Unlock()
	return os.RemoveAll(filepath.Join(s.rootDir, url.PathEscape(name)))
}

// CreateObject stores an object as a regular file on disk. The backing content
// for the object may be in the same file that's being updated, so a temporary
// file is first created and then moved into place. This also makes it so any
// object content readers currently open continue reading from the original
// file instead of the newly created file.
//
// The crc32c checksum and md5 hash of the object content is calculated when
// reading the object content. Any checksum or hash in the passed-in object
// metadata is overwritten.
func (s *storageFS) CreateObject(obj StreamingObject, conditions Conditions) (StreamingObject, error) {
	if obj.Generation > 0 {
		return StreamingObject{}, errors.New("not implemented: fs storage type does not support objects generation yet")
	}

	// Note: this was a quick fix for issue #701. Now that we have a way to
	// persist object attributes, we should implement versioning in the
	// filesystem backend and handle generations outside of the backends.
	obj.Generation = time.Now().UnixNano() / 1000

	s.mtx.Lock()
	defer s.mtx.Unlock()
	err := s.createBucket(obj.BucketName, BucketAttrs{VersioningEnabled: false})
	if err != nil {
		return StreamingObject{}, err
	}

	var activeGeneration int64
	existingObj, err := s.getObject(obj.BucketName, obj.Name)
	if err != nil {
		activeGeneration = 0
	} else {
		activeGeneration = existingObj.Generation
	}

	if !conditions.ConditionsMet(activeGeneration) {
		return StreamingObject{}, PreConditionFailed
	}

	path := filepath.Join(s.rootDir, url.PathEscape(obj.BucketName), obj.Name)
	if err = os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return StreamingObject{}, err
	}

	// Nothing to do if this operation only creates directories
	if strings.HasSuffix(obj.Name, "/") {
		// TODO: populate Crc32c, Md5Hash, and Etag
		return StreamingObject{obj.ObjectAttrs, noopSeekCloser{bytes.NewReader([]byte{})}}, nil
	}

	var buf bytes.Buffer
	hasher := checksum.NewStreamingHasher()
	objectContent := io.TeeReader(obj.Content, hasher)

	if _, err = io.Copy(&buf, objectContent); err != nil {
		return StreamingObject{}, err
	}

	if obj.Crc32c == "" {
		obj.Crc32c = hasher.EncodedCrc32cChecksum()
	}
	if obj.Md5Hash == "" {
		obj.Md5Hash = hasher.EncodedMd5Hash()
	}
	if obj.Etag == "" {
		obj.Etag = obj.Md5Hash
	}
	if obj.StorageClass == "" {
		obj.StorageClass = "STANDARD"
	}

	// TODO: Handle if metadata is not present more gracefully?
	encoded, err := json.Marshal(obj.ObjectAttrs)
	if err != nil {
		return StreamingObject{}, err
	}

	if err := writeFile(path, buf.Bytes(), 0o600); err != nil {
		return StreamingObject{}, err
	}

	if err = s.mh.write(path, encoded); err != nil {
		return StreamingObject{}, err
	}

	err = openObjectAndSetSize(&obj, path)

	return obj, err
}

// ListObjects lists the objects in a given bucket with a given prefix and
// delimiter.
func (s *storageFS) ListObjects(bucketName string, prefix string, versions bool) ([]ObjectAttrs, error) {
	s.mtx.RLock()
	defer s.mtx.RUnlock()

	objects := []ObjectAttrs{}
	bucketPath := filepath.Join(s.rootDir, url.PathEscape(bucketName))
	if err := filepath.Walk(bucketPath, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}

		objName, _ := filepath.Rel(bucketPath, path)
		if s.mh.isSpecialFile(info.Name()) {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		if prefix != "" && !strings.HasPrefix(objName, prefix) {
			return nil
		}
		objAttrs, err := s.getObjectAttrs(bucketName, objName)
		if err != nil {
			return err
		}
		objects = append(objects, objAttrs)
		return nil
	}); err != nil {
		return nil, err
	}
	return objects, nil
}

// GetObject get an object by bucket and name.
func (s *storageFS) GetObject(bucketName, objectName string) (StreamingObject, error) {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	return s.getObject(bucketName, objectName)
}

// GetObjectWithGeneration retrieves a specific version of the object. Not
// implemented for this backend.
func (s *storageFS) GetObjectWithGeneration(bucketName, objectName string, generation int64) (StreamingObject, error) {
	obj, err := s.GetObject(bucketName, objectName)
	if err != nil {
		return obj, err
	}
	if obj.Generation != generation {
		return obj, fmt.Errorf("generation mismatch, object generation is %v, requested generation is %v (note: filesystem backend does not support versioning)", obj.Generation, generation)
	}
	return obj, nil
}

func (s *storageFS) getObject(bucketName, objectName string) (StreamingObject, error) {
	attrs, err := s.getObjectAttrs(bucketName, objectName)
	if err != nil {
		return StreamingObject{}, err
	}

	obj := StreamingObject{ObjectAttrs: attrs}
	path := filepath.Join(s.rootDir, url.PathEscape(bucketName), objectName)
	err = openObjectAndSetSize(&obj, path)

	return obj, err
}

func openObjectAndSetSize(obj *StreamingObject, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}

	obj.Content = newLazyReader(path)
	obj.Size = info.Size()

	return nil
}

func (s *storageFS) getObjectAttrs(bucketName, objectName string) (ObjectAttrs, error) {
	path := filepath.Join(s.rootDir, url.PathEscape(bucketName), objectName)
	encoded, err := s.mh.read(path)
	if err != nil {
		return ObjectAttrs{}, err
	}

	var attrs ObjectAttrs
	if err = json.Unmarshal(encoded, &attrs); err != nil {
		return ObjectAttrs{}, err
	}

	info, err := os.Stat(path)
	if err != nil {
		return ObjectAttrs{}, fmt.Errorf("failed to stat: %w", err)
	}

	attrs.Name = filepath.ToSlash(objectName)
	attrs.BucketName = bucketName
	attrs.Size = info.Size()
	return attrs, nil
}

// DeleteObject deletes an object by bucket and name.
func (s *storageFS) DeleteObject(bucketName, objectName string) error {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	if objectName == "" {
		return errors.New("can't delete object with empty name")
	}
	path := filepath.Join(s.rootDir, url.PathEscape(bucketName), objectName)
	if err := s.mh.remove(path); err != nil {
		return err
	}
	return os.Remove(path)
}

func (s *storageFS) PatchObject(bucketName, objectName string, attrsToUpdate ObjectAttrs) (StreamingObject, error) {
	obj, err := s.GetObject(bucketName, objectName)
	if err != nil {
		return StreamingObject{}, err
	}
	defer obj.Close()

	obj.patch(attrsToUpdate)
	obj.Generation = 0 // reset generation id
	return s.CreateObject(obj, NoConditions{})
}

func (s *storageFS) UpdateObject(bucketName, objectName string, attrsToUpdate ObjectAttrs) (StreamingObject, error) {
	obj, err := s.GetObject(bucketName, objectName)
	if err != nil {
		return StreamingObject{}, err
	}
	defer obj.Close()

	if attrsToUpdate.Metadata != nil {
		obj.Metadata = map[string]string{}
	}
	obj.patch(attrsToUpdate)
	obj.Generation = 0 // reset generation id
	return s.CreateObject(obj, NoConditions{})
}

type concatenatedContent struct {
	io.Reader
}

func (c concatenatedContent) Close() error {
	return errors.New("not implemented")
}

func (c concatenatedContent) Seek(offset int64, whence int) (int64, error) {
	return 0, errors.New("not implemented")
}

func concatObjectReaders(objects []StreamingObject) io.ReadSeekCloser {
	readers := make([]io.Reader, len(objects))
	for i := range objects {
		readers[i] = objects[i].Content
	}
	return concatenatedContent{io.MultiReader(readers...)}
}

func (s *storageFS) ComposeObject(bucketName string, objectNames []string, destinationName string, metadata map[string]string, contentType string, contentDisposition string, contentLanguage string) (StreamingObject, error) {
	var sourceObjects []StreamingObject
	for _, n := range objectNames {
		obj, err := s.GetObject(bucketName, n)
		if err != nil {
			return StreamingObject{}, err
		}
		defer obj.Close()
		sourceObjects = append(sourceObjects, obj)
	}

	now := time.Now().Format(timestampFormat)
	dest := StreamingObject{
		ObjectAttrs: ObjectAttrs{
			BucketName:         bucketName,
			Name:               destinationName,
			ContentType:        contentType,
			ContentDisposition: contentDisposition,
			ContentLanguage:    contentLanguage,
			Created:            now,
			Updated:            now,
		},
	}

	dest.Content = concatObjectReaders(sourceObjects)
	dest.Metadata = metadata

	result, err := s.CreateObject(dest, NoConditions{})
	if err != nil {
		return result, err
	}

	return result, nil
}

func (s *storageFS) DeleteAllFiles() error {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	if err := os.RemoveAll(s.rootDir); err != nil {
		return err
	}
	return os.MkdirAll(s.rootDir, 0o700)
}
