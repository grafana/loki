package filesystem

import (
	"github.com/thanos-io/objstore"
	"github.com/thanos-io/objstore/providers/filesystem"
)

// NewBucketClient creates a new filesystem bucket client
func NewBucketClient(cfg Config) (objstore.Bucket, error) {
	return filesystem.NewBucket(cfg.Directory)
}
