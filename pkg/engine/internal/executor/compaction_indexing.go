package executor

import (
	"context"
	"errors"
	"fmt"

	"github.com/grafana/loki/v3/pkg/dataobj"
	dataobjindex "github.com/grafana/loki/v3/pkg/dataobj/index"
)

type indexPathResolver func(context.Context, *dataobj.Object) (string, error)

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

// flushAndUploadIndex flushes calc, resolves the content-addressed index path,
// uploads the index, and closes its backing resources.
func (c *Context) flushAndUploadIndex(
	ctx context.Context,
	calc *dataobjindex.Calculator,
	resolvePath indexPathResolver,
) (string, error) {
	obj, closer, _, err := calc.Flush()
	if err != nil {
		return "", fmt.Errorf("flushing index: %w", err)
	}
	defer func() {
		_ = closer.Close()
	}()

	path, err := resolvePath(ctx, obj)
	if err != nil {
		return "", errors.Join(fmt.Errorf("generating index path: %w", err), closer.Close())
	}
	if _, err := c.uploadObject(ctx, c.bucket, path, obj); err != nil {
		return "", errors.Join(fmt.Errorf("uploading index %q: %w", path, err), closer.Close())
	}
	return path, nil
}
