// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

// Package shipper detects directories on the local file system and uploads
// them to a block storage.
package shipper

import (
	"context"
	"encoding/json"
	"io/ioutil"
	"math"
	"os"
	"path"
	"path/filepath"
	"sort"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/oklog/ulid"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/tsdb"
	"github.com/prometheus/prometheus/tsdb/fileutil"
	"github.com/thanos-io/thanos/pkg/block"
	"github.com/thanos-io/thanos/pkg/block/metadata"
	"github.com/thanos-io/thanos/pkg/objstore"
	"github.com/thanos-io/thanos/pkg/runutil"
)

type metrics struct {
	dirSyncs          prometheus.Counter
	dirSyncFailures   prometheus.Counter
	uploads           prometheus.Counter
	uploadFailures    prometheus.Counter
	uploadedCompacted prometheus.Gauge
}

func newMetrics(reg prometheus.Registerer, uploadCompacted bool) *metrics {
	var m metrics

	m.dirSyncs = promauto.With(reg).NewCounter(prometheus.CounterOpts{
		Name: "thanos_shipper_dir_syncs_total",
		Help: "Total number of dir syncs",
	})
	m.dirSyncFailures = promauto.With(reg).NewCounter(prometheus.CounterOpts{
		Name: "thanos_shipper_dir_sync_failures_total",
		Help: "Total number of failed dir syncs",
	})
	m.uploads = promauto.With(reg).NewCounter(prometheus.CounterOpts{
		Name: "thanos_shipper_uploads_total",
		Help: "Total number of uploaded blocks",
	})
	m.uploadFailures = promauto.With(reg).NewCounter(prometheus.CounterOpts{
		Name: "thanos_shipper_upload_failures_total",
		Help: "Total number of block upload failures",
	})
	uploadCompactedGaugeOpts := prometheus.GaugeOpts{
		Name: "thanos_shipper_upload_compacted_done",
		Help: "If 1 it means shipper uploaded all compacted blocks from the filesystem.",
	}
	if uploadCompacted {
		m.uploadedCompacted = promauto.With(reg).NewGauge(uploadCompactedGaugeOpts)
	} else {
		m.uploadedCompacted = promauto.With(nil).NewGauge(uploadCompactedGaugeOpts)
	}
	return &m
}

// Shipper watches a directory for matching files and directories and uploads
// them to a remote data store.
type Shipper struct {
	logger  log.Logger
	dir     string
	metrics *metrics
	bucket  objstore.Bucket
	labels  func() labels.Labels
	source  metadata.SourceType

	uploadCompacted        bool
	allowOutOfOrderUploads bool
	hashFunc               metadata.HashFunc
}

// New creates a new shipper that detects new TSDB blocks in dir and uploads them to
// remote if necessary. It attaches the Thanos metadata section in each meta JSON file.
// If uploadCompacted is enabled, it also uploads compacted blocks which are already in filesystem.
func New(
	logger log.Logger,
	r prometheus.Registerer,
	dir string,
	bucket objstore.Bucket,
	lbls func() labels.Labels,
	source metadata.SourceType,
	uploadCompacted bool,
	allowOutOfOrderUploads bool,
	hashFunc metadata.HashFunc,
) *Shipper {
	if logger == nil {
		logger = log.NewNopLogger()
	}
	if lbls == nil {
		lbls = func() labels.Labels { return nil }
	}

	return &Shipper{
		logger:                 logger,
		dir:                    dir,
		bucket:                 bucket,
		labels:                 lbls,
		metrics:                newMetrics(r, uploadCompacted),
		source:                 source,
		allowOutOfOrderUploads: allowOutOfOrderUploads,
		uploadCompacted:        uploadCompacted,
		hashFunc:               hashFunc,
	}
}

