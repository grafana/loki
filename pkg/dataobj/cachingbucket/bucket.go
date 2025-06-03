package cachingbucket

import (
	"context"
	"io"

	"github.com/thanos-io/objstore"
	"github.com/thanos-io/objstore/providers/filesystem"
)

type CachingBucket struct {
	objstore.Bucket
	cache objstore.Bucket
}

var _ objstore.Bucket = (*CachingBucket)(nil)

func New(bucket objstore.Bucket, cacheDir string) *CachingBucket {
	cache, err := filesystem.NewBucket(cacheDir)
	if err != nil {
		panic(err)
	}
	return &CachingBucket{
		Bucket: bucket,
		cache:  cache,
	}
}

func (b *CachingBucket) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	handle, err := b.cache.Get(ctx, name)
	if err == nil {
		return handle, nil
	}

	handle, err = b.Bucket.Get(ctx, name)
	if err != nil {
		return nil, err
	}

	err = b.cache.Upload(ctx, name, handle)
	if err != nil {
		return nil, err
	}

	return b.cache.Get(ctx, name)
}

func (b *CachingBucket) Delete(ctx context.Context, name string) error {
	err := b.cache.Delete(ctx, name)
	if err != nil {
		return err
	}
	return b.Bucket.Delete(ctx, name)
}

func (b *CachingBucket) Attributes(ctx context.Context, name string) (objstore.ObjectAttributes, error) {
	handle, err := b.cache.Attributes(ctx, name)
	if err == nil {
		return handle, nil
	}

	return b.Bucket.Attributes(ctx, name)
}
