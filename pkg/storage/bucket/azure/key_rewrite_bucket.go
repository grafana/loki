package azure

import (
	"context"
	"io"
	"strings"

	"github.com/thanos-io/objstore"
)

// keyRewriteBucket wraps a bucket and replaces ":" with a configured delimiter in all object keys.
type keyRewriteBucket struct {
	objstore.Bucket
	delimiter string
}

func (b *keyRewriteBucket) rewriteKey(key string) string {
	return strings.Replace(key, ":", b.delimiter, -1)
}

func (b *keyRewriteBucket) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	return b.Bucket.Get(ctx, b.rewriteKey(name))
}

func (b *keyRewriteBucket) GetRange(ctx context.Context, name string, off, length int64) (io.ReadCloser, error) {
	return b.Bucket.GetRange(ctx, b.rewriteKey(name), off, length)
}

func (b *keyRewriteBucket) Exists(ctx context.Context, name string) (bool, error) {
	return b.Bucket.Exists(ctx, b.rewriteKey(name))
}

func (b *keyRewriteBucket) Attributes(ctx context.Context, name string) (objstore.ObjectAttributes, error) {
	return b.Bucket.Attributes(ctx, b.rewriteKey(name))
}

func (b *keyRewriteBucket) Upload(ctx context.Context, name string, r io.Reader) error {
	return b.Bucket.Upload(ctx, b.rewriteKey(name), r)
}

func (b *keyRewriteBucket) Delete(ctx context.Context, name string) error {
	return b.Bucket.Delete(ctx, b.rewriteKey(name))
}
