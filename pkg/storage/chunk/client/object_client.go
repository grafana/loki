package client

import (
	"context"
	"io"
	"time"
)

type ObjectAttributes struct {
	Size int64
}

// ObjectClient is used to store arbitrary data in Object Store (S3/GCS/Azure/...)
type ObjectClient interface {
	ObjectExists(ctx context.Context, objectKey string) (bool, error)
	GetAttributes(ctx context.Context, objectKey string) (ObjectAttributes, error)

	PutObject(ctx context.Context, objectKey string, object io.Reader) error
	// NOTE: The consumer of GetObject should always call the Close method when it is done reading which otherwise could cause a resource leak.
	GetObject(ctx context.Context, objectKey string) (io.ReadCloser, int64, error)
	GetObjectRange(ctx context.Context, objectKey string, off, length int64) (io.ReadCloser, error)

	// List objects with given prefix.
	//
	// If delimiter is empty, all objects are returned, even if they are in nested in "subdirectories".
	// If delimiter is not empty, it is used to compute common prefixes ("subdirectories"),
	// and objects containing delimiter in the name will not be returned in the result.
	//
	// For example, if the prefix is "notes/" and the delimiter is a slash (/) as in "notes/summer/july", the common prefix is "notes/summer/".
	// Common prefixes will always end with passed delimiter.
	//
	// Keys of returned storage objects have given prefix.
	List(ctx context.Context, prefix string, delimiter string) ([]StorageObject, []StorageCommonPrefix, error)
	DeleteObject(ctx context.Context, objectKey string) error
	IsObjectNotFoundErr(err error) bool
	IsRetryableErr(err error) bool
	Stop()
}

// StorageObject represents an object being stored in an Object Store
type StorageObject struct {
	Key        string
	ModifiedAt time.Time
}

// StorageCommonPrefix represents a common prefix aka a synthetic directory in Object Store.
// It is guaranteed to always end with delimiter passed to List method.
type StorageCommonPrefix string