// Timestamps returns the minimum timestamp for which data is available and the highest timestamp
// of blocks that were successfully uploaded.
func (s *Shipper) Timestamps() (minTime, maxSyncTime int64, err error) {
	meta, err := ReadMetaFile(s.dir)
	if err != nil {
		return 0, 0, errors.Wrap(err, "read shipper meta file")
	}
	// Build a map of blocks we already uploaded.
	hasUploaded := make(map[ulid.ULID]struct{}, len(meta.Uploaded))
	for _, id := range meta.Uploaded {
		hasUploaded[id] = struct{}{}
	}

	minTime = math.MaxInt64
	maxSyncTime = math.MinInt64

	metas, err := s.blockMetasFromOldest()
	if err != nil {
		return 0, 0, err
	}
	for _, m := range metas {
		if m.MinTime < minTime {
			minTime = m.MinTime
		}
		if _, ok := hasUploaded[m.ULID]; ok && m.MaxTime > maxSyncTime {
			maxSyncTime = m.MaxTime
		}
	}

	if minTime == math.MaxInt64 {
		// No block yet found. We cannot assume any min block size so propagate 0 minTime.
		minTime = 0
	}
	return minTime, maxSyncTime, nil
}

type lazyOverlapChecker struct {
	synced bool
	logger log.Logger
	bucket objstore.Bucket
	labels func() labels.Labels

	metas       []tsdb.BlockMeta
	lookupMetas map[ulid.ULID]struct{}
}

func newLazyOverlapChecker(logger log.Logger, bucket objstore.Bucket, labels func() labels.Labels) *lazyOverlapChecker {
	return &lazyOverlapChecker{
		logger: logger,
		bucket: bucket,
		labels: labels,

		lookupMetas: map[ulid.ULID]struct{}{},
	}
}

func (c *lazyOverlapChecker) sync(ctx context.Context) error {
	if err := c.bucket.Iter(ctx, "", func(path string) error {
		id, ok := block.IsBlockDir(path)
		if !ok {
			return nil
		}

		m, err := block.DownloadMeta(ctx, c.logger, c.bucket, id)
		if err != nil {
			return err
		}

		if !labels.Equal(labels.FromMap(m.Thanos.Labels), c.labels()) {
			return nil
		}

		c.metas = append(c.metas, m.BlockMeta)
		c.lookupMetas[m.ULID] = struct{}{}
		return nil

	}); err != nil {
		return errors.Wrap(err, "get all block meta.")
	}

	c.synced = true
	return nil
}

func (c *lazyOverlapChecker) IsOverlapping(ctx context.Context, newMeta tsdb.BlockMeta) error {
	if !c.synced {
		level.Info(c.logger).Log("msg", "gathering all existing blocks from the remote bucket for check", "id", newMeta.ULID.String())
		if err := c.sync(ctx); err != nil {
			return err
		}
	}

	// TODO(bwplotka) so confusing! we need to sort it first. Add comment to TSDB code.
	metas := append([]tsdb.BlockMeta{newMeta}, c.metas...)
	sort.Slice(metas, func(i, j int) bool {
		return metas[i].MinTime < metas[j].MinTime
	})
	if o := tsdb.OverlappingBlocks(metas); len(o) > 0 {
		// TODO(bwplotka): Consider checking if overlaps relates to block in concern?
		return errors.Errorf("shipping compacted block %s is blocked; overlap spotted: %s", newMeta.ULID, o.String())
	}
	return nil
}

