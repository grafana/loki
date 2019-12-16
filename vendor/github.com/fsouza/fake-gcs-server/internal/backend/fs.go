// Copyright 2018 Francisco Souza. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package backend

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// StorageFS is an implementation of the backend storage that stores data on disk
// The layout is the following:
// - rootDir
//   |- bucket1
//   \- bucket2
//     |- object1
//     \- object2
// Bucket and object names are url path escaped, so there's no special meaning of forward slashes.
type StorageFS struct {
	rootDir string
}

// NewStorageFS creates an instance of StorageMemory
func NewStorageFS(objects []Object, rootDir string) (Storage, error) {
	if !strings.HasSuffix(rootDir, "/") {
		rootDir += "/"
	}
	s := &StorageFS{
		rootDir: rootDir,
	}
	for _, o := range objects {
		err := s.CreateObject(o)
		if err != nil {
			return nil, err
		}
	}
	return s, nil
}

// CreateBucket creates a bucket
func (s *StorageFS) CreateBucket(name string) error {
	return os.MkdirAll(filepath.Join(s.rootDir, url.PathEscape(name)), 0700)
}

// ListBuckets lists buckets
func (s *StorageFS) ListBuckets() ([]string, error) {
	infos, err := ioutil.ReadDir(s.rootDir)
	if err != nil {
		return nil, err
	}
	buckets := []string{}
	for _, info := range infos {
		if info.IsDir() {
			unescaped, err := url.PathUnescape(info.Name())
			if err != nil {
				return nil, fmt.Errorf("failed to unescape object name %s: %s", info.Name(), err)
			}
			buckets = append(buckets, unescaped)
		}
	}
	return buckets, nil
}

// GetBucket checks if a bucket exists
func (s *StorageFS) GetBucket(name string) error {
	_, err := os.Stat(filepath.Join(s.rootDir, url.PathEscape(name)))
	return err
}

// CreateObject stores an object
func (s *StorageFS) CreateObject(obj Object) error {
	err := s.CreateBucket(obj.BucketName)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(obj)
	if err != nil {
		return err
	}
	return ioutil.WriteFile(filepath.Join(s.rootDir, url.PathEscape(obj.BucketName), url.PathEscape(obj.Name)), encoded, 0664)
}

// ListObjects lists the objects in a given bucket with a given prefix and delimeter
func (s *StorageFS) ListObjects(bucketName string) ([]Object, error) {
	infos, err := ioutil.ReadDir(path.Join(s.rootDir, url.PathEscape(bucketName)))
	if err != nil {
		return nil, err
	}
	objects := []Object{}
	for _, info := range infos {
		unescaped, err := url.PathUnescape(info.Name())
		if err != nil {
			return nil, fmt.Errorf("failed to unescape object name %s: %s", info.Name(), err)
		}
		object, err := s.GetObject(bucketName, unescaped)
		if err != nil {
			return nil, err
		}
		objects = append(objects, object)
	}
	return objects, nil
}

// GetObject get an object by bucket and name
func (s *StorageFS) GetObject(bucketName, objectName string) (Object, error) {
	encoded, err := ioutil.ReadFile(filepath.Join(s.rootDir, url.PathEscape(bucketName), url.PathEscape(objectName)))
	if err != nil {
		return Object{}, err
	}
	var obj Object
	err = json.Unmarshal(encoded, &obj)
	if err != nil {
		return Object{}, err
	}
	obj.Name = objectName
	obj.BucketName = bucketName
	return obj, nil
}

// DeleteObject deletes an object by bucket and name
func (s *StorageFS) DeleteObject(bucketName, objectName string) error {
	if objectName == "" {
		return fmt.Errorf("can't delete object with empty name")
	}
	return os.Remove(filepath.Join(s.rootDir, url.PathEscape(bucketName), url.PathEscape(objectName)))
}
