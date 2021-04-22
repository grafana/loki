// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

// Package block contains common functionality for interacting with TSDB blocks
// in the context of Thanos.
package block

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/oklog/ulid"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/thanos-io/thanos/pkg/block/metadata"
	"github.com/thanos-io/thanos/pkg/objstore"
	"github.com/thanos-io/thanos/pkg/runutil"
)

const (
	// MetaFilename is the known JSON filename for meta information.
	MetaFilename = "meta.json"
	// IndexFilename is the known index file for block index.
	IndexFilename = "index"
	// IndexHeaderFilename is the canonical name for binary index header file that stores essential information.
	IndexHeaderFilename = "index-header"
	// ChunksDirname is the known dir name for chunks with compressed samples.
	ChunksDirname = "chunks"

	// DebugMetas is a directory for debug meta files that happen in the past. Useful for debugging.
	DebugMetas = "debug/metas"
)

// Download downloads directory that is mean to be block directory. If any of the files
// have a hash calculated in the meta file and it matches with what is in the destination path then
// we do not download it. We always re-download the meta file.
func Download(ctx context.Context, logger log.Logger, bucket objstore.Bucket, id ulid.ULID, dst string) error {
	if err := os.MkdirAll(dst, 0777); err != nil {
		return errors.Wrap(err, "create dir")
	}

	if err := objstore.DownloadFile(ctx, logger, bucket, path.Join(id.String(), MetaFilename), path.Join(dst, MetaFilename)); err != nil {
		return err
	}
	m, err := metadata.ReadFromDir(dst)
	if err != nil {
		return errors.Wrapf(err, "reading meta from %s", dst)
	}

	ignoredPaths := []string{MetaFilename}
	for _, fl := range m.Thanos.Files {
		if fl.Hash == nil || fl.Hash.Func == metadata.NoneFunc || fl.RelPath == "" {
			continue
		}
		actualHash, err := metadata.CalculateHash(filepath.Join(dst, fl.RelPath), fl.Hash.Func, logger)
		if err != nil {
			level.Info(logger).Log("msg", "failed to calculate hash when downloading; re-downloading", "relPath", fl.RelPath, "err", err)
			continue
		}

		if fl.Hash.Equal(&actualHash) {
			ignoredPaths = append(ignoredPaths, fl.RelPath)
		}
	}

	if err := objstore.DownloadDir(ctx, logger, bucket, id.String(), id.String(), dst, ignoredPaths...); err != nil {
		return err
	}

	chunksDir := filepath.Join(dst, ChunksDirname)
	_, err = os.Stat(chunksDir)
	if os.IsNotExist(err) {
		// This can happen if block is empty. We cannot easily upload empty directory, so create one here.
		return os.Mkdir(chunksDir, os.ModePerm)
	}

	if err != nil {
		return errors.Wrapf(err, "stat %s", chunksDir)
	}

	return nil
}

// Upload uploads a TSDB block to the object storage. It verifies basic
// features of Thanos block.
func Upload(ctx context.Context, logger log.Logger, bkt objstore.Bucket, bdir string, hf metadata.HashFunc) error {
	return upload(ctx, logger, bkt, bdir, hf, true)
}

// UploadPromBlock uploads a TSDB block to the object storage. It assumes
// the block is used in Prometheus so it doesn't check Thanos external labels.
func UploadPromBlock(ctx context.Context, logger log.Logger, bkt objstore.Bucket, bdir string, hf metadata.HashFunc) error {
	return upload(ctx, logger, bkt, bdir, hf, false)
}

