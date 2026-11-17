package executor

import (
	"context"
	"fmt"

	"github.com/thanos-io/objstore"

	v2 "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/stats"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/util"
)

// indexOutput is one index object that an index merge or filter writes for
// one tenant. It records the posting key bounds of its postings rows, so the
// result artifact of the object can report them.
type indexOutput struct {
	tenant  string
	builder *indexobj.MergeBuilder
	keys    keyBounds
}

// keyBounds is the smallest and the largest posting key of the postings rows
// of an index output. The zero value has no rows.
type keyBounds struct {
	// hasPostings is true once the output got a postings row. min and max
	// have no meaning before that.
	hasPostings bool
	min, max    postings.Key
}

// newIndexOutput returns an empty output for tenant. The log objects of its
// rows must have layout. The caller must call reset when it is done with the
// output.
func (c *Context) newIndexOutput(tenant string, layout streams.Layout) (*indexOutput, error) {
	builder, err := indexobj.NewMergeBuilder(c.indexobjCfg, c.scratchStore, tenant, layout)
	if err != nil {
		return nil, fmt.Errorf("creating index builder: %w", err)
	}
	return &indexOutput{tenant: tenant, builder: builder}, nil
}

// appendPostingsRow writes row to the output. It returns an error if the key
// of row sorts before the key of the previous row, because the posting key
// bounds of the output are its first and last key.
func (o *indexOutput) appendPostingsRow(row postings.Row) error {
	key := row.Key()
	if o.keys.hasPostings && key.Compare(o.keys.max) < 0 {
		return fmt.Errorf("postings row with key %v sorts before the previous key %v", key, o.keys.max)
	}

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

	if !o.keys.hasPostings {
		o.keys.hasPostings, o.keys.min = true, key
	}
	o.keys.max = key
	return nil
}

func (o *indexOutput) appendStat(stat stats.Stat) error {
	return o.builder.AppendStat(o.tenant, stat)
}

// reset releases the resources of the output and empties it.
func (o *indexOutput) reset() {
	o.builder.Reset()
	o.keys = keyBounds{}
}

// artifact returns the result artifact of the output when it is uploaded to
// path. An output without postings rows has no posting key bounds.
func (o *indexOutput) artifact(path string) (v2.ResultArtifact, error) {
	artifact := v2.ResultArtifact{Path: path}
	if o.keys.hasPostings {
		keys, err := postings.NewKeyRange(o.keys.min, o.keys.max)
		if err != nil {
			return v2.ResultArtifact{}, fmt.Errorf("posting key bounds of index %q: %w", path, err)
		}
		artifact.PostingKeys = keys
	}
	return artifact, nil
}

// upload flushes the output and uploads its index object to a content-hash
// path in bucket. It returns the artifact of the object and the compressed
// and uncompressed sizes of the object in bytes. The output is empty after a
// successful upload.
func (o *indexOutput) upload(ctx context.Context, bucket objstore.Bucket) (artifact v2.ResultArtifact, compressedBytes, uncompressedBytes int64, err error) {
	// Read the size before Flush, because Flush resets the builder.
	uncompressedBytes = int64(o.builder.GetEstimatedSize())

	obj, closer, err := o.builder.Flush()
	if err != nil {
		return v2.ResultArtifact{}, 0, 0, fmt.Errorf("flushing index: %w", err)
	}
	defer util.CloseAndHandleError(closer, &err)

	path, err := compactedIndexPath(ctx, o.tenant, obj)
	if err != nil {
		return v2.ResultArtifact{}, 0, 0, fmt.Errorf("generating index path: %w", err)
	}
	compressedBytes, err = uploadObject(ctx, bucket, path, obj)
	if err != nil {
		return v2.ResultArtifact{}, 0, 0, fmt.Errorf("uploading index %q: %w", path, err)
	}
	artifact, err = o.artifact(path)
	if err != nil {
		return v2.ResultArtifact{}, 0, 0, err
	}
	o.reset()
	return artifact, compressedBytes, uncompressedBytes, nil
}
