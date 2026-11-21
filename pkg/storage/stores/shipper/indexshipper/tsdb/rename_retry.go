package tsdb

import (
	"context"
	"fmt"
	"time"
)

const (
	renameAttempts = 5
	renameBackoff  = 50 * time.Millisecond
)

// renameWithRetry renames src to dst, retrying a bounded number of times while
// retriable reports the failure as transient. On Windows a freshly closed or
// unmapped file can stay locked for a short while (e.g. antivirus or indexer
// scanning it), which makes an immediate rename fail with "Access is denied".
// The delay doubles after every failed attempt.
func renameWithRetry(
	ctx context.Context,
	rename func(oldpath, newpath string) error,
	retriable func(error) bool,
	attempts int,
	delay time.Duration,
	src, dst string,
) error {
	var err error
	for i := 0; i < attempts; i++ {
		if err = rename(src, dst); err == nil {
			return nil
		}
		if !retriable(err) || i == attempts-1 {
			break
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("rename %s to %s: %w (retry aborted: %w)", src, dst, err, ctx.Err())
		case <-time.After(delay):
			delay *= 2
		}
	}
	return fmt.Errorf("rename %s to %s: %w", src, dst, err)
}