// upload uploads block from given block dir that ends with block id.
// It makes sure cleanup is done on error to avoid partial block uploads.
// TODO(bplotka): Ensure bucket operations have reasonable backoff retries.
// NOTE: Upload updates `meta.Thanos.File` section.
func upload(ctx context.Context, logger log.Logger, bkt objstore.Bucket, bdir string, hf metadata.HashFunc, checkExternalLabels bool) error {
	df, err := os.Stat(bdir)
	if err != nil {
		return err
	}
	if !df.IsDir() {
		return errors.Errorf("%s is not a directory", bdir)
	}

	// Verify dir.
	id, err := ulid.Parse(df.Name())
	if err != nil {
		return errors.Wrap(err, "not a block dir")
	}

	meta, err := metadata.ReadFromDir(bdir)
	if err != nil {
		// No meta or broken meta file.
		return errors.Wrap(err, "read meta")
	}

	if checkExternalLabels {
		if meta.Thanos.Labels == nil || len(meta.Thanos.Labels) == 0 {
			return errors.New("empty external labels are not allowed for Thanos block.")
		}
	}

	metaEncoded := strings.Builder{}
	meta.Thanos.Files, err = gatherFileStats(bdir, hf, logger)
	if err != nil {
		return errors.Wrap(err, "gather meta file stats")
	}

	if err := meta.Write(&metaEncoded); err != nil {
		return errors.Wrap(err, "encode meta file")
	}

	// TODO(yeya24): Remove this step.
	if err := bkt.Upload(ctx, path.Join(DebugMetas, fmt.Sprintf("%s.json", id)), strings.NewReader(metaEncoded.String())); err != nil {
		return cleanUp(logger, bkt, id, errors.Wrap(err, "upload debug meta file"))
	}

	if err := objstore.UploadDir(ctx, logger, bkt, path.Join(bdir, ChunksDirname), path.Join(id.String(), ChunksDirname)); err != nil {
		return cleanUp(logger, bkt, id, errors.Wrap(err, "upload chunks"))
	}

	if err := objstore.UploadFile(ctx, logger, bkt, path.Join(bdir, IndexFilename), path.Join(id.String(), IndexFilename)); err != nil {
		return cleanUp(logger, bkt, id, errors.Wrap(err, "upload index"))
	}

	// Meta.json always need to be uploaded as a last item. This will allow to assume block directories without meta file to be pending uploads.
	if err := bkt.Upload(ctx, path.Join(id.String(), MetaFilename), strings.NewReader(metaEncoded.String())); err != nil {
		// Don't call cleanUp here. Despite getting error, meta.json may have been uploaded in certain cases,
		// and even though cleanUp will not see it yet, meta.json may appear in the bucket later.
		// (Eg. S3 is known to behave this way when it returns 503 "SlowDown" error).
		// If meta.json is not uploaded, this will produce partial blocks, but such blocks will be cleaned later.
		return errors.Wrap(err, "upload meta file")
	}

	return nil
}

func cleanUp(logger log.Logger, bkt objstore.Bucket, id ulid.ULID, err error) error {
	// Cleanup the dir with an uncancelable context.
	cleanErr := Delete(context.Background(), logger, bkt, id)
	if cleanErr != nil {
		return errors.Wrapf(err, "failed to clean block after upload issue. Partial block in system. Err: %s", err.Error())
	}
	return err
}

// MarkForDeletion creates a file which stores information about when the block was marked for deletion.
func MarkForDeletion(ctx context.Context, logger log.Logger, bkt objstore.Bucket, id ulid.ULID, details string, markedForDeletion prometheus.Counter) error {
	deletionMarkFile := path.Join(id.String(), metadata.DeletionMarkFilename)
	deletionMarkExists, err := bkt.Exists(ctx, deletionMarkFile)
	if err != nil {
		return errors.Wrapf(err, "check exists %s in bucket", deletionMarkFile)
	}
	if deletionMarkExists {
		level.Warn(logger).Log("msg", "requested to mark for deletion, but file already exists; this should not happen; investigate", "err", errors.Errorf("file %s already exists in bucket", deletionMarkFile))
		return nil
	}

	deletionMark, err := json.Marshal(metadata.DeletionMark{
		ID:           id,
		DeletionTime: time.Now().Unix(),
		Version:      metadata.DeletionMarkVersion1,
		Details:      details,
	})
	if err != nil {
		return errors.Wrap(err, "json encode deletion mark")
	}

	if err := bkt.Upload(ctx, deletionMarkFile, bytes.NewBuffer(deletionMark)); err != nil {
		return errors.Wrapf(err, "upload file %s to bucket", deletionMarkFile)
	}
	markedForDeletion.Inc()
	level.Info(logger).Log("msg", "block has been marked for deletion", "block", id)
	return nil
}

