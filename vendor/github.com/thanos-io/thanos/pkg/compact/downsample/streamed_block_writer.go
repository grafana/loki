// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package downsample

import (
	"context"
	"io"
	"path/filepath"
	"strings"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/tsdb"
	"github.com/prometheus/prometheus/tsdb/chunks"
	"github.com/prometheus/prometheus/tsdb/fileutil"
	"github.com/prometheus/prometheus/tsdb/index"
	"github.com/thanos-io/thanos/pkg/block"
	"github.com/thanos-io/thanos/pkg/block/metadata"
	"github.com/thanos-io/thanos/pkg/errutil"
	"github.com/thanos-io/thanos/pkg/runutil"
)

// streamedBlockWriter writes downsampled blocks to a new data block. Implemented to save memory consumption
// by writing chunks data right into the files, omitting keeping them in-memory. Index and meta data should be
// sealed afterwards, when there aren't more series to process.
type streamedBlockWriter struct {
	blockDir       string
	finalized      bool // Set to true, if Close was called.
	logger         log.Logger
	ignoreFinalize bool // If true Close does not finalize block due to internal error.
	meta           metadata.Meta
	totalChunks    uint64
	totalSamples   uint64

	chunkWriter tsdb.ChunkWriter
	indexWriter tsdb.IndexWriter
	indexReader tsdb.IndexReader
	closers     []io.Closer

	seriesRefs uint64 // postings is a current posting position.
}

// NewStreamedBlockWriter returns streamedBlockWriter instance, it's not concurrency safe.
// Caller is responsible to Close all io.Closers by calling the Close when downsampling is done.
// In case if error happens outside of the StreamedBlockWriter during the processing,
// index and meta files will be written anyway, so the caller is always responsible for removing block directory with
// a garbage on error.
// This approach simplifies StreamedBlockWriter interface, which is a best trade-off taking into account the error is an
// exception, not a general case.
func NewStreamedBlockWriter(
	blockDir string,
	indexReader tsdb.IndexReader,
	logger log.Logger,
	originMeta metadata.Meta,
) (w *streamedBlockWriter, err error) {
	closers := make([]io.Closer, 0, 2)

	// We should close any opened Closer up to an error.
	defer func() {
		if err != nil {
			var merr errutil.MultiError
			merr.Add(err)
			for _, cl := range closers {
				merr.Add(cl.Close())
			}
			err = merr.Err()
		}
	}()

	chunkWriter, err := chunks.NewWriter(filepath.Join(blockDir, block.ChunksDirname))
	if err != nil {
		return nil, errors.Wrap(err, "create chunk writer in streamedBlockWriter")
	}
	closers = append(closers, chunkWriter)

	indexWriter, err := index.NewWriter(context.TODO(), filepath.Join(blockDir, block.IndexFilename))
	if err != nil {
		return nil, errors.Wrap(err, "open index writer in streamedBlockWriter")
	}
	closers = append(closers, indexWriter)

	symbols := indexReader.Symbols()
	for symbols.Next() {
		if err = indexWriter.AddSymbol(symbols.At()); err != nil {
			return nil, errors.Wrap(err, "add symbols")
		}
	}
	if err := symbols.Err(); err != nil {
		return nil, errors.Wrap(err, "read symbols")
	}

	return &streamedBlockWriter{
		logger:      logger,
		blockDir:    blockDir,
		indexReader: indexReader,
		indexWriter: indexWriter,
		chunkWriter: chunkWriter,
		meta:        originMeta,
		closers:     closers,
	}, nil
}

// WriteSeries writes chunks data to the chunkWriter, writes lset and chunks MetasFetcher to indexWrites and adds label sets to
// labelsValues sets and memPostings to be written on the finalize state in the end of downsampling process.
func (w *streamedBlockWriter) WriteSeries(lset labels.Labels, chunks []chunks.Meta) error {
	if w.finalized || w.ignoreFinalize {
		return errors.New("series can't be added, writers has been closed or internal error happened")
	}

	if len(chunks) == 0 {
		level.Warn(w.logger).Log("msg", "empty chunks happened, skip series", "series", strings.ReplaceAll(lset.String(), "\"", "'"))
		return nil
	}

	if err := w.chunkWriter.WriteChunks(chunks...); err != nil {
		w.ignoreFinalize = true
		return errors.Wrap(err, "add chunks")
	}

	if err := w.indexWriter.AddSeries(w.seriesRefs, lset, chunks...); err != nil {
		w.ignoreFinalize = true
		return errors.Wrap(err, "add series")
	}

	w.seriesRefs++

	w.totalChunks += uint64(len(chunks))
	for i := range chunks {
		w.totalSamples += uint64(chunks[i].Chunk.NumSamples())
	}

	return nil
}

// Close calls finalizer to complete index and meta files and closes all io.CLoser writers.
// Idempotent.
func (w *streamedBlockWriter) Close() error {
	if w.finalized {
		return nil
	}
	w.finalized = true

	merr := errutil.MultiError{}

	if w.ignoreFinalize {
		// Close open file descriptors anyway.
		for _, cl := range w.closers {
			merr.Add(cl.Close())
		}
		return merr.Err()
	}

	// Finalize saves prepared index and metadata to corresponding files.

	for _, cl := range w.closers {
		merr.Add(cl.Close())
	}

	if err := w.writeMetaFile(); err != nil {
		return errors.Wrap(err, "write meta meta")
	}

	if err := w.syncDir(); err != nil {
		return errors.Wrap(err, "sync blockDir")
	}

	if err := merr.Err(); err != nil {
		return errors.Wrap(err, "finalize")
	}

	// No error, claim success.

	level.Info(w.logger).Log(
		"msg", "finalized downsampled block",
		"mint", w.meta.MinTime,
		"maxt", w.meta.MaxTime,
		"ulid", w.meta.ULID,
		"resolution", w.meta.Thanos.Downsample.Resolution,
	)
	return nil
}

// syncDir syncs blockDir on disk.
func (w *streamedBlockWriter) syncDir() (err error) {
	df, err := fileutil.OpenDir(w.blockDir)
	if err != nil {
		return errors.Wrap(err, "open temporary block blockDir")
	}

	defer runutil.CloseWithErrCapture(&err, df, "close temporary block blockDir")

	if err := fileutil.Fdatasync(df); err != nil {
		return errors.Wrap(err, "sync temporary blockDir")
	}

	return nil
}

// writeMetaFile writes meta file.
func (w *streamedBlockWriter) writeMetaFile() error {
	w.meta.Version = metadata.TSDBVersion1
	w.meta.Thanos.Source = metadata.CompactorSource
	w.meta.Thanos.SegmentFiles = block.GetSegmentFiles(w.blockDir)
	w.meta.Stats.NumChunks = w.totalChunks
	w.meta.Stats.NumSamples = w.totalSamples
	w.meta.Stats.NumSeries = w.seriesRefs

	return w.meta.WriteToDir(w.logger, w.blockDir)
}
