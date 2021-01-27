package bucket

import (
	"context"
	"strings"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/thanos-io/thanos/pkg/objstore"
)

// DeletePrefix removes all objects with given prefix, recursively.
// It returns number of deleted objects.
// If deletion of any object fails, it returns error and stops.
func DeletePrefix(ctx context.Context, bkt objstore.Bucket, prefix string, logger log.Logger) (int, error) {
	result := 0
	err := bkt.Iter(ctx, prefix, func(name string) error {
		if strings.HasSuffix(name, objstore.DirDelim) {
			deleted, err := DeletePrefix(ctx, bkt, name, logger)
			result += deleted
			return err
		}

		if err := bkt.Delete(ctx, name); err != nil {
			return err
		}
		result++
		level.Debug(logger).Log("msg", "deleted file", "file", name)
		return nil
	})

	return result, err
}