// Delete removes directory that is meant to be block directory.
// NOTE: Always prefer this method for deleting blocks.
//  * We have to delete block's files in the certain order (meta.json first and deletion-mark.json last)
//  to ensure we don't end up with malformed partial blocks. Thanos system handles well partial blocks
//  only if they don't have meta.json. If meta.json is present Thanos assumes valid block.
//  * This avoids deleting empty dir (whole bucket) by mistake.
func Delete(ctx context.Context, logger log.Logger, bkt objstore.Bucket, id ulid.ULID) error {
	metaFile := path.Join(id.String(), MetaFilename)
	deletionMarkFile := path.Join(id.String(), metadata.DeletionMarkFilename)

	// Delete block meta file.
	ok, err := bkt.Exists(ctx, metaFile)
	if err != nil {
		return errors.Wrapf(err, "stat %s", metaFile)
	}

	if ok {
		if err := bkt.Delete(ctx, metaFile); err != nil {
			return errors.Wrapf(err, "delete %s", metaFile)
		}
		level.Debug(logger).Log("msg", "deleted file", "file", metaFile, "bucket", bkt.Name())
	}

	// Delete the block objects, but skip:
	// - The metaFile as we just deleted. This is required for eventual object storages (list after write).
	// - The deletionMarkFile as we'll delete it at last.
	err = deleteDirRec(ctx, logger, bkt, id.String(), func(name string) bool {
		return name == metaFile || name == deletionMarkFile
	})
	if err != nil {
		return err
	}

	// Delete block deletion mark.
	ok, err = bkt.Exists(ctx, deletionMarkFile)
	if err != nil {
		return errors.Wrapf(err, "stat %s", deletionMarkFile)
	}

	if ok {
		if err := bkt.Delete(ctx, deletionMarkFile); err != nil {
			return errors.Wrapf(err, "delete %s", deletionMarkFile)
		}
		level.Debug(logger).Log("msg", "deleted file", "file", deletionMarkFile, "bucket", bkt.Name())
	}

	return nil
}

// deleteDirRec removes all objects prefixed with dir from the bucket. It skips objects that return true for the passed keep function.
// NOTE: For objects removal use `block.Delete` strictly.
func deleteDirRec(ctx context.Context, logger log.Logger, bkt objstore.Bucket, dir string, keep func(name string) bool) error {
	return bkt.Iter(ctx, dir, func(name string) error {
		// If we hit a directory, call DeleteDir recursively.
		if strings.HasSuffix(name, objstore.DirDelim) {
			return deleteDirRec(ctx, logger, bkt, name, keep)
		}
		if keep(name) {
			return nil
		}
		if err := bkt.Delete(ctx, name); err != nil {
			return err
		}
		level.Debug(logger).Log("msg", "deleted file", "file", name, "bucket", bkt.Name())
		return nil
	})
}

// DownloadMeta downloads only meta file from bucket by block ID.
// TODO(bwplotka): Differentiate between network error & partial upload.
func DownloadMeta(ctx context.Context, logger log.Logger, bkt objstore.Bucket, id ulid.ULID) (metadata.Meta, error) {
	rc, err := bkt.Get(ctx, path.Join(id.String(), MetaFilename))
	if err != nil {
		return metadata.Meta{}, errors.Wrapf(err, "meta.json bkt get for %s", id.String())
	}
	defer runutil.CloseWithLogOnErr(logger, rc, "download meta bucket client")

	var m metadata.Meta

	obj, err := ioutil.ReadAll(rc)
	if err != nil {
		return metadata.Meta{}, errors.Wrapf(err, "read meta.json for block %s", id.String())
	}

	if err = json.Unmarshal(obj, &m); err != nil {
		return metadata.Meta{}, errors.Wrapf(err, "unmarshal meta.json for block %s", id.String())
	}

	return m, nil
}

func IsBlockDir(path string) (id ulid.ULID, ok bool) {
	id, err := ulid.Parse(filepath.Base(path))
	return id, err == nil
}

