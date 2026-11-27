package compactortest

import (
	"context"
	"io"
	"sync"

	"github.com/thanos-io/objstore"
)

// Bucket is an in-memory bucket whose GetAndReplace is atomic.
//
// objstore.InMemBucket reads the object before it takes its lock. Two
// concurrent calls can then read the same version, and the later write drops
// the change of the earlier one. Object storage providers make the call
// atomic with a conditional write, and compaction relies on that when jobs
// swap the same ToC at once.
type Bucket struct {
	*objstore.InMemBucket

	// replaceMu serializes the calls to GetAndReplace.
	replaceMu sync.Mutex
}

// NewBucket returns an empty Bucket.
func NewBucket() *Bucket {
	return &Bucket{InMemBucket: objstore.NewInMemBucket()}
}

// GetAndReplace replaces the object at name with the result of f, which gets
// the current object or nil. No other call of GetAndReplace runs at the same
// time.
func (b *Bucket) GetAndReplace(ctx context.Context, name string, f func(io.ReadCloser) (io.ReadCloser, error)) error {
	b.replaceMu.Lock()
	defer b.replaceMu.Unlock()
	return b.InMemBucket.GetAndReplace(ctx, name, f)
}
