package executor

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/grafana/loki/v3/pkg/dataobj"
	dataobjindex "github.com/grafana/loki/v3/pkg/dataobj/index"
)

type indexPathResolver func(context.Context, *dataobj.Object) (string, error)

// uploadAndIndexObject uploads a logs object, adds it to calc, and closes its
// backing resources.
func (c *Context) uploadAndIndexObject(
	ctx context.Context,
	obj *dataobj.Object,
	closer io.Closer,
	uploadDestination string,
	calc *dataobjindex.Calculator,
) (int64, error) {
	size, err := c.uploadObject(ctx, c.dataObjectBucket(), uploadDestination, obj)
	if err != nil {
		return 0, errors.Join(fmt.Errorf("uploading %q: %w", uploadDestination, err), closer.Close())
	}
	if err := calc.Calculate(ctx, c.logger, obj, uploadDestination); err != nil {
		return 0, errors.Join(fmt.Errorf("indexing %q: %w", uploadDestination, err), closer.Close())
	}
	if err := closer.Close(); err != nil {
		return 0, fmt.Errorf("closing object %q: %w", uploadDestination, err)
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
	path, err := resolvePath(ctx, obj)
	if err != nil {
		return "", errors.Join(fmt.Errorf("generating index path: %w", err), closer.Close())
	}
	if _, err := c.uploadObject(ctx, c.bucket, path, obj); err != nil {
		return "", errors.Join(fmt.Errorf("uploading index %q: %w", path, err), closer.Close())
	}
	if err := closer.Close(); err != nil {
		return "", fmt.Errorf("closing index %q: %w", path, err)
	}
	return path, nil
}
