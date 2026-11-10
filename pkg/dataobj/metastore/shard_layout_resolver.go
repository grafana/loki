package metastore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-kit/log"
	lru "github.com/hashicorp/golang-lru/v2"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/singleflight"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

const (
	// shardLayoutResolverSize is the number of shard layouts that a resolver remembers.
	shardLayoutResolverSize = 32768

	// shardLayoutOpenTimeout bounds the read of the shard layout of one index object.
	shardLayoutOpenTimeout = 30 * time.Second
)

// shardLayoutOpener reads the shard layout that an index object records for a tenant.
type shardLayoutOpener func(ctx context.Context, tenantID, indexPath string) (streams.ShardLayout, error)

type shardLayoutKey struct {
	tenantID  string
	indexPath string
}

// String returns the key as one string. A tenant ID never holds a slash.
func (k shardLayoutKey) String() string {
	return k.tenantID + "/" + k.indexPath
}

// shardLayoutResolver tells the shard layout of an index object for a tenant. A shard bucket range
// depends on it. An index object never changes, so the resolver remembers the layouts it reads and
// never reads one twice, as long as it has room.
//
// A shardLayoutResolver is safe for concurrent use.
type shardLayoutResolver struct {
	openShardLayout shardLayoutOpener
	logger          log.Logger
	known           *lru.Cache[shardLayoutKey, streams.ShardLayout]
	flights         singleflight.Group
}

func newShardLayoutResolver(openShardLayout shardLayoutOpener, logger log.Logger) *shardLayoutResolver {
	known, err := lru.New[shardLayoutKey, streams.ShardLayout](shardLayoutResolverSize)
	if err != nil {
		// The only error is a size that is not positive, and the size is a constant.
		panic("metastore: creating the shard layout cache: " + err.Error())
	}
	return &shardLayoutResolver{openShardLayout: openShardLayout, logger: logger, known: known}
}

// resolveShardLayout returns the shard layout of the index object for the tenant. It does not
// remember an error. Concurrent calls for the same index object share one read. When the context of
// a call ends, the call returns the cause of the context and does not stop the read. A panic in the
// read becomes an error. A read that takes longer than shardLayoutOpenTimeout fails with an error
// that does not wrap context.DeadlineExceeded.
func (r *shardLayoutResolver) resolveShardLayout(ctx context.Context, tenantID, indexPath string) (streams.ShardLayout, error) {
	key := shardLayoutKey{tenantID: tenantID, indexPath: indexPath}
	if layout, ok := r.known.Get(key); ok {
		return layout, nil
	}

	// The read below outlives the call. A call that is already canceled must not start one: when a
	// resolution is canceled, each index object that is left would start a read that nothing waits
	// for.
	if ctx.Err() != nil {
		return streams.ShardLayout{}, context.Cause(ctx)
	}

	results := r.flights.DoChan(key.String(), func() (value any, err error) {
		// The library raises a panic of this function again on a new goroutine, and then nothing can
		// recover it. So the function recovers it itself.
		defer recoverPanicAsError(ctx, r.logger, indexPath, &err)

		// A read that ended after the check above has stored its layout, so this call needs no
		// read.
		if layout, ok := r.known.Get(key); ok {
			return layout, nil
		}

		// The read ignores the cancellation of the call that started it, because other calls can
		// wait for it. The timeout bounds a read that no caller waits for any more.
		openCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shardLayoutOpenTimeout)
		defer cancel()

		layout, err := r.openShardLayout(openCtx, tenantID, indexPath)
		if err != nil {
			// The query API turns any error that wraps context.DeadlineExceeded into a 504 that tells
			// the user to shorten the query. A shorter query does not fix a slow read, and the failure
			// would count as a query timeout. So the error names the read and does not wrap the
			// deadline error, on purpose.
			if errors.Is(openCtx.Err(), context.DeadlineExceeded) {
				return nil, fmt.Errorf("reading the shard layout of index object %q from object storage took longer than %s", indexPath, shardLayoutOpenTimeout)
			}
			return nil, err
		}
		r.known.Add(key, layout)
		return layout, nil
	})

	select {
	case <-ctx.Done():
		return streams.ShardLayout{}, context.Cause(ctx)
	case result := <-results:
		if result.Err != nil {
			return streams.ShardLayout{}, result.Err
		}
		return result.Val.(streams.ShardLayout), nil
	}
}

// shardLayoutResolution is the outcome of resolving the layout of one index object.
type shardLayoutResolution struct {
	layout streams.ShardLayout
	err    error
}

// resolveShardLayouts returns the shard layout of each index object, in the order of indexes. A
// layout that cannot be read has its error in its own slot, and the other slots stay valid.
func (r *shardLayoutResolver) resolveShardLayouts(ctx context.Context, tenantID string, indexes []IndexEntry) []shardLayoutResolution {
	resolutions := make([]shardLayoutResolution, len(indexes))

	var group errgroup.Group
	group.SetLimit(maxConcurrentSectionOpens)
	for i, index := range indexes {
		group.Go(func() error {
			resolutions[i].layout, resolutions[i].err = r.resolveShardLayout(ctx, tenantID, index.Path)
			return nil
		})
	}
	_ = group.Wait()
	return resolutions
}
