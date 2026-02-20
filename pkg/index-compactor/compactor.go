package indexcompactor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/services"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
)

type Compactor struct {
	services.Service

	cfg      Config
	readBkt  objstore.BucketReader
	writeBkt objstore.Bucket
	logger   log.Logger
}

func NewCompactor(cfg Config, bucket objstore.Bucket, logger log.Logger) (*Compactor, error) {
	readBkt := objstore.NewPrefixedBucket(bucket, "index/v0")
	writeBkt := objstore.NewPrefixedBucket(bucket, "index/v0-compacted-test")

	c := &Compactor{
		cfg:      cfg,
		readBkt:  readBkt,
		writeBkt: writeBkt,
		logger:   logger,
	}
	c.Service = services.NewBasicService(c.starting, c.running, c.stopping).WithName("index-compactor")
	return c, nil
}

func (c *Compactor) starting(ctx context.Context) error {
	level.Info(c.logger).Log("msg", "index compactor starting")

	go func() {
		err := c.compactOnce(ctx)
		if err != nil {
			level.Error(c.logger).Log("msg", "initial compacting index objects failed", "err", err)
			panic(err)
		}

		ticker := time.NewTicker(c.cfg.CompactionInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := c.compactOnce(ctx); err != nil {
					level.Error(c.logger).Log("msg", "compaction failed", "err", err)
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return nil
}

func (c *Compactor) running(ctx context.Context) error {
	// Block until stopped.
	<-ctx.Done()
	return nil
}

func (c *Compactor) stopping(_ error) error {
	level.Info(c.logger).Log("msg", "index compactor stopping")
	return nil
}

func (c *Compactor) compactOnce(ctx context.Context) error {
	now := time.Now().UTC()
	start := now.Add(-31 * 24 * time.Hour)

	paths, err := c.discoverPaths(ctx, start, now)
	if err != nil {
		return fmt.Errorf("discovering index objects: %w", err)
	}
	if len(paths) == 0 {
		level.Info(c.logger).Log("msg", "no index objects found to compact")
		return nil
	}

	level.Info(c.logger).Log("msg", "discovered index objects", "count", len(paths))

	return mergeIndexObjects(ctx, c.logger, c.readBkt, c.writeBkt, paths, c.cfg)
}

// discoverPaths reads TOC entries from the bucket for the given time range
// and returns a deduplicated sorted list of index object paths.
func (c *Compactor) discoverPaths(ctx context.Context, start, end time.Time) ([]string, error) {
	prefix := metastore.TocPrefix
	tocPaths := tocPathsForRange(prefix, start, end)

	level.Debug(c.logger).Log("msg", "computed TOC paths", "count", len(tocPaths), "start", start.Format(time.RFC3339), "end", end.Format(time.RFC3339))

	seen := make(map[string]struct{})
	for _, tp := range tocPaths {
		obj, err := readSmallObject(ctx, c.readBkt, tp)
		if err != nil {
			if c.readBkt.IsObjNotFoundErr(err) {
				level.Debug(c.logger).Log("msg", "TOC not found, skipping", "path", tp)
				continue
			}
			return nil, fmt.Errorf("reading TOC %s: %w", tp, err)
		}

		for result := range indexpointers.Iter(ctx, obj) {
			ptr, err := result.Value()
			if err != nil {
				return nil, fmt.Errorf("iterating TOC %s: %w", tp, err)
			}
			seen[ptr.Path] = struct{}{}
		}
	}

	paths := make([]string, 0, len(seen))
	for p := range seen {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths, nil
}

func tocPathsForRange(prefix string, start, end time.Time) []string {
	minWindow := start.Truncate(tocWindowSize).UTC()
	maxWindow := end.Truncate(tocWindowSize).UTC()

	var paths []string
	for w := minWindow; !w.After(maxWindow); w = w.Add(tocWindowSize) {
		name := strings.ReplaceAll(w.UTC().Format(time.RFC3339), ":", "_")
		paths = append(paths, prefix+name+".toc")
	}
	return paths
}

func readSmallObject(ctx context.Context, bucket objstore.BucketReader, path string) (*dataobj.Object, error) {
	r, err := bucket.Get(ctx, path)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return dataobj.FromReaderAt(bytes.NewReader(data), int64(len(data)))
}