// Sync performs a single synchronization, which ensures all non-compacted local blocks have been uploaded
// to the object bucket once.
//
// If uploaded.
//
// It is not concurrency-safe, however it is compactor-safe (running concurrently with compactor is ok).
func (s *Shipper) Sync(ctx context.Context) (uploaded int, err error) {
	meta, err := ReadMetaFile(s.dir)
	if err != nil {
		// If we encounter any error, proceed with an empty meta file and overwrite it later.
		// The meta file is only used to avoid unnecessary bucket.Exists call,
		// which are properly handled by the system if their occur anyway.
		if !os.IsNotExist(err) {
			level.Warn(s.logger).Log("msg", "reading meta file failed, will override it", "err", err)
		}
		meta = &Meta{Version: MetaVersion1}
	}

	// Build a map of blocks we already uploaded.
	hasUploaded := make(map[ulid.ULID]struct{}, len(meta.Uploaded))
	for _, id := range meta.Uploaded {
		hasUploaded[id] = struct{}{}
	}

	// Reset the uploaded slice so we can rebuild it only with blocks that still exist locally.
	meta.Uploaded = nil

	var (
		checker    = newLazyOverlapChecker(s.logger, s.bucket, s.labels)
		uploadErrs int
	)

	metas, err := s.blockMetasFromOldest()
	if err != nil {
		return 0, err
	}
	for _, m := range metas {
		// Do not sync a block if we already uploaded or ignored it. If it's no longer found in the bucket,
		// it was generally removed by the compaction process.
		if _, uploaded := hasUploaded[m.ULID]; uploaded {
			meta.Uploaded = append(meta.Uploaded, m.ULID)
			continue
		}

		if m.Stats.NumSamples == 0 {
			// Ignore empty blocks.
			level.Debug(s.logger).Log("msg", "ignoring empty block", "block", m.ULID)
			continue
		}

		// We only ship of the first compacted block level as normal flow.
		if m.Compaction.Level > 1 {
			if !s.uploadCompacted {
				continue
			}
		}

		// Check against bucket if the meta file for this block exists.
		ok, err := s.bucket.Exists(ctx, path.Join(m.ULID.String(), block.MetaFilename))
		if err != nil {
			return 0, errors.Wrap(err, "check exists")
		}
		if ok {
			meta.Uploaded = append(meta.Uploaded, m.ULID)
			continue
		}

		if m.Compaction.Level > 1 {
			if err := checker.IsOverlapping(ctx, m.BlockMeta); err != nil {
				if !s.allowOutOfOrderUploads {
					return 0, errors.Errorf("Found overlap or error during sync, cannot upload compacted block, details: %v", err)
				}
				level.Error(s.logger).Log("msg", "found overlap or error during sync, cannot upload compacted block", "err", err)
				uploadErrs++
				continue
			}
		}

		if err := s.upload(ctx, m); err != nil {
			if !s.allowOutOfOrderUploads {
				return 0, errors.Wrapf(err, "upload %v", m.ULID)
			}

			// No error returned, just log line. This is because we want other blocks to be uploaded even
			// though this one failed. It will be retried on second Sync iteration.
			level.Error(s.logger).Log("msg", "shipping failed", "block", m.ULID, "err", err)
			uploadErrs++
			continue
		}
		meta.Uploaded = append(meta.Uploaded, m.ULID)
		uploaded++
		s.metrics.uploads.Inc()
	}
	if err := WriteMetaFile(s.logger, s.dir, meta); err != nil {
		level.Warn(s.logger).Log("msg", "updating meta file failed", "err", err)
	}

	s.metrics.dirSyncs.Inc()
	if uploadErrs > 0 {
		s.metrics.uploadFailures.Add(float64(uploadErrs))
		return uploaded, errors.Errorf("failed to sync %v blocks", uploadErrs)
	}

	if s.uploadCompacted {
		s.metrics.uploadedCompacted.Set(1)
	}
	return uploaded, nil
}

// sync uploads the block if not exists in remote storage.
// TODO(khyatisoneji): Double check if block does not have deletion-mark.json for some reason, otherwise log it or return error.
func (s *Shipper) upload(ctx context.Context, meta *metadata.Meta) error {
	level.Info(s.logger).Log("msg", "upload new block", "id", meta.ULID)

	// We hard-link the files into a temporary upload directory so we are not affected
	// by other operations happening against the TSDB directory.
	updir := filepath.Join(s.dir, "thanos", "upload", meta.ULID.String())

	// Remove updir just in case.
	if err := os.RemoveAll(updir); err != nil {
		return errors.Wrap(err, "clean upload directory")
	}
	if err := os.MkdirAll(updir, 0777); err != nil {
		return errors.Wrap(err, "create upload dir")
	}
	defer func() {
		if err := os.RemoveAll(updir); err != nil {
			level.Error(s.logger).Log("msg", "failed to clean upload directory", "err", err)
		}
	}()

	dir := filepath.Join(s.dir, meta.ULID.String())
	if err := hardlinkBlock(dir, updir); err != nil {
		return errors.Wrap(err, "hard link block")
	}
	// Attach current labels and write a new meta file with Thanos extensions.
	if lset := s.labels(); lset != nil {
		meta.Thanos.Labels = lset.Map()
	}
	meta.Thanos.Source = s.source
	meta.Thanos.SegmentFiles = block.GetSegmentFiles(updir)
	if err := meta.WriteToDir(s.logger, updir); err != nil {
		return errors.Wrap(err, "write meta file")
	}
	return block.Upload(ctx, s.logger, s.bucket, updir, s.hashFunc)
}

