package bucketindex

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io/ioutil"
	"path"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/oklog/ulid"
	"github.com/pkg/errors"
	"github.com/thanos-io/thanos/pkg/block"
	"github.com/thanos-io/thanos/pkg/block/metadata"
	"github.com/thanos-io/thanos/pkg/objstore"
	"github.com/thanos-io/thanos/pkg/runutil"

	"github.com/cortexproject/cortex/pkg/storage/bucket"
	"github.com/cortexproject/cortex/pkg/util"
)

var (
	ErrBlockMetaNotFound          = block.ErrorSyncMetaNotFound
	ErrBlockMetaCorrupted         = block.ErrorSyncMetaCorrupted
	ErrBlockDeletionMarkNotFound  = errors.New("block deletion mark not found")
	ErrBlockDeletionMarkCorrupted = errors.New("block deletion mark corrupted")
)

// Writer is responsible to generate and write a bucket index.
type Writer struct {
	bkt    objstore.InstrumentedBucket
	logger log.Logger
}

func NewWriter(bkt objstore.Bucket, userID string, logger log.Logger) *Writer {
	return &Writer{
		bkt:    bucket.NewUserBucketClient(userID, bkt),
		logger: util.WithUserID(userID, logger),
	}
}

// WriteIndex generates the bucket index and writes it to the storage. If the old index is not
// passed in input, then the bucket index will be generated from scratch.
func (w *Writer) WriteIndex(ctx context.Context, old *Index) (*Index, error) {
	idx, err := w.GenerateIndex(ctx, old)
	if err != nil {
		return nil, errors.Wrap(err, "generate bucket index")
	}

	// Marshal the index.
	content, err := json.Marshal(idx)
	if err != nil {
		return nil, errors.Wrap(err, "marshal bucket index")
	}

	// Compress it.
	var gzipContent bytes.Buffer
	gzip := gzip.NewWriter(&gzipContent)
	gzip.Name = IndexFilename

	if _, err := gzip.Write(content); err != nil {
		return nil, errors.Wrap(err, "gzip bucket index")
	}
	if err := gzip.Close(); err != nil {
		return nil, errors.Wrap(err, "close gzip bucket index")
	}

	// Upload the index to the storage.
	if err := w.bkt.Upload(ctx, IndexCompressedFilename, &gzipContent); err != nil {
		return nil, errors.Wrap(err, "upload bucket index")
	}

	return idx, nil
}

// GenerateIndex generates the bucket index and returns it, without storing it to the storage.
// If the old index is not passed in input, then the bucket index will be generated from scratch.
func (w *Writer) GenerateIndex(ctx context.Context, old *Index) (*Index, error) {
	var oldBlocks []*Block
	var oldBlockDeletionMarks []*BlockDeletionMark

	// Read the old index, if provided.
	if old != nil {
		oldBlocks = old.Blocks
		oldBlockDeletionMarks = old.BlockDeletionMarks
	}

	blocks, err := w.generateBlocksIndex(ctx, oldBlocks)
	if err != nil {
		return nil, err
	}

	blockDeletionMarks, err := w.generateBlockDeletionMarksIndex(ctx, oldBlockDeletionMarks)
	if err != nil {
		return nil, err
	}

	return &Index{
		Version:            IndexVersion1,
		Blocks:             blocks,
		BlockDeletionMarks: blockDeletionMarks,
		UpdatedAt:          time.Now().Unix(),
	}, nil
}

func (w *Writer) generateBlocksIndex(ctx context.Context, old []*Block) ([]*Block, error) {
	out := make([]*Block, 0, len(old))
	discovered := map[ulid.ULID]struct{}{}

	// Find all blocks in the storage.
	err := w.bkt.Iter(ctx, "", func(name string) error {
		if id, ok := block.IsBlockDir(name); ok {
			discovered[id] = struct{}{}
		}
		return nil
	})
	if err != nil {
		return nil, errors.Wrap(err, "list blocks")
	}

	// Since blocks are immutable, all blocks already existing in the index can just be copied.
	for _, b := range old {
		if _, ok := discovered[b.ID]; ok {
			out = append(out, b)
			delete(discovered, b.ID)
		}
	}

	// Remaining blocks are new ones and we have to fetch the meta.json for each of them, in order
	// to find out if their upload has been completed (meta.json is uploaded last) and get the block
	// information to store in the bucket index.
	for id := range discovered {
		b, err := w.generateBlockIndexEntry(ctx, id)
		if errors.Is(err, ErrBlockMetaNotFound) {
			level.Warn(w.logger).Log("msg", "skipped partial block when generating bucket index", "block", id.String())
			continue
		}
		if errors.Is(err, ErrBlockMetaCorrupted) {
			level.Error(w.logger).Log("msg", "skipped block with corrupted meta.json when generating bucket index", "block", id.String(), "err", err)
			continue
		}
		if err != nil {
			return nil, err
		}

		out = append(out, b)
	}

	return out, nil
}

