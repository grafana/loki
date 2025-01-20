// Copyright 2018 Francisco Souza. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package backend proides the backends used by fake-gcs-server.
package backend

type Conditions interface {
	ConditionsMet(activeGeneration int64) bool
}

type NoConditions struct{}

func (NoConditions) ConditionsMet(int64) bool {
	return true
}

// Storage is the generic interface for implementing the backend storage of the
// server.
type Storage interface {
	CreateBucket(name string, bucketAttrs BucketAttrs) error
	ListBuckets() ([]Bucket, error)
	GetBucket(name string) (Bucket, error)
	UpdateBucket(name string, attrsToUpdate BucketAttrs) error
	DeleteBucket(name string) error
	CreateObject(obj StreamingObject, conditions Conditions) (StreamingObject, error)
	ListObjects(bucketName string, prefix string, versions bool) ([]ObjectAttrs, error)
	GetObject(bucketName, objectName string) (StreamingObject, error)
	GetObjectWithGeneration(bucketName, objectName string, generation int64) (StreamingObject, error)
	DeleteObject(bucketName, objectName string) error
	PatchObject(bucketName, objectName string, attrsToUpdate ObjectAttrs) (StreamingObject, error)
	UpdateObject(bucketName, objectName string, attrsToUpdate ObjectAttrs) (StreamingObject, error)
	ComposeObject(bucketName string, objectNames []string, destinationName string, metadata map[string]string, contentType string, contentDisposition string, contentLanguage string) (StreamingObject, error)
	DeleteAllFiles() error
}

type Error string

func (e Error) Error() string { return string(e) }

const (
	BucketNotFound     = Error("bucket not found")
	BucketNotEmpty     = Error("bucket must be empty prior to deletion")
	PreConditionFailed = Error("Precondition failed")
)
