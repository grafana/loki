// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package block

import (
	"context"
	"io"
	"os"
	"path/filepath"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/tsdb"
	"github.com/prometheus/prometheus/tsdb/chunks"
	tsdb_errors "github.com/prometheus/prometheus/tsdb/errors"
	"github.com/prometheus/prometheus/tsdb/fileutil"
	"github.com/prometheus/prometheus/tsdb/index"
)

// Reader is like tsdb.BlockReader but without tombstones and size methods.
type Reader interface {
	// Index returns an IndexReader over the block's data.
	Index() (tsdb.IndexReader, error)

	// Chunks returns a ChunkReader over the block's data.
	Chunks() (tsdb.ChunkReader, error)

	// Meta returns block metadata file.
	Meta() tsdb.BlockMeta
}

// SeriesWriter is interface for writing series into one or multiple Blocks.
// Statistics has to be counted by implementation.
type SeriesWriter interface {
	tsdb.IndexWriter
	tsdb.ChunkWriter
}

// Writer is interface for creating block(s).
type Writer interface {
	SeriesWriter

	Flush() (tsdb.BlockStats, error)
}

type DiskWriter struct {
	statsGatheringSeriesWriter

	bTmp, bDir string
	logger     log.Logger
	closers    []io.Closer
}

const tmpForCreationBlockDirSuffix = ".tmp-for-creation"

// NewDiskWriter allows to write single TSDB block to disk and returns statistics.
// Destination block directory has to exists.
func NewDiskWriter(ctx context.Context, logger log.Logger, bDir string) (_ *DiskWriter, err error) {
	bTmp := bDir + tmpForCreationBlockDirSuffix

	d := &DiskWriter{
		bTmp:   bTmp,
		bDir:   bDir,
		logger: logger,
	}
	defer func() {
		if err != nil {
			err = tsdb_errors.NewMulti(err, tsdb_errors.CloseAll(d.closers)).Err()
			if err := os.RemoveAll(bTmp); err != nil {
				level.Error(logger).Log("msg", "removed tmp folder after failed compaction", "err", err.Error())
			}
		}
	}()

	if err = os.RemoveAll(bTmp); err != nil {
		return nil, err
	}
	if err = os.MkdirAll(bTmp, 0777); err != nil {
		return nil, err
	}

	chunkw, err := chunks.NewWriter(filepath.Join(bTmp, ChunksDirname))
	if err != nil {
		return nil, errors.Wrap(err, "open chunk writer")
	}
	d.closers = append(d.closers, chunkw)

	// TODO(bwplotka): Setup instrumentedChunkWriter if we want to upstream this code.

	indexw, err := index.NewWriter(ctx, filepath.Join(bTmp, IndexFilename))
	if err != nil {
		return nil, errors.Wrap(err, "open index writer")
	}
	d.closers = append(d.closers, indexw)
	d.statsGatheringSeriesWriter = statsGatheringSeriesWriter{iw: indexw, cw: chunkw}
	return d, nil
}

func (d *DiskWriter) Flush() (_ tsdb.BlockStats, err error) {
	defer func() {
		if err != nil {
			err = tsdb_errors.NewMulti(err, tsdb_errors.CloseAll(d.closers)).Err()
			if err := os.RemoveAll(d.bTmp); err != nil {
				level.Error(d.logger).Log("msg", "removed tmp folder failed after block(s) write", "err", err.Error())
			}
		}
	}()
	df, err := fileutil.OpenDir(d.bTmp)
	if err != nil {
		return tsdb.BlockStats{}, errors.Wrap(err, "open temporary block dir")
	}
	defer func() {
		if df != nil {
			err = tsdb_errors.NewMulti(err, df.Close()).Err()
		}
	}()

	if err := df.Sync(); err != nil {
		return tsdb.BlockStats{}, errors.Wrap(err, "sync temporary dir file")
	}

	// Close temp dir before rename block dir (for windows platform).
	if err = df.Close(); err != nil {
		return tsdb.BlockStats{}, errors.Wrap(err, "close temporary dir")
	}
	df = nil

	if err := tsdb_errors.CloseAll(d.closers); err != nil {
		d.closers = nil
		return tsdb.BlockStats{}, err
	}
	d.closers = nil

	// Block files successfully written, make them visible by moving files from tmp dir.
	if err := fileutil.Replace(filepath.Join(d.bTmp, IndexFilename), filepath.Join(d.bDir, IndexFilename)); err != nil {
		return tsdb.BlockStats{}, errors.Wrap(err, "replace index file")
	}
	if err := fileutil.Replace(filepath.Join(d.bTmp, ChunksDirname), filepath.Join(d.bDir, ChunksDirname)); err != nil {
		return tsdb.BlockStats{}, errors.Wrap(err, "replace chunks dir")
	}
	return d.stats, nil
}

type statsGatheringSeriesWriter struct {
	iw tsdb.IndexWriter
	cw tsdb.ChunkWriter

	stats   tsdb.BlockStats
	symbols int64
}

func (s *statsGatheringSeriesWriter) AddSymbol(sym string) error {
	if err := s.iw.AddSymbol(sym); err != nil {
		return err
	}
	s.symbols++
	return nil
}

func (s *statsGatheringSeriesWriter) AddSeries(ref uint64, l labels.Labels, chks ...chunks.Meta) error {
	if err := s.iw.AddSeries(ref, l, chks...); err != nil {
		return err
	}
	s.stats.NumSeries++
	return nil
}

func (s *statsGatheringSeriesWriter) WriteChunks(chks ...chunks.Meta) error {
	if err := s.cw.WriteChunks(chks...); err != nil {
		return err
	}
	s.stats.NumChunks += uint64(len(chks))
	for _, chk := range chks {
		s.stats.NumSamples += uint64(chk.Chunk.NumSamples())
	}
	return nil
}

func (s statsGatheringSeriesWriter) Close() error {
	return tsdb_errors.NewMulti(s.iw.Close(), s.cw.Close()).Err()
}
