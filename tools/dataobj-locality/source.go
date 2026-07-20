package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"go.uber.org/atomic"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/common/model"
	"github.com/thanos-io/objstore"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/loki"
	"github.com/grafana/loki/v3/pkg/storage/bucket"
	lokicfg "github.com/grafana/loki/v3/pkg/util/cfg"
)

// sectionSource yields postings sections to the collector. Implementations
// exist for object storage and local directories. fn may be called
// concurrently. sectionIdx is the raw index of the section within the index
// object (position in obj.Sections()), consistent with how SectionIndex is
// used for logs sections in postings.Row.
type sectionSource interface {
	each(ctx context.Context, fn func(sec *dataobj.Section, objPath string, sectionIdx int64) error) error
}

// indexObjectSource enumerates tenant-owned postings sections from index
// objects referenced by the metastore's Tables of Contents over [from, to].
// It backs both the object-storage and local-directory sources; only the
// underlying bucket differs.
type indexObjectSource struct {
	rawBucket    objstore.Bucket
	metastoreCfg metastore.Config
	tenant       string
	from, to     time.Time
	logger       log.Logger
	// concurrency bounds how many index objects are opened and scanned in
	// parallel. Defaults to 8 if unset (see [indexObjectSource.each]).
	concurrency int
}

func (s *indexObjectSource) each(ctx context.Context, fn func(sec *dataobj.Section, objPath string, sectionIdx int64) error) error {
	indexes, err := s.getIndexes(ctx)
	if err != nil {
		return fmt.Errorf("listing index objects: %w", err)
	}

	total := len(indexes)
	level.Info(s.logger).Log("msg", "index objects found", "count", total)

	// The metastore hands back index-pointer paths relative to the
	// IndexStoragePrefix view, so direct object reads must use the same view.
	idxBucket := s.rawBucket
	if p := s.metastoreCfg.IndexStoragePrefix; p != "" {
		idxBucket = objstore.NewPrefixedBucket(s.rawBucket, p)
	}

	var done atomic.Int64
	g, gCtx := errgroup.WithContext(ctx)
	g.SetLimit(s.effectiveConcurrency())

	for _, entry := range indexes {
		g.Go(func() error {
			n := done.Add(1)
			// Log the first object, the last, and every 50th as a heartbeat.
			if n == 1 || n%50 == 0 || n == int64(total) {
				level.Info(s.logger).Log("msg", "processing index object", "n", n, "total", total, "path", entry.Path)
			}

			obj, err := dataobj.FromBucket(gCtx, idxBucket, entry.Path, 0)
			if err != nil {
				return fmt.Errorf("opening index object %s: %w", entry.Path, err)
			}
			for i, sec := range obj.Sections() {
				if !postings.CheckSection(sec) || (s.tenant != "" && sec.Tenant != s.tenant) {
					continue
				}
				if err := fn(sec, entry.Path, int64(i)); err != nil {
					return err
				}
			}
			return nil
		})
	}
	return g.Wait()
}

func (s *indexObjectSource) getIndexes(ctx context.Context) ([]metastore.IndexEntry, error) {
	if s.tenant == "" {
		return s.getAllTenantIndexes(ctx)
	}

	ctx = user.InjectOrgID(ctx, s.tenant)
	// metastore.NewObjectMetastore applies IndexStoragePrefix internally, so it
	// receives the raw (un-index-prefixed) bucket.
	store := metastore.NewObjectMetastore(
		s.rawBucket,
		s.metastoreCfg,
		s.logger,
		metastore.NewObjectMetastoreMetrics(nil),
	)
	resp, err := store.GetIndexes(ctx, metastore.GetIndexesRequest{Start: s.from, End: s.to})
	if err != nil {
		return nil, err
	}
	return resp.Indexes, nil
}

