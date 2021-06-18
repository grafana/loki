// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package metadata

// metadata package implements writing and reading wrapped meta.json where Thanos puts its metadata.
// Those metadata contains external labels, downsampling resolution and source type.
// This package is minimal and separated because it used by testutils which limits test helpers we can use in
// this package.

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/go-kit/kit/log"
	"github.com/oklog/ulid"
	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/prometheus/prometheus/tsdb"
	"github.com/prometheus/prometheus/tsdb/fileutil"
	"github.com/prometheus/prometheus/tsdb/tombstones"
	"github.com/thanos-io/thanos/pkg/runutil"
	"gopkg.in/yaml.v3"
)

type SourceType string

const (
	// TODO(bwplotka): Merge with pkg/component package.
	UnknownSource         SourceType = ""
	SidecarSource         SourceType = "sidecar"
	ReceiveSource         SourceType = "receive"
	CompactorSource       SourceType = "compactor"
	CompactorRepairSource SourceType = "compactor.repair"
	RulerSource           SourceType = "ruler"
	BucketRepairSource    SourceType = "bucket.repair"
	BucketRewriteSource   SourceType = "bucket.rewrite"
	TestSource            SourceType = "test"
)

const (
	// MetaFilename is the known JSON filename for meta information.
	MetaFilename = "meta.json"
	// TSDBVersion1 is a enumeration of TSDB meta versions supported by Thanos.
	TSDBVersion1 = 1
	// ThanosVersion1 is a enumeration of Thanos section of TSDB meta supported by Thanos.
	ThanosVersion1 = 1
)

// Meta describes the a block's meta. It wraps the known TSDB meta structure and
// extends it by Thanos-specific fields.
type Meta struct {
	tsdb.BlockMeta

	Thanos Thanos `json:"thanos"`
}

func (m *Meta) String() string {
	return fmt.Sprintf("%s (min time: %d, max time: %d)", m.ULID, m.MinTime, m.MaxTime)
}

// Thanos holds block meta information specific to Thanos.
type Thanos struct {
	// Version of Thanos meta file. If none specified, 1 is assumed (since first version did not have explicit version specified).
	Version int `json:"version,omitempty"`

	// Labels are the external labels identifying the producer as well as tenant.
	// See https://thanos.io/tip/thanos/storage.md#external-labels for details.
	Labels     map[string]string `json:"labels"`
	Downsample ThanosDownsample  `json:"downsample"`

	// Source is a real upload source of the block.
	Source SourceType `json:"source"`

	// List of segment files (in chunks directory), in sorted order. Optional.
	// Deprecated. Use Files instead.
	SegmentFiles []string `json:"segment_files,omitempty"`

	// File is a sorted (by rel path) list of all files in block directory of this block known to TSDB.
	// Sorted by relative path.
	// Useful to avoid API call to get size of each file, as well as for debugging purposes.
	// Optional, added in v0.17.0.
	Files []File `json:"files,omitempty"`

	// Rewrites is present when any rewrite (deletion, relabel etc) were applied to this block. Optional.
	Rewrites []Rewrite `json:"rewrites,omitempty"`
}

type Rewrite struct {
	// ULIDs of all source head blocks that went into the block.
	Sources []ulid.ULID `json:"sources,omitempty"`
	// Deletions if applied (in order).
	DeletionsApplied []DeletionRequest `json:"deletions_applied,omitempty"`
}

type Matchers []*labels.Matcher

func (m *Matchers) UnmarshalYAML(value *yaml.Node) (err error) {
	*m, err = parser.ParseMetricSelector(value.Value)
	if err != nil {
		return errors.Wrapf(err, "parse metric selector %v", value.Value)
	}
	return nil
}

type DeletionRequest struct {
	Matchers  Matchers             `json:"matchers" yaml:"matchers"`
	Intervals tombstones.Intervals `json:"intervals,omitempty" yaml:"intervals,omitempty"`
}

type File struct {
	RelPath string `json:"rel_path"`
	// SizeBytes is optional (e.g meta.json does not show size).
	SizeBytes int64 `json:"size_bytes,omitempty"`

	// Hash is an optional hash of this file. Used for potentially avoiding an extra download.
	Hash *ObjectHash `json:"hash,omitempty"`
}

type ThanosDownsample struct {
	Resolution int64 `json:"resolution"`
}

// InjectThanos sets Thanos meta to the block meta JSON and saves it to the disk.
// NOTE: It should be used after writing any block by any Thanos component, otherwise we will miss crucial metadata.
func InjectThanos(logger log.Logger, bdir string, meta Thanos, downsampledMeta *tsdb.BlockMeta) (*Meta, error) {
	newMeta, err := ReadFromDir(bdir)
	if err != nil {
		return nil, errors.Wrap(err, "read new meta")
	}
	newMeta.Thanos = meta

	// While downsampling we need to copy original compaction.
	if downsampledMeta != nil {
		newMeta.Compaction = downsampledMeta.Compaction
	}

	if err := newMeta.WriteToDir(logger, bdir); err != nil {
		return nil, errors.Wrap(err, "write new meta")
	}

	return newMeta, nil
}

// WriteToDir writes the encoded meta into <dir>/meta.json.
func (m Meta) WriteToDir(logger log.Logger, dir string) error {
	// Make any changes to the file appear atomic.
	path := filepath.Join(dir, MetaFilename)
	tmp := path + ".tmp"

	f, err := os.Create(tmp)
	if err != nil {
		return err
	}

	if err := m.Write(f); err != nil {
		runutil.CloseWithLogOnErr(logger, f, "close meta")
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return renameFile(logger, tmp, path)
}

// Write writes the given encoded meta to writer.
func (m Meta) Write(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "\t")
	return enc.Encode(&m)
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

// ReadFromDir reads the given meta from <dir>/meta.json.
func ReadFromDir(dir string) (*Meta, error) {
	f, err := os.Open(filepath.Join(dir, MetaFilename))
	if err != nil {
		return nil, err
	}
	return Read(f)
}

// Read the block meta from the given reader.
func Read(rc io.ReadCloser) (_ *Meta, err error) {
	defer runutil.ExhaustCloseWithErrCapture(&err, rc, "close meta JSON")

	var m Meta
	if err = json.NewDecoder(rc).Decode(&m); err != nil {
		return nil, err
	}

	if m.Version != TSDBVersion1 {
		return nil, errors.Errorf("unexpected meta file version %d", m.Version)
	}

	version := m.Thanos.Version
	if version == 0 {
		// For compatibility.
		version = ThanosVersion1
	}

	if version != ThanosVersion1 {
		return nil, errors.Errorf("unexpected meta file Thanos section version %d", m.Version)
	}

	if m.Thanos.Labels == nil {
		// To avoid extra nil checks, allocate map here if empty.
		m.Thanos.Labels = make(map[string]string)
	}
	return &m, nil
}
