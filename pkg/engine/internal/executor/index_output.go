package executor

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/thanos-io/objstore"

	v2 "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/stats"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

// indexOutput is one index object that an index merge or filter writes for
// one tenant. It records the time range and the posting key bounds of its
// postings rows, so the result artifact of the object can report them.
type indexOutput struct {
	tenant  string
	builder *indexobj.MergeBuilder

	// hasPostings is true once the output got a postings row. hasTime is true
	// once it got a row with a time range. The fields that follow each of
	// them have no meaning before that.
	hasPostings      bool
	minKey, maxKey   postings.Key
	hasTime          bool
	minTime, maxTime int64
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
	if o.hasPostings && key.Compare(o.maxKey) < 0 {
		return fmt.Errorf("postings row with key %v sorts before the previous key %v", key, o.maxKey)
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

	if !o.hasPostings {
		o.hasPostings, o.minKey = true, key
	}
	o.maxKey = key

	// A bloom row of a column that no record set has an empty time range,
	// with its minimum above its maximum. It adds no time to the output.
	if row.MinTimestamp > row.MaxTimestamp {
		return nil
	}
	if !o.hasTime {
		o.hasTime, o.minTime, o.maxTime = true, row.MinTimestamp, row.MaxTimestamp
	}
	o.minTime = min(o.minTime, row.MinTimestamp)
	o.maxTime = max(o.maxTime, row.MaxTimestamp)
	return nil
}

func (o *indexOutput) appendStat(stat stats.Stat) error {
	return o.builder.AppendStat(o.tenant, stat)
}

// reset releases the resources of the output and empties it.
func (o *indexOutput) reset() {
	o.builder.Reset()
	*o = indexOutput{tenant: o.tenant, builder: o.builder}
}

// artifact returns the result artifact of the output when it is uploaded to
// path. An output without postings rows has no posting key bounds, and one
// without a postings row with a time range has no time range.
func (o *indexOutput) artifact(path string) (v2.ResultArtifact, error) {
	artifact := v2.ResultArtifact{Path: path}
	if o.hasPostings {
		keys, err := postings.NewKeyRange(o.minKey, o.maxKey)
		if err != nil {
			return v2.ResultArtifact{}, fmt.Errorf("posting key bounds of index %q: %w", path, err)
		}
		artifact.PostingKeys = keys
	}
	if o.hasTime {
		artifact.MinTime = time.Unix(0, o.minTime).UTC()
		artifact.MaxTime = time.Unix(0, o.maxTime).UTC()
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

	pathReader, err := obj.Reader(ctx)
	if err != nil {
		return v2.ResultArtifact{}, 0, 0, errors.Join(err, closer.Close())
	}
	path, hashErr := v2.CompactedIndexPath(o.tenant, pathReader)
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
	if err := bucket.Upload(ctx, path, uploadReader); err != nil {
		return v2.ResultArtifact{}, 0, 0, errors.Join(fmt.Errorf("uploading index %q: %w", path, err), uploadReader.Close(), closer.Close())
	}
	if err := uploadReader.Close(); err != nil {
		return v2.ResultArtifact{}, 0, 0, errors.Join(fmt.Errorf("closing upload reader for index %q: %w", path, err), closer.Close())
	}

	if err := closer.Close(); err != nil {
		return v2.ResultArtifact{}, 0, 0, fmt.Errorf("closing index %q: %w", path, err)
	}
	artifact, err = o.artifact(path)
	if err != nil {
		return v2.ResultArtifact{}, 0, 0, err
	}
	// Flush emptied the builder, so only the recorded bounds remain.
	*o = indexOutput{tenant: o.tenant, builder: o.builder}
	return artifact, compressedBytes, uncompressedBytes, nil
}
