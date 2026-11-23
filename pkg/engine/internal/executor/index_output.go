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

// indexOutput writes the output of an index merge or filter for one tenant as
// an index run: one or more index objects cut by size. It records the posting
// key bounds of each index object, so the result artifact of the object can
// report them.
//
// Postings rows must come first, in posting key order, then stats rows. The
// output cuts a new index object before a postings row when the current one is
// full and the posting key changes. So no posting key spans two index objects,
// every index object of the output holds postings, and all stats rows of the
// output go into its last index object. An earlier index object records the
// layout in an empty stats section, unless the output has the zero layout.
//
// The tasks of a split merge each write one output into the same index run,
// so the stats rows of a run can sit in several of its index objects.
//
// After an error, the output holds no usable state, and the caller must call
// reset.
//
// The output uploads each index object when it cuts it, so it holds only one
// index object in memory.
type indexOutput struct {
	tenant  string
	bucket  objstore.Bucket
	builder *indexobj.MergeBuilder

	// keys holds the posting key bounds of the current index object.
	keys keyBounds

	// gotStats is true once the output got a stats row. A postings row after
	// it would make the output cut before the stats rows are done.
	gotStats bool

	// artifacts are the uploaded index objects, in posting key order.
	artifacts []v2.ResultArtifact
	sizes     outputSizes
}

// outputSizes holds the sizes of the uploaded index objects of an output, in
// bytes.
type outputSizes struct {
	compressed, uncompressed int64
}

// keyBounds is the smallest and the largest of the posting keys that it got,
// in key order. The zero value holds no keys.
type keyBounds struct {
	// hasKeys is true once the bounds got a key. min and max have no meaning
	// before that.
	hasKeys  bool
	min, max postings.Key
}

// add extends the bounds to key. It returns an error and leaves the bounds
// unchanged if key sorts before the last key that the bounds got.
func (b *keyBounds) add(key postings.Key) error {
	if b.hasKeys && key.Compare(b.max) < 0 {
		return fmt.Errorf("postings row with key %v sorts before the previous key %v", key, b.max)
	}
	if !b.hasKeys {
		b.hasKeys, b.min = true, key
	}
	b.max = key
	return nil
}

// keyRange returns the bounds as a KeyRange. It returns the zero KeyRange if
// the bounds hold no keys.
func (b keyBounds) keyRange() (postings.KeyRange, error) {
	if !b.hasKeys {
		return postings.KeyRange{}, nil
	}
	return postings.NewKeyRange(b.min, b.max)
}

// newIndexOutput returns an empty output for tenant that uploads to the
// bucket of c. The log objects of its rows must have layout. The caller must
// call reset when it is done with the output.
func (c *Context) newIndexOutput(tenant string, layout streams.Layout) (*indexOutput, error) {
	builder, err := indexobj.NewMergeBuilder(c.indexobjCfg, c.scratchStore, tenant, layout)
	if err != nil {
		return nil, fmt.Errorf("creating index builder: %w", err)
	}
	return &indexOutput{tenant: tenant, bucket: c.bucket, builder: builder}, nil
}

// appendPostingsRow writes row to the output. It first uploads the current
// index object if that object is full and the key of row sorts after its last
// key.
//
// It returns an error if the key of row sorts before the key of the previous
// row, because the posting key bounds of each index object are its first and
// last key. It also returns an error after a stats row.
func (o *indexOutput) appendPostingsRow(ctx context.Context, row postings.Row) error {
	key := row.Key()
	if o.gotStats {
		return fmt.Errorf("postings row with key %v after a stats row", key)
	}
	if o.keys.hasKeys && o.builder.IsFull() && key.Compare(o.keys.max) > 0 {
		if err := o.uploadCurrent(ctx); err != nil {
			return err
		}
	}
	if err := o.keys.add(key); err != nil {
		return err
	}

	switch row.Kind {
	case postings.KindLabel:
		return o.builder.AppendPostingsLabelEntry(o.tenant, row.LabelEntry())
	case postings.KindBloom:
		return o.builder.AppendPostingsBloomEntry(o.tenant, row.BloomEntry())
	default:
		return fmt.Errorf("unknown postings kind: %v", row.Kind)
	}
}

