package executor

import (
	"context"
	"fmt"

	"github.com/grafana/loki/v3/pkg/dataobj"
	v2 "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2"
	dataobjindex "github.com/grafana/loki/v3/pkg/dataobj/index"
	"github.com/grafana/loki/v3/pkg/util"
)

// uploadAndIndexObject uploads a logs object, adds it to calc, and closes its
// backing resources.
func (c *Context) uploadAndIndexObject(
	ctx context.Context,
	obj *dataobj.Object,
	uploadDestination string,
	calc *dataobjindex.Calculator,
) (int64, error) {
	size, err := uploadObject(ctx, c.dataObjectBucket(), uploadDestination, obj)
	if err != nil {
		return 0, fmt.Errorf("uploading %q: %w", uploadDestination, err)
	}
	if err := calc.Calculate(ctx, c.logger, obj, uploadDestination); err != nil {
		return 0, fmt.Errorf("indexing %q: %w", uploadDestination, err)
	}
	return size, nil
}

// flushAndUploadIndex flushes calc, uploads the index to the content-addressed
// compacted index path of tenant, and releases the flushed object. It returns
// the artifact of the index, with the time range of its postings rows. The
// artifact has no posting key bounds, because calc does not track them.
func (c *Context) flushAndUploadIndex(
	ctx context.Context,
	tenant string,
	calc *dataobjindex.Calculator,
) (artifact v2.ResultArtifact, flushErr error) {
	obj, closer, timeRange, err := calc.Flush()
	if err != nil {
		return v2.ResultArtifact{}, fmt.Errorf("flushing index: %w", err)
	}
	defer func() {
		closeErr := closer.Close()
		if flushErr == nil {
			flushErr = closeErr
		}
	}()

	path, err := compactedIndexPath(ctx, tenant, obj)
	if err != nil {
		return v2.ResultArtifact{}, fmt.Errorf("generating index path: %w", err)
	}
	if _, err := uploadObject(ctx, c.bucket, path, obj); err != nil {
		return v2.ResultArtifact{}, fmt.Errorf("uploading index %q: %w", path, err)
	}
	artifact = v2.ResultArtifact{Path: path}
	if !timeRange.MinTime.IsZero() {
		artifact.MinTime, artifact.MaxTime = timeRange.MinTime.UTC(), timeRange.MaxTime.UTC()
	}
	return artifact, nil
}

func compactedIndexPath(ctx context.Context, tenant string, obj *dataobj.Object) (path string, returnErr error) {
	reader, err := obj.Reader(ctx)
	if err != nil {
		return "", err
	}
	defer util.CloseAndHandleError(reader, &returnErr)
	return v2.CompactedIndexPath(tenant, reader)
}
