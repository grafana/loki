// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package metadata

import (
	"context"
	"encoding/json"
	"io/ioutil"
	"path"

	"github.com/go-kit/kit/log"
	"github.com/oklog/ulid"
	"github.com/pkg/errors"
	"github.com/thanos-io/thanos/pkg/objstore"
	"github.com/thanos-io/thanos/pkg/runutil"
)

const (
	// DeletionMarkFilename is the known json filename to store details about when block is marked for deletion.
	DeletionMarkFilename = "deletion-mark.json"

	// DeletionMarkVersion1 is the version of deletion-mark file supported by Thanos.
	DeletionMarkVersion1 = 1
)

// ErrorDeletionMarkNotFound is the error when deletion-mark.json file is not found.
var ErrorDeletionMarkNotFound = errors.New("deletion-mark.json not found")

// ErrorUnmarshalDeletionMark is the error when unmarshalling deletion-mark.json file.
// This error can occur because deletion-mark.json has been partially uploaded to block storage
// or the deletion-mark.json file is not a valid json file.
var ErrorUnmarshalDeletionMark = errors.New("unmarshal deletion-mark.json")

// DeletionMark stores block id and when block was marked for deletion.
type DeletionMark struct {
	// ID of the tsdb block.
	ID ulid.ULID `json:"id"`

	// DeletionTime is a unix timestamp of when the block was marked to be deleted.
	DeletionTime int64 `json:"deletion_time"`

	// Version of the file.
	Version int `json:"version"`
}

// ReadDeletionMark reads the given deletion mark file from <dir>/deletion-mark.json in bucket.
func ReadDeletionMark(ctx context.Context, bkt objstore.InstrumentedBucketReader, logger log.Logger, dir string) (*DeletionMark, error) {
	deletionMarkFile := path.Join(dir, DeletionMarkFilename)

	r, err := bkt.ReaderWithExpectedErrs(bkt.IsObjNotFoundErr).Get(ctx, deletionMarkFile)
	if err != nil {
		if bkt.IsObjNotFoundErr(err) {
			return nil, ErrorDeletionMarkNotFound
		}
		return nil, errors.Wrapf(err, "get file: %s", deletionMarkFile)
	}

	defer runutil.CloseWithLogOnErr(logger, r, "close bkt deletion-mark reader")

	metaContent, err := ioutil.ReadAll(r)
	if err != nil {
		return nil, errors.Wrapf(err, "read file: %s", deletionMarkFile)
	}

	deletionMark := DeletionMark{}
	if err := json.Unmarshal(metaContent, &deletionMark); err != nil {
		return nil, errors.Wrapf(ErrorUnmarshalDeletionMark, "file: %s; err: %v", deletionMarkFile, err.Error())
	}

	if deletionMark.Version != DeletionMarkVersion1 {
		return nil, errors.Errorf("unexpected deletion-mark file version %d", deletionMark.Version)
	}

	return &deletionMark, nil
}
