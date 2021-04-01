package bucketindex

import (
	"bytes"
	"context"
	"io"
	"io/ioutil"
	"path"

	"github.com/oklog/ulid"
	"github.com/thanos-io/thanos/pkg/block"
	"github.com/thanos-io/thanos/pkg/block/metadata"
	"github.com/thanos-io/thanos/pkg/objstore"
)

// globalMarkersBucket is a bucket client which stores markers (eg. block deletion marks) in a per-tenant
// global location too.
type globalMarkersBucket struct {
	parent objstore.Bucket
}

// BucketWithGlobalMarkers wraps the input bucket into a bucket which also keeps track of markers
// in the global markers location.
func BucketWithGlobalMarkers(b objstore.Bucket) objstore.Bucket {
	return &globalMarkersBucket{
		parent: b,
	}
}

// Upload implements objstore.Bucket.
func (b *globalMarkersBucket) Upload(ctx context.Context, name string, r io.Reader) error {
	blockID, ok := b.isBlockDeletionMark(name)
	if !ok {
		return b.parent.Upload(ctx, name, r)
	}

	// Read the marker.
	body, err := ioutil.ReadAll(r)
	if err != nil {
		return err
	}

	// Upload it to the original location.
	if err := b.parent.Upload(ctx, name, bytes.NewBuffer(body)); err != nil {
		return err
	}

	// Upload it to the global markers location too.
	globalMarkPath := path.Clean(path.Join(path.Dir(name), "../", BlockDeletionMarkFilepath(blockID)))
	return b.parent.Upload(ctx, globalMarkPath, bytes.NewBuffer(body))
}

// Delete implements objstore.Bucket.
func (b *globalMarkersBucket) Delete(ctx context.Context, name string) error {
	// Call the parent.
	if err := b.parent.Delete(ctx, name); err != nil {
		return err
	}

	// Delete the marker in the global markers location too.
	if blockID, ok := b.isBlockDeletionMark(name); ok {
		globalMarkPath := path.Clean(path.Join(path.Dir(name), "../", BlockDeletionMarkFilepath(blockID)))
		if err := b.parent.Delete(ctx, globalMarkPath); err != nil {
			if !b.parent.IsObjNotFoundErr(err) {
				return err
			}
		}
	}

	return nil
}

// Name implements objstore.Bucket.
func (b *globalMarkersBucket) Name() string {
	return b.parent.Name()
}

// Close implements objstore.Bucket.
func (b *globalMarkersBucket) Close() error {
	return b.parent.Close()
}

// Iter implements objstore.Bucket.
func (b *globalMarkersBucket) Iter(ctx context.Context, dir string, f func(string) error, options ...objstore.IterOption) error {
	return b.parent.Iter(ctx, dir, f, options...)
}

// Get implements objstore.Bucket.
func (b *globalMarkersBucket) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	return b.parent.Get(ctx, name)
}

// GetRange implements objstore.Bucket.
func (b *globalMarkersBucket) GetRange(ctx context.Context, name string, off, length int64) (io.ReadCloser, error) {
	return b.parent.GetRange(ctx, name, off, length)
}

// Exists implements objstore.Bucket.
func (b *globalMarkersBucket) Exists(ctx context.Context, name string) (bool, error) {
	return b.parent.Exists(ctx, name)
}

// IsObjNotFoundErr implements objstore.Bucket.
func (b *globalMarkersBucket) IsObjNotFoundErr(err error) bool {
	return b.parent.IsObjNotFoundErr(err)
}

// Attributes implements objstore.Bucket.
func (b *globalMarkersBucket) Attributes(ctx context.Context, name string) (objstore.ObjectAttributes, error) {
	return b.parent.Attributes(ctx, name)
}

// WithExpectedErrs implements objstore.InstrumentedBucket.
func (b *globalMarkersBucket) WithExpectedErrs(fn objstore.IsOpFailureExpectedFunc) objstore.Bucket {
	if ib, ok := b.parent.(objstore.InstrumentedBucket); ok {
		return ib.WithExpectedErrs(fn)
	}

	return b
}

// ReaderWithExpectedErrs implements objstore.InstrumentedBucketReader.
func (b *globalMarkersBucket) ReaderWithExpectedErrs(fn objstore.IsOpFailureExpectedFunc) objstore.BucketReader {
	if ib, ok := b.parent.(objstore.InstrumentedBucketReader); ok {
		return ib.ReaderWithExpectedErrs(fn)
	}

	return b
}

func (b *globalMarkersBucket) isBlockDeletionMark(name string) (ulid.ULID, bool) {
	if path.Base(name) != metadata.DeletionMarkFilename {
		return ulid.ULID{}, false
	}

	// Parse the block ID in the path. If there's not block ID, then it's not the per-block
	// deletion mark.
	return block.IsBlockDir(path.Dir(name))
}
