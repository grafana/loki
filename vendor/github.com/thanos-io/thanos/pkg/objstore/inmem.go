// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package objstore

import (
	"bytes"
	"context"
	"io"
	"io/ioutil"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"
)

var errNotFound = errors.New("inmem: object not found")

// InMemBucket implements the objstore.Bucket interfaces against local memory.
// Methods from Bucket interface are thread-safe. Objects are assumed to be immutable.
type InMemBucket struct {
	mtx     sync.RWMutex
	objects map[string][]byte
	attrs   map[string]ObjectAttributes
}

// NewInMemBucket returns a new in memory Bucket.
// NOTE: Returned bucket is just a naive in memory bucket implementation. For test use cases only.
func NewInMemBucket() *InMemBucket {
	return &InMemBucket{
		objects: map[string][]byte{},
		attrs:   map[string]ObjectAttributes{},
	}
}

// Objects returns internally stored objects.
// NOTE: For assert purposes.
func (b *InMemBucket) Objects() map[string][]byte {
	return b.objects
}

// Iter calls f for each entry in the given directory. The argument to f is the full
// object name including the prefix of the inspected directory.
func (b *InMemBucket) Iter(_ context.Context, dir string, f func(string) error, options ...IterOption) error {
	unique := map[string]struct{}{}
	params := ApplyIterOptions(options...)

	var dirPartsCount int
	dirParts := strings.SplitAfter(dir, DirDelim)
	for _, p := range dirParts {
		if p == "" {
			continue
		}
		dirPartsCount++
	}

	b.mtx.RLock()
	for filename := range b.objects {
		if !strings.HasPrefix(filename, dir) || dir == filename {
			continue
		}

		if params.Recursive {
			// Any object matching the prefix should be included.
			unique[filename] = struct{}{}
			continue
		}

		parts := strings.SplitAfter(filename, DirDelim)
		unique[strings.Join(parts[:dirPartsCount+1], "")] = struct{}{}
	}
	b.mtx.RUnlock()

	var keys []string
	for n := range unique {
		keys = append(keys, n)
	}
	sort.Slice(keys, func(i, j int) bool {
		if strings.HasSuffix(keys[i], DirDelim) && strings.HasSuffix(keys[j], DirDelim) {
			return strings.Compare(keys[i], keys[j]) < 0
		}
		if strings.HasSuffix(keys[i], DirDelim) {
			return false
		}
		if strings.HasSuffix(keys[j], DirDelim) {
			return true
		}

		return strings.Compare(keys[i], keys[j]) < 0
	})

	for _, k := range keys {
		if err := f(k); err != nil {
			return err
		}
	}
	return nil
}

// Get returns a reader for the given object name.
func (b *InMemBucket) Get(_ context.Context, name string) (io.ReadCloser, error) {
	if name == "" {
		return nil, errors.New("inmem: object name is empty")
	}

	b.mtx.RLock()
	file, ok := b.objects[name]
	b.mtx.RUnlock()
	if !ok {
		return nil, errNotFound
	}

	return ioutil.NopCloser(bytes.NewReader(file)), nil
}

// GetRange returns a new range reader for the given object name and range.
func (b *InMemBucket) GetRange(_ context.Context, name string, off, length int64) (io.ReadCloser, error) {
	if name == "" {
		return nil, errors.New("inmem: object name is empty")
	}

	b.mtx.RLock()
	file, ok := b.objects[name]
	b.mtx.RUnlock()
	if !ok {
		return nil, errNotFound
	}

	if int64(len(file)) < off {
		return ioutil.NopCloser(bytes.NewReader(nil)), nil
	}

	if length == -1 {
		return ioutil.NopCloser(bytes.NewReader(file[off:])), nil
	}

	if length <= 0 {
		return ioutil.NopCloser(bytes.NewReader(nil)), errors.New("length cannot be smaller or equal 0")
	}

	if int64(len(file)) <= off+length {
		// Just return maximum of what we have.
		length = int64(len(file)) - off
	}

	return ioutil.NopCloser(bytes.NewReader(file[off : off+length])), nil
}

// Exists checks if the given directory exists in memory.
func (b *InMemBucket) Exists(_ context.Context, name string) (bool, error) {
	b.mtx.RLock()
	defer b.mtx.RUnlock()
	_, ok := b.objects[name]
	return ok, nil
}

// Attributes returns information about the specified object.
func (b *InMemBucket) Attributes(_ context.Context, name string) (ObjectAttributes, error) {
	b.mtx.RLock()
	attrs, ok := b.attrs[name]
	b.mtx.RUnlock()
	if !ok {
		return ObjectAttributes{}, errNotFound
	}
	return attrs, nil
}

// Upload writes the file specified in src to into the memory.
func (b *InMemBucket) Upload(_ context.Context, name string, r io.Reader) error {
	b.mtx.Lock()
	defer b.mtx.Unlock()
	body, err := ioutil.ReadAll(r)
	if err != nil {
		return err
	}
	b.objects[name] = body
	b.attrs[name] = ObjectAttributes{
		Size:         int64(len(body)),
		LastModified: time.Now(),
	}
	return nil
}

// Delete removes all data prefixed with the dir.
func (b *InMemBucket) Delete(_ context.Context, name string) error {
	b.mtx.Lock()
	defer b.mtx.Unlock()
	if _, ok := b.objects[name]; !ok {
		return errNotFound
	}
	delete(b.objects, name)
	delete(b.attrs, name)
	return nil
}

// IsObjNotFoundErr returns true if error means that object is not found. Relevant to Get operations.
func (b *InMemBucket) IsObjNotFoundErr(err error) bool {
	return errors.Is(err, errNotFound)
}

func (b *InMemBucket) Close() error { return nil }

// Name returns the bucket name.
func (b *InMemBucket) Name() string {
	return "inmem"
}