// getAllTenantIndexes reads every tenant section in the metastore ToCs. The
// regular metastore API requires an org ID and deliberately filters ToC
// sections to that tenant, so it cannot service serve's all-tenant mode.
func (s *indexObjectSource) getAllTenantIndexes(ctx context.Context) ([]metastore.IndexEntry, error) {
	bucket := s.indexBucket()
	indexes := make(map[string]metastore.IndexEntry)
	var indexesMu sync.Mutex

	g, gCtx := errgroup.WithContext(ctx)
	g.SetLimit(s.effectiveConcurrency())
	for tocPath := range metastore.IterTableOfContentsPaths(s.from, s.to) {
		g.Go(func() error {
			obj, err := dataobj.FromBucket(gCtx, bucket, tocPath, 0)
			if err != nil {
				if bucket.IsObjNotFoundErr(err) {
					return nil
				}
				return fmt.Errorf("opening metastore object %s: %w", tocPath, err)
			}

			var reader indexpointers.RowReader
			defer reader.Close()
			buf := make([]indexpointers.IndexPointer, 1024)
			for _, section := range obj.Sections().Filter(indexpointers.CheckSection) {
				sec, err := indexpointers.Open(gCtx, section)
				if err != nil {
					return fmt.Errorf("opening index pointers in %s: %w", tocPath, err)
				}
				reader.Reset(sec)
				if err := reader.SetPredicate(indexpointers.TimeRangeRowPredicate{Start: s.from, End: s.to}); err != nil {
					return fmt.Errorf("setting time predicate for %s: %w", tocPath, err)
				}
				if err := reader.Open(gCtx); err != nil {
					return fmt.Errorf("opening index pointer reader for %s: %w", tocPath, err)
				}
				for {
					n, readErr := reader.Read(gCtx, buf)
					indexesMu.Lock()
					for _, pointer := range buf[:n] {
						entry := metastore.IndexEntry{Path: pointer.Path, Start: pointer.StartTs, End: pointer.EndTs}
						if existing, ok := indexes[pointer.Path]; ok {
							if existing.Start.Before(entry.Start) {
								entry.Start = existing.Start
							}
							if existing.End.After(entry.End) {
								entry.End = existing.End
							}
						}
						indexes[pointer.Path] = entry
					}
					indexesMu.Unlock()
					if errors.Is(readErr, io.EOF) {
						break
					}
					if readErr != nil {
						return fmt.Errorf("reading index pointers from %s: %w", tocPath, readErr)
					}
				}
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}

	result := make([]metastore.IndexEntry, 0, len(indexes))
	for _, entry := range indexes {
		result = append(result, entry)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Path < result[j].Path
	})
	return result, nil
}

func (s *indexObjectSource) effectiveConcurrency() int {
	if s.concurrency < 1 {
		return 8
	}
	return s.concurrency
}

// indexBucket returns a bucket view whose paths match the index pointers
// returned by the metastore.
func (s *indexObjectSource) indexBucket() objstore.Bucket {
	if p := s.metastoreCfg.IndexStoragePrefix; p != "" {
		return objstore.NewPrefixedBucket(s.rawBucket, p)
	}
	return s.rawBucket
}

// compactedIndexMarker is the path segment that distinguishes a compacted
// index object from an uncompacted (per-partition) one. The compactor writes
// merged indexes under "indexes/tenants/<tenant>/<sha2>/<sha-rest>" (see
// indexMergePath.Build in pkg/engine/compactor/output_paths.go), whereas the
// per-partition indexer writes directly under "indexes/<sha2>/<sha-rest>" (see
// ObjectKey in pkg/dataobj/index/indexer.go).
const compactedIndexMarker = "indexes/tenants/"

// isCompactedIndexPath reports whether an index object path was produced by the
// compactor rather than the per-partition indexer.
func isCompactedIndexPath(path string) bool {
	return strings.Contains(path, compactedIndexMarker)
}

// buildBucketFromLokiConfig loads a full Loki config file and derives the
// object-store bucket and metastore config from it, mirroring the
// getDataObjBucket + compaction-worker wiring in pkg/loki/modules.go.
func buildBucketFromLokiConfig(ctx context.Context, configFile string, expandEnv bool, logger log.Logger) (objstore.Bucket, metastore.Config, error) {
	level.Info(logger).Log("msg", "loading loki config", "file", configFile)

	args := []string{"-config.file=" + configFile}
	if expandEnv {
		args = append(args, "-config.expand-env=true")
	}
	var c loki.ConfigWrapper
	if err := lokicfg.DynamicUnmarshal(&c, args, flag.NewFlagSet("loki-config", flag.ContinueOnError)); err != nil {
		return nil, metastore.Config{}, fmt.Errorf("loading loki config %s: %w", configFile, err)
	}

	// Derive the backend from the schema, mirroring getDataObjBucket.
	schema, err := c.SchemaConfig.SchemaForTime(model.Now())
	if err != nil {
		return nil, metastore.Config{}, fmt.Errorf("resolving schema for current time: %w", err)
	}

	// Apply named-store resolution before creating the client.
	objCfg := c.StorageConfig.ObjectStore
	backend := schema.ObjectType
	if st, ok := objCfg.NamedStores.LookupStoreType(schema.ObjectType); ok {
		backend = st
		if err := objCfg.NamedStores.OverrideConfig(&objCfg.Config, schema.ObjectType); err != nil {
			return nil, metastore.Config{}, fmt.Errorf("resolving named store %q: %w", schema.ObjectType, err)
		}
	}

	mCfg := c.DataObj.Metastore
	level.Info(logger).Log(
		"msg", "config loaded",
		"backend", backend,
		"dataobj_prefix", c.DataObj.StorageBucketPrefix,
		"index_prefix", mCfg.IndexStoragePrefix,
	)

	ib, err := bucket.NewClient(ctx, backend, objCfg.Config, "dataobj-locality", logger)
	if err != nil {
		return nil, metastore.Config{}, fmt.Errorf("creating bucket client: %w", err)
	}

	// Apply the dataobj-level namespace prefix so paths resolve identically to
	// what the running engine uses. Use a plain Bucket interface so the
	// NewPrefixedBucket wrapper (which is not InstrumentedBucket) is accepted.
	var b objstore.Bucket = ib
	if p := c.DataObj.StorageBucketPrefix; p != "" {
		b = objstore.NewPrefixedBucket(b, p)
	}

	return b, mCfg, nil
}
