package bucketcache

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
)

// Values of the kind label of the metrics.
const (
	kindLogs  = "logs"
	kindIndex = "index"
)

// Backend is the cache of one kind of object.
type Backend struct {
	Config     Config
	Subranges  cache.Cache
	Attributes AttributesCache
}

// Router serves GetRange and Attributes from the cache of the kind of the object. It passes every
// other call, and every object that no cache serves, to the bucket it wraps.
//
// Each kind has its own cache. When the two backends are separate servers, a high eviction rate in
// one kind does not evict the other.
type Router struct {
	objstore.Bucket

	logs  *cachedBucket
	index *cachedBucket
}

// New returns a Router that caches the reads of log objects in logs, and of index objects in index. A
// nil Backend leaves its kind uncached. bucketID scopes the keys to one bucket. The Router does not stop
// the caches.
func New(bucket objstore.Bucket, logs, index *Backend, bucketID string, reg prometheus.Registerer) (*Router, error) {
	if bucket == nil {
		return nil, errors.New("the bucket cache needs a bucket")
	}
	if logs == nil && index == nil {
		return nil, errors.New("the bucket cache needs a backend for log objects or index objects")
	}

	r := &Router{Bucket: bucket}
	var err error
	if logs != nil {
		if r.logs, err = newCachedBucket(bucket, logs, kindLogs, bucketID, reg); err != nil {
			return nil, fmt.Errorf("creating the cache of log objects: %w", err)
		}
	}
	if index != nil {
		if r.index, err = newCachedBucket(bucket, index, kindIndex, bucketID, reg); err != nil {
			return nil, fmt.Errorf("creating the cache of index objects: %w", err)
		}
	}
	return r, nil
}

func (r *Router) GetRange(ctx context.Context, name string, offset, length int64) (io.ReadCloser, error) {
	if c := r.cacheOf(name); c != nil {
		return c.GetRange(ctx, name, offset, length)
	}
	return r.Bucket.GetRange(ctx, name, offset, length)
}

func (r *Router) Attributes(ctx context.Context, name string) (objstore.ObjectAttributes, error) {
	if c := r.cacheOf(name); c != nil {
		return c.Attributes(ctx, name)
	}
	return r.Bucket.Attributes(ctx, name)
}

// cacheOf returns the cache that serves the object, or nil when no cache serves it.
func (r *Router) cacheOf(name string) *cachedBucket {
	switch kindOf(name) {
	case kindLogs:
		return r.logs
	case kindIndex:
		return r.index
	}
	return nil
}

// kindOf returns the kind of a cacheable object: a log object or an index object. It returns an empty
// string for any other object. The content of a cacheable object never changes, and the cache keys carry
// no version of an object. The table of contents does change, so no cache may serve it.
func kindOf(name string) string {
	switch {
	case strings.HasPrefix(name, "objects/"):
		return kindLogs
	case strings.HasPrefix(name, metastore.IndexStoragePrefix+"/indexes/"):
		return kindIndex
	}
	return ""
}
