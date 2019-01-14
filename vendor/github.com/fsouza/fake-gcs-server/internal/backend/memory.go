package backend

import (
	"errors"
	"fmt"
	"sync"
)

// StorageMemory is an implementation of the backend storage that stores data in memory
type StorageMemory struct {
	buckets map[string][]Object
	mtx     sync.RWMutex
}

// NewStorageMemory creates an instance of StorageMemory
func NewStorageMemory(objects []Object) Storage {
	s := &StorageMemory{
		buckets: make(map[string][]Object),
	}
	for _, o := range objects {
		s.buckets[o.BucketName] = append(s.buckets[o.BucketName], o)
	}
	return s
}

// CreateBucket creates a bucket
func (s *StorageMemory) CreateBucket(name string) error {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	if _, ok := s.buckets[name]; !ok {
		s.buckets[name] = nil
	}
	return nil
}

// ListBuckets lists buckets
func (s *StorageMemory) ListBuckets() ([]string, error) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	buckets := []string{}
	for bucket := range s.buckets {
		buckets = append(buckets, bucket)
	}
	return buckets, nil
}

// GetBucket checks if a bucket exists
func (s *StorageMemory) GetBucket(name string) error {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	if _, ok := s.buckets[name]; !ok {
		return fmt.Errorf("no bucket named %s", name)
	}
	return nil
}

// CreateObject stores an object
func (s *StorageMemory) CreateObject(obj Object) error {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	index := s.findObject(obj)
	if index < 0 {
		s.buckets[obj.BucketName] = append(s.buckets[obj.BucketName], obj)
	} else {
		s.buckets[obj.BucketName][index] = obj
	}
	return nil
}

// findObject looks for an object in its bucket and return the index where it
// was found, or -1 if the object doesn't exist.
//
// It doesn't lock the mutex, callers must lock the mutex before calling this
// method.
func (s *StorageMemory) findObject(obj Object) int {
	for i, o := range s.buckets[obj.BucketName] {
		if obj.ID() == o.ID() {
			return i
		}
	}
	return -1
}

// ListObjects lists the objects in a given bucket with a given prefix and delimeter
func (s *StorageMemory) ListObjects(bucketName string) ([]Object, error) {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	objects, ok := s.buckets[bucketName]
	if !ok {
		return nil, errors.New("bucket not found")
	}
	return objects, nil
}

// GetObject get an object by bucket and name
func (s *StorageMemory) GetObject(bucketName, objectName string) (Object, error) {
	obj := Object{BucketName: bucketName, Name: objectName}
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	index := s.findObject(obj)
	if index < 0 {
		return obj, errors.New("object not found")
	}
	return s.buckets[bucketName][index], nil
}

// DeleteObject deletes an object by bucket and name
func (s *StorageMemory) DeleteObject(bucketName, objectName string) error {
	obj := Object{BucketName: bucketName, Name: objectName}
	index := s.findObject(obj)
	if index < 0 {
		return fmt.Errorf("no such object in bucket %s: %s", bucketName, objectName)
	}
	bucket := s.buckets[obj.BucketName]
	bucket[index] = bucket[len(bucket)-1]
	s.buckets[obj.BucketName] = bucket[:len(bucket)-1]
	return nil
}