func (w *Writer) generateBlockIndexEntry(ctx context.Context, id ulid.ULID) (*Block, error) {
	metaFile := path.Join(id.String(), block.MetaFilename)

	// Get the block's meta.json file.
	r, err := w.bkt.Get(ctx, metaFile)
	if w.bkt.IsObjNotFoundErr(err) {
		return nil, ErrBlockMetaNotFound
	}
	if err != nil {
		return nil, errors.Wrapf(err, "get block meta file: %v", metaFile)
	}
	defer runutil.CloseWithLogOnErr(w.logger, r, "close get block meta file")

	metaContent, err := ioutil.ReadAll(r)
	if err != nil {
		return nil, errors.Wrapf(err, "read block meta file: %v", metaFile)
	}

	// Unmarshal it.
	m := metadata.Meta{}
	if err := json.Unmarshal(metaContent, &m); err != nil {
		return nil, errors.Wrapf(ErrBlockMetaCorrupted, "unmarshal block meta file %s: %v", metaFile, err)
	}

	if m.Version != metadata.TSDBVersion1 {
		return nil, errors.Errorf("unexpected block meta version: %s version: %d", metaFile, m.Version)
	}

	block := BlockFromThanosMeta(m)

	// Get the meta.json attributes.
	attrs, err := w.bkt.Attributes(ctx, metaFile)
	if err != nil {
		return nil, errors.Wrapf(err, "read meta file attributes: %v", metaFile)
	}

	// Since the meta.json file is the last file of a block being uploaded and it's immutable
	// we can safely assume that the last modified timestamp of the meta.json is the time when
	// the block has completed to be uploaded.
	block.UploadedAt = attrs.LastModified.Unix()

	return block, nil
}

func (w *Writer) generateBlockDeletionMarksIndex(ctx context.Context, old []*BlockDeletionMark) ([]*BlockDeletionMark, error) {
	out := make([]*BlockDeletionMark, 0, len(old))
	discovered := map[ulid.ULID]struct{}{}

	// Find all markers in the storage.
	err := w.bkt.Iter(ctx, MarkersPathname+"/", func(name string) error {
		if blockID, ok := IsBlockDeletionMarkFilename(path.Base(name)); ok {
			discovered[blockID] = struct{}{}
		}
		return nil
	})
	if err != nil {
		return nil, errors.Wrap(err, "list block deletion marks")
	}

	// Since deletion marks are immutable, all markers already existing in the index can just be copied.
	for _, m := range old {
		if _, ok := discovered[m.ID]; ok {
			out = append(out, m)
			delete(discovered, m.ID)
		}
	}

	// Remaining markers are new ones and we have to fetch them.
	for id := range discovered {
		m, err := w.generateBlockDeletionMarkIndexEntry(ctx, id)
		if errors.Is(err, ErrBlockDeletionMarkNotFound) {
			// This could happen if the block is permanently deleted between the "list objects" and now.
			level.Warn(w.logger).Log("msg", "skipped missing block deletion mark when generating bucket index", "block", id.String())
			continue
		}
		if errors.Is(err, ErrBlockDeletionMarkCorrupted) {
			level.Error(w.logger).Log("msg", "skipped corrupted block deletion mark when generating bucket index", "block", id.String(), "err", err)
			continue
		}
		if err != nil {
			return nil, err
		}

		out = append(out, m)
	}

	return out, nil
}

func (w *Writer) generateBlockDeletionMarkIndexEntry(ctx context.Context, id ulid.ULID) (*BlockDeletionMark, error) {
	m := metadata.DeletionMark{}

	if err := metadata.ReadMarker(ctx, w.logger, w.bkt, id.String(), &m); err != nil {
		if errors.Is(err, metadata.ErrorUnmarshalMarker) {
			return nil, errors.Wrap(ErrBlockDeletionMarkCorrupted, err.Error())
		}
		return nil, err
	}

	return BlockDeletionMarkFromThanosMarker(&m), nil
}
