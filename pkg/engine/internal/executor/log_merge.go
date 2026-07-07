package executor

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/go-kit/log/level"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
)

func (c *Context) executeLogMerge(node *physical.LogMerge) Pipeline {
	return newLazyPipeline(func(ctx context.Context, _ []Pipeline) Pipeline {
		if err := c.doLogObjectMerge(ctx, node); err != nil {
			return errorPipeline(ctx, err)
		}
		return emptyPipeline()
	}, nil)
}

func (c *Context) doLogObjectMerge(ctx context.Context, node *physical.LogMerge) error {
	if c.bucket == nil {
		return errors.New("no object store bucket configured")
	}

	exists, err := c.outputExists(ctx, node.OutputIndexPath)
	if err != nil {
		return fmt.Errorf("checking output existence: %w", err)
	}
	if exists {
		level.Info(c.logger).Log("msg", "LogMerge: output already exists, short-circuiting", "path", node.OutputIndexPath)
		return nil
	}

	if err := c.bucket.Upload(ctx, node.OutputIndexPath, bytes.NewReader(nil)); err != nil {
		return fmt.Errorf("uploading merged index: %w", err)
	}

	return nil
}
