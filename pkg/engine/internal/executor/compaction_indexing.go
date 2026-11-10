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
	size, err := c.uploadObject(ctx, c.dataObjectBucket(), uploadDestination, obj)
	if err != nil {
		return 0, fmt.Errorf("uploading %q: %w", uploadDestination, err)
	}
	if err := calc.Calculate(ctx, c.logger, obj, uploadDestination); err != nil {
		return 0, fmt.Errorf("indexing %q: %w", uploadDestination, err)
	}
	return size, nil
}

// flushAndUploadIndex flushes calc, uploads the index to the content-addressed
// compacted index path of tenant, and releases the flushed object.
func (c *Context) flushAndUploadIndex(
	ctx context.Context,
	tenant string,
	calc *dataobjindex.Calculator,
) (output string, flushErr error) {
	obj, closer, _, err := calc.Flush()
	if err != nil {
		return "", fmt.Errorf("flushing index: %w", err)
	}
	defer func() {
		closeErr := closer.Close()
		if flushErr == nil {
			flushErr = closeErr
		}
	}()

	path, err := compactedIndexPath(ctx, tenant, obj)
	if err != nil {
		return "", fmt.Errorf("generating index path: %w", err)
	}
	if _, err := c.uploadObject(ctx, c.bucket, path, obj); err != nil {
		return "", fmt.Errorf("uploading index %q: %w", path, err)
	}
	return path, nil
}

func compactedIndexPath(ctx context.Context, tenant string, obj *dataobj.Object) (path string, returnErr error) {
	reader, err := obj.Reader(ctx)
	if err != nil {
		return "", err
	}
	defer util.CloseAndHandleError(reader, &returnErr)
	return v2.CompactedIndexPath(tenant, reader)
}
