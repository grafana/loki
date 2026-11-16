package executor

import (
	"context"
	"errors"
	"fmt"
	"time"

	v2 "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/stats"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

// indexOutput is one index object that an index merge or filter writes for
// one tenant. It records the time range and the posting key bounds of its
// postings rows, so the result artifact of the object can report them.
//
// Postings rows must arrive in posting key order. The first row then holds the
// smallest key, and the last row holds the largest key.
type indexOutput struct {
	tenant  string
	builder *indexobj.MergeBuilder

	// hasPostings is true once the output got a postings row. The other
	// fields have no meaning before that.
	hasPostings      bool
	minKey, maxKey   postings.Key
	minTime, maxTime int64
}

// newIndexOutput returns an empty output for tenant, whose log objects have
// layout. The caller must call reset when it is done with the output.
func (c *Context) newIndexOutput(tenant string, layout streams.Layout) (*indexOutput, error) {
	builder, err := indexobj.NewMergeBuilder(c.indexobjCfg, c.scratchStore, tenant, layout)
	if err != nil {
		return nil, fmt.Errorf("creating index builder: %w", err)
	}
	return &indexOutput{tenant: tenant, builder: builder}, nil
}

// appendPostingsRow writes row to the output.
func (o *indexOutput) appendPostingsRow(row postings.Row) error {
	var err error
	switch row.Kind {
	case postings.KindLabel:
		err = o.builder.AppendPostingsLabelEntry(o.tenant, row.LabelEntry())
	case postings.KindBloom:
		err = o.builder.AppendPostingsBloomEntry(o.tenant, row.BloomEntry())
	default:
		err = fmt.Errorf("unknown postings kind: %v", row.Kind)
	}
	if err != nil {
		return err
	}

	key := row.Key()
	if !o.hasPostings {
		o.hasPostings = true
		o.minKey, o.minTime, o.maxTime = key, row.MinTimestamp, row.MaxTimestamp
	}
	o.maxKey = key
	o.minTime = min(o.minTime, row.MinTimestamp)
	o.maxTime = max(o.maxTime, row.MaxTimestamp)
	return nil
}

// appendStat writes stat to the output.
func (o *indexOutput) appendStat(stat stats.Stat) error {
	return o.builder.AppendStat(o.tenant, stat)
}

// reset releases the resources of the output.
func (o *indexOutput) reset() { o.builder.Reset() }

// artifact returns the result artifact of the output when it is uploaded to
// path. An output without postings rows has no time range and no posting key
// bounds.
func (o *indexOutput) artifact(path string) (v2.ResultArtifact, error) {
	artifact := v2.ResultArtifact{Path: path}
	if !o.hasPostings {
		return artifact, nil
	}
	keys, err := postings.NewKeyRange(o.minKey, o.maxKey)
	if err != nil {
		return v2.ResultArtifact{}, fmt.Errorf("posting key bounds of index %q: %w", path, err)
	}
	artifact.MinTime = time.Unix(0, o.minTime).UTC()
	artifact.MaxTime = time.Unix(0, o.maxTime).UTC()
	artifact.PostingKeys = keys
	return artifact, nil
}

// uploadIndex flushes out and uploads the index object to a content-hash
// path. It returns the artifact for the uploaded object and the object's
// compressed and uncompressed sizes in bytes.
func (c *Context) uploadIndex(ctx context.Context, out *indexOutput) (artifact v2.ResultArtifact, compressedBytes, uncompressedBytes int64, err error) {
	// Read the size before Flush, because Flush resets the builder.
	uncompressedBytes = int64(out.builder.GetEstimatedSize())

	obj, closer, err := out.builder.Flush()
	if err != nil {
		return v2.ResultArtifact{}, 0, 0, fmt.Errorf("flushing index: %w", err)
	}

	pathReader, err := obj.Reader(ctx)
	if err != nil {
		return v2.ResultArtifact{}, 0, 0, errors.Join(err, closer.Close())
	}
	path, hashErr := v2.CompactedIndexPath(out.tenant, pathReader)
	if cerr := pathReader.Close(); cerr != nil && hashErr == nil {
		hashErr = cerr
	}
	if hashErr != nil {
		return v2.ResultArtifact{}, 0, 0, errors.Join(hashErr, closer.Close())
	}

	compressedBytes = obj.Size()
	uploadReader, err := obj.Reader(ctx)
	if err != nil {
		return v2.ResultArtifact{}, 0, 0, errors.Join(err, closer.Close())
	}
	if err := c.bucket.Upload(ctx, path, uploadReader); err != nil {
		return v2.ResultArtifact{}, 0, 0, errors.Join(fmt.Errorf("uploading index %q: %w", path, err), uploadReader.Close(), closer.Close())
	}
	if err := uploadReader.Close(); err != nil {
		return v2.ResultArtifact{}, 0, 0, errors.Join(fmt.Errorf("closing upload reader for index %q: %w", path, err), closer.Close())
	}

	if err := closer.Close(); err != nil {
		return v2.ResultArtifact{}, 0, 0, fmt.Errorf("closing index %q: %w", path, err)
	}
	artifact, err = out.artifact(path)
	if err != nil {
		return v2.ResultArtifact{}, 0, 0, err
	}
	return artifact, compressedBytes, uncompressedBytes, nil
}
