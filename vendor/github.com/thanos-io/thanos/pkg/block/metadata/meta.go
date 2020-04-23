// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package metadata

// metadata package implements writing and reading wrapped meta.json where Thanos puts its metadata.
// Those metadata contains external labels, downsampling resolution and source type.
// This package is minimal and separated because it used by testutils which limits test helpers we can use in
// this package.

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/tsdb"
	"github.com/prometheus/prometheus/tsdb/fileutil"
	"github.com/thanos-io/thanos/pkg/runutil"
)

type SourceType string

const (
	UnknownSource         SourceType = ""
	SidecarSource         SourceType = "sidecar"
	ReceiveSource         SourceType = "receive"
	CompactorSource       SourceType = "compactor"
	CompactorRepairSource SourceType = "compactor.repair"
	RulerSource           SourceType = "ruler"
	BucketRepairSource    SourceType = "bucket.repair"
	TestSource            SourceType = "test"
)

const (
	// MetaFilename is the known JSON filename for meta information.
	MetaFilename = "meta.json"
)

const (
	// MetaVersion is a enumeration of meta versions supported by Thanos.
	MetaVersion1 = iota + 1
)

// Meta describes the a block's meta. It wraps the known TSDB meta structure and
// extends it by Thanos-specific fields.
type Meta struct {
	tsdb.BlockMeta

	Thanos Thanos `json:"thanos"`
}

// Thanos holds block meta information specific to Thanos.
type Thanos struct {
	Labels     map[string]string `json:"labels"`
	Downsample ThanosDownsample  `json:"downsample"`

	// Source is a real upload source of the block.
	Source SourceType `json:"source"`
}

type ThanosDownsample struct {
	Resolution int64 `json:"resolution"`
}

// InjectThanos sets Thanos meta to the block meta JSON and saves it to the disk.
// NOTE: It should be used after writing any block by any Thanos component, otherwise we will miss crucial metadata.
func InjectThanos(logger log.Logger, bdir string, meta Thanos, downsampledMeta *tsdb.BlockMeta) (*Meta, error) {
	newMeta, err := Read(bdir)
	if err != nil {
		return nil, errors.Wrap(err, "read new meta")
	}
	newMeta.Thanos = meta

	// While downsampling we need to copy original compaction.
	if downsampledMeta != nil {
		newMeta.Compaction = downsampledMeta.Compaction
	}

	if err := Write(logger, bdir, newMeta); err != nil {
		return nil, errors.Wrap(err, "write new meta")
	}

	return newMeta, nil
}

// Write writes the given meta into <dir>/meta.json.
func Write(logger log.Logger, dir string, meta *Meta) error {
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
		runutil.CloseWithLogOnErr(logger, f, "close meta")
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return renameFile(logger, tmp, path)
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
		runutil.CloseWithLogOnErr(logger, pdir, "close dir")
		return err
	}
	return pdir.Close()
}

// Read reads the given meta from <dir>/meta.json.
func Read(dir string) (*Meta, error) {
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