// appendStat writes stat to the current index object. The output never cuts
// an index object before a stats row, so all stats rows go into the last index
// object.
func (o *indexOutput) appendStat(stat stats.Stat) error {
	o.gotStats = true
	return o.builder.AppendStat(o.tenant, stat)
}

// reset releases the resources of the current index object and empties the
// output. It forgets the index objects that the output uploaded, but does not
// delete them. A failed task leaves them in the bucket, unreferenced by any
// ToC. Deleting them is not safe, because an index object at the same
// content-hash path can be one that a ToC references.
func (o *indexOutput) reset() {
	o.builder.Reset()
	o.keys = keyBounds{}
	o.gotStats = false
	o.artifacts = nil
	o.sizes = outputSizes{}
}

// finish uploads the current index object and returns the artifacts of every
// uploaded index object, in posting key order, and their total sizes. It
// empties the output, so the output can write another run.
func (o *indexOutput) finish(ctx context.Context) ([]v2.ResultArtifact, outputSizes, error) {
	if err := o.uploadCurrent(ctx); err != nil {
		return nil, outputSizes{}, err
	}
	artifacts, sizes := o.artifacts, o.sizes
	o.reset()
	return artifacts, sizes, nil
}

// currentArtifact returns the result artifact of the current index object at
// path. An index object without postings rows has no posting key bounds.
func (o *indexOutput) currentArtifact(path string) (v2.ResultArtifact, error) {
	keys, err := o.keys.keyRange()
	if err != nil {
		return v2.ResultArtifact{}, fmt.Errorf("posting key bounds of index %q: %w", path, err)
	}
	return v2.ResultArtifact{Path: path, PostingKeys: keys}, nil
}

// uploadCurrent flushes the current index object and uploads it to a
// content-hash path. It adds its artifact and sizes to the output and starts
// an empty index object.
func (o *indexOutput) uploadCurrent(ctx context.Context) error {
	artifact, sizes, err := o.flushAndUpload(ctx)
	if err != nil {
		return err
	}
	o.artifacts = append(o.artifacts, artifact)
	o.sizes.compressed += sizes.compressed
	o.sizes.uncompressed += sizes.uncompressed
	o.keys = keyBounds{}
	return nil
}

// flushAndUpload flushes the current index object, uploads it to a
// content-hash path and releases it. The artifact and the sizes have no
// meaning if it returns an error, including an error that releases the index
// object.
func (o *indexOutput) flushAndUpload(ctx context.Context) (artifact v2.ResultArtifact, sizes outputSizes, err error) {
	// Read the size before Flush, because Flush resets the builder.
	uncompressedBytes := int64(o.builder.GetEstimatedSize())

	obj, closer, err := o.builder.Flush()
	if err != nil {
		return v2.ResultArtifact{}, outputSizes{}, fmt.Errorf("flushing index: %w", err)
	}
	defer util.CloseAndHandleError(closer, &err)

	path, err := compactedIndexPath(ctx, o.tenant, obj)
	if err != nil {
		return v2.ResultArtifact{}, outputSizes{}, fmt.Errorf("generating index path: %w", err)
	}
	compressedBytes, err := uploadObject(ctx, o.bucket, path, obj)
	if err != nil {
		return v2.ResultArtifact{}, outputSizes{}, fmt.Errorf("uploading index %q: %w", path, err)
	}
	artifact, err = o.currentArtifact(path)
	if err != nil {
		return v2.ResultArtifact{}, outputSizes{}, err
	}
	return artifact, outputSizes{compressed: compressedBytes, uncompressed: uncompressedBytes}, nil
}
