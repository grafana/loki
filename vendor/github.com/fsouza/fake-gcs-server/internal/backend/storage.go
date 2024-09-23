// Copyright 2018 Francisco Souza. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package backend

// Storage is the generic interface for implementing the backend storage of the server
type Storage interface {
	CreateBucket(name string) error
	ListBuckets() ([]string, error)
	GetBucket(name string) error
	CreateObject(obj Object) error
	ListObjects(bucketName string) ([]Object, error)
	GetObject(bucketName, objectName string) (Object, error)
	DeleteObject(bucketName, objectName string) error
}