// GetSegmentFiles returns list of segment files for given block. Paths are relative to the chunks directory.
// In case of errors, nil is returned.
func GetSegmentFiles(blockDir string) []string {
	files, err := ioutil.ReadDir(filepath.Join(blockDir, ChunksDirname))
	if err != nil {
		return nil
	}

	// ReadDir returns files in sorted order already.
	var result []string
	for _, f := range files {
		result = append(result, f.Name())
	}
	return result
}

// TODO(bwplotka): Gather stats when dirctly uploading files.
func gatherFileStats(blockDir string, hf metadata.HashFunc, logger log.Logger) (res []metadata.File, _ error) {
	files, err := ioutil.ReadDir(filepath.Join(blockDir, ChunksDirname))
	if err != nil {
		return nil, errors.Wrapf(err, "read dir %v", filepath.Join(blockDir, ChunksDirname))
	}
	for _, f := range files {
		mf := metadata.File{
			RelPath:   filepath.Join(ChunksDirname, f.Name()),
			SizeBytes: f.Size(),
		}
		if hf != metadata.NoneFunc && !f.IsDir() {
			h, err := metadata.CalculateHash(filepath.Join(blockDir, ChunksDirname, f.Name()), hf, logger)
			if err != nil {
				return nil, errors.Wrapf(err, "calculate hash %v", filepath.Join(ChunksDirname, f.Name()))
			}
			mf.Hash = &h
		}
		res = append(res, mf)
	}

	indexFile, err := os.Stat(filepath.Join(blockDir, IndexFilename))
	if err != nil {
		return nil, errors.Wrapf(err, "stat %v", filepath.Join(blockDir, IndexFilename))
	}
	mf := metadata.File{
		RelPath:   indexFile.Name(),
		SizeBytes: indexFile.Size(),
	}
	if hf != metadata.NoneFunc {
		h, err := metadata.CalculateHash(filepath.Join(blockDir, IndexFilename), hf, logger)
		if err != nil {
			return nil, errors.Wrapf(err, "calculate hash %v", indexFile.Name())
		}
		mf.Hash = &h
	}
	res = append(res, mf)

	metaFile, err := os.Stat(filepath.Join(blockDir, MetaFilename))
	if err != nil {
		return nil, errors.Wrapf(err, "stat %v", filepath.Join(blockDir, MetaFilename))
	}
	res = append(res, metadata.File{RelPath: metaFile.Name()})

	sort.Slice(res, func(i, j int) bool {
		return strings.Compare(res[i].RelPath, res[j].RelPath) < 0
	})
	// TODO(bwplotka): Add optional files like tombstones?
	return res, err
}

// MarkForNoCompact creates a file which marks block to be not compacted.
func MarkForNoCompact(ctx context.Context, logger log.Logger, bkt objstore.Bucket, id ulid.ULID, reason metadata.NoCompactReason, details string, markedForNoCompact prometheus.Counter) error {
	m := path.Join(id.String(), metadata.NoCompactMarkFilename)
	noCompactMarkExists, err := bkt.Exists(ctx, m)
	if err != nil {
		return errors.Wrapf(err, "check exists %s in bucket", m)
	}
	if noCompactMarkExists {
		level.Warn(logger).Log("msg", "requested to mark for no compaction, but file already exists; this should not happen; investigate", "err", errors.Errorf("file %s already exists in bucket", m))
		return nil
	}

	noCompactMark, err := json.Marshal(metadata.NoCompactMark{
		ID:      id,
		Version: metadata.NoCompactMarkVersion1,

		NoCompactTime: time.Now().Unix(),
		Reason:        reason,
		Details:       details,
	})
	if err != nil {
		return errors.Wrap(err, "json encode no compact mark")
	}

	if err := bkt.Upload(ctx, m, bytes.NewBuffer(noCompactMark)); err != nil {
		return errors.Wrapf(err, "upload file %s to bucket", m)
	}
	markedForNoCompact.Inc()
	level.Info(logger).Log("msg", "block has been marked for no compaction", "block", id)
	return nil
}