// blockMetasFromOldest returns the block meta of each block found in dir
// sorted by minTime asc.
func (s *Shipper) blockMetasFromOldest() (metas []*metadata.Meta, _ error) {
	fis, err := ioutil.ReadDir(s.dir)
	if err != nil {
		return nil, errors.Wrap(err, "read dir")
	}
	names := make([]string, 0, len(fis))
	for _, fi := range fis {
		names = append(names, fi.Name())
	}
	for _, n := range names {
		if _, ok := block.IsBlockDir(n); !ok {
			continue
		}
		dir := filepath.Join(s.dir, n)

		fi, err := os.Stat(dir)
		if err != nil {
			return nil, errors.Wrapf(err, "stat block %v", dir)
		}
		if !fi.IsDir() {
			continue
		}
		m, err := metadata.ReadFromDir(dir)
		if err != nil {
			return nil, errors.Wrapf(err, "read metadata for block %v", dir)
		}
		metas = append(metas, m)
	}
	sort.Slice(metas, func(i, j int) bool {
		return metas[i].BlockMeta.MinTime < metas[j].BlockMeta.MinTime
	})
	return metas, nil
}

func hardlinkBlock(src, dst string) error {
	chunkDir := filepath.Join(dst, block.ChunksDirname)

	if err := os.MkdirAll(chunkDir, 0777); err != nil {
		return errors.Wrap(err, "create chunks dir")
	}

	fis, err := ioutil.ReadDir(filepath.Join(src, block.ChunksDirname))
	if err != nil {
		return errors.Wrap(err, "read chunk dir")
	}
	files := make([]string, 0, len(fis))
	for _, fi := range fis {
		files = append(files, fi.Name())
	}
	for i, fn := range files {
		files[i] = filepath.Join(block.ChunksDirname, fn)
	}
	files = append(files, block.MetaFilename, block.IndexFilename)

	for _, fn := range files {
		if err := os.Link(filepath.Join(src, fn), filepath.Join(dst, fn)); err != nil {
			return errors.Wrapf(err, "hard link file %s", fn)
		}
	}
	return nil
}

// Meta defines the format thanos.shipper.json file that the shipper places in the data directory.
type Meta struct {
	Version  int         `json:"version"`
	Uploaded []ulid.ULID `json:"uploaded"`
}

const (
	// MetaFilename is the known JSON filename for meta information.
	MetaFilename = "thanos.shipper.json"

	// MetaVersion1 represents 1 version of meta.
	MetaVersion1 = 1
)

// WriteMetaFile writes the given meta into <dir>/thanos.shipper.json.
func WriteMetaFile(logger log.Logger, dir string, meta *Meta) error {
	// Make any changes to the file appear atomic.
	path := filepath.Join(dir, MetaFilename)
	tmp := path + ".tmp"

	f, err := os.Create(tmp)
	if err != nil {
		return err
	}

	enc := json.NewEncoder(f)
	enc.SetIndent("", "\t")

	if err := enc.Encode(meta); err != nil {
		runutil.CloseWithLogOnErr(logger, f, "write meta file close")
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return renameFile(logger, tmp, path)
}

// ReadMetaFile reads the given meta from <dir>/thanos.shipper.json.
func ReadMetaFile(dir string) (*Meta, error) {
	b, err := ioutil.ReadFile(filepath.Join(dir, MetaFilename))
	if err != nil {
		return nil, err
	}
	var m Meta

	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	if m.Version != MetaVersion1 {
		return nil, errors.Errorf("unexpected meta file version %d", m.Version)
	}

	return &m, nil
}

func renameFile(logger log.Logger, from, to string) error {
	if err := os.RemoveAll(to); err != nil {
		return err
	}
	if err := os.Rename(from, to); err != nil {
		return err
	}

	// Directory was renamed; sync parent dir to persist rename.
	pdir, err := fileutil.OpenDir(filepath.Dir(to))
	if err != nil {
		return err
	}

	if err = fileutil.Fdatasync(pdir); err != nil {
		runutil.CloseWithLogOnErr(logger, pdir, "rename file dir close")
		return err
	}
	return pdir.Close()
}
