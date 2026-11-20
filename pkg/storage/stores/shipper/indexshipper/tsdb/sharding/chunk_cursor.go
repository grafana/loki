package sharding

import (
	"context"
	"errors"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/logproto"
)

// ErrChunkCursorsUnsupported means a source cannot provide ordered chunk cursors.
var ErrChunkCursorsUnsupported = errors.New("index does not support ordered chunk cursors")

// ChunkCursor walks one file in order by fingerprint, start time, end time,
// and checksum. At returns a value that remains valid after the next call
// to Next.
type ChunkCursor interface {
	Next() bool
	At() logproto.ChunkRefWithSizingInfo
	Err() error
	Close() error
}

// ChunkCursorSource opens cursors over the indexes matching a shard request.
// On success the caller owns and must close the cursors. On failure the source
// closes every cursor it opened.
type ChunkCursorSource interface {
	OpenChunkCursors(ctx context.Context, tenant string, from, through model.Time, matchers ...*labels.Matcher) ([]ChunkCursor, error)
}

// CloseChunkCursors releases all the supplied cursors.
func CloseChunkCursors(cursors []ChunkCursor) error {
	var err error
	for _, cursor := range cursors {
		if cursor != nil {
			err = errors.Join(err, cursor.Close())
		}
	}
	return err
}
