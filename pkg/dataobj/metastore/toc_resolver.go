package metastore

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/scalar"
	"github.com/go-kit/log"
	"github.com/thanos-io/objstore"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
)

// TableOfContentsResolver returns the index-object entries recorded in the given ToC window paths
// (tablePaths) whose [Start, End] overlaps the inclusive query range [start, end], scoped to the request
// context's tenant. The result is deduped and sorted by path. ObjectMetastore.GetIndexes delegates to it
// after computing the window paths, so the resolution strategy (read on demand vs. serve from a warmed
// cache) is pluggable.
type TableOfContentsResolver interface {
	GetIndexes(ctx context.Context, tablePaths []string, start, end time.Time) ([]IndexEntry, error)
}

// TableOfContentsLazyResolver reads each ToC on demand: one full-object download per path, decoded in
// memory, filtered to the request context's tenant and [start, end]. It is the default resolver and the
// fallback for the warm resolver.
type TableOfContentsLazyResolver struct {
	bucket objstore.Bucket
	logger log.Logger
}

// NewTableOfContentsLazyResolver builds the default, read-on-demand resolver over bucket.
func NewTableOfContentsLazyResolver(bucket objstore.Bucket, logger log.Logger) *TableOfContentsLazyResolver {
	if logger == nil {
		logger = log.NewNopLogger()
	}
	return &TableOfContentsLazyResolver{bucket: bucket, logger: logger}
}

func (r *TableOfContentsLazyResolver) GetIndexes(ctx context.Context, tablePaths []string, start, end time.Time) ([]IndexEntry, error) {
	objects := make([][]IndexEntry, len(tablePaths))
	g, ctx := errgroup.WithContext(ctx)

	sStart := scalar.NewTimestampScalar(arrow.Timestamp(start.UnixNano()), arrow.FixedWidthTypes.Timestamp_ns)
	sEnd := scalar.NewTimestampScalar(arrow.Timestamp(end.UnixNano()), arrow.FixedWidthTypes.Timestamp_ns)

	for i, path := range tablePaths {
		g.Go(func() error {
			var err error
			objects[i], err = r.listObjects(ctx, path, sStart, sEnd)
			// A missing ToC means the window has no data yet; ignore it rather than failing the query.
			if err != nil && !r.bucket.IsObjNotFoundErr(err) {
				return fmt.Errorf("listing objects from metastore %s: %w", path, err)
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	return dedupeAndSortEntries(objects), nil
}

// listObjects downloads one ToC object in full and returns the current tenant's index entries overlapping
// [sStart, sEnd].
func (r *TableOfContentsLazyResolver) listObjects(ctx context.Context, path string, sStart, sEnd *scalar.Timestamp) ([]IndexEntry, error) {
	var buf bytes.Buffer
	objectReader, err := r.bucket.Get(ctx, path)
	if err != nil {
		return nil, err
	}
	defer objectReader.Close()

	n, err := buf.ReadFrom(objectReader)
	if err != nil {
		return nil, fmt.Errorf("reading metastore object: %w", err)
	}
	object, err := dataobj.FromReaderAt(bytes.NewReader(buf.Bytes()), n)
	if err != nil {
		return nil, fmt.Errorf("getting object from reader: %w", err)
	}

	var entries []IndexEntry
	err = forEachIndexPointer(ctx, object, sStart, sEnd, func(indexPointer indexpointers.IndexPointer) {
		entries = append(entries, IndexEntry{
			Path:  indexPointer.Path,
			Start: indexPointer.StartTs,
			End:   indexPointer.EndTs,
		})
	})
	if err != nil {
		return nil, err
	}
	return entries, nil
}
