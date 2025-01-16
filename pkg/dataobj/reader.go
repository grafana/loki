package dataobj

import (
	"context"
	"errors"
	"fmt"

	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/encoding"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/sections/streams"
	"github.com/grafana/loki/v3/pkg/logproto"
)

// reader connects to an object storage bucket and supports basic reading from
// data objects.
//
// reader isn't exposed as a public API because it's insufficient for reading
// at scale; more work is needed to support efficient reads and filtering data.
// At the moment, reader is only used for tests.
type reader struct {
	bucket objstore.Bucket
}

func newReader(bucket objstore.Bucket) *reader {
	return &reader{bucket: bucket}
}

// Objects returns an iterator over all data objects for the provided tenant.
func (r *reader) Objects(ctx context.Context, tenant string) result.Seq[string] {
	tenantPath := fmt.Sprintf("tenant-%s/objects/", tenant)

	return result.Iter(func(yield func(string) bool) error {
		errIterationStopped := errors.New("iteration stopped")

		err := r.bucket.Iter(ctx, tenantPath, func(name string) error {
			if !yield(name) {
				return errIterationStopped
			}
			return nil
		}, objstore.WithRecursiveIter())

		switch {
		case errors.Is(err, errIterationStopped):
			return nil
		default:
			return err
		}
	})
}

// Streams returns an iterator over all [logproto.Stream] entries for the
// provided object. Each emitted stream contains all logs for that stream in
// ascending timestamp order. Streams are emitted in in the order they were
// first appended to the data object.
func (r *reader) Streams(ctx context.Context, object string) result.Seq[logproto.Stream] {
	return result.Iter(func(yield func(logproto.Stream) bool) error {
		dec := encoding.BucketDecoder(r.bucket, object)

		streamRecords, err := result.Collect(streams.Iter(ctx, dec))
		if err != nil {
			return fmt.Errorf("reading streams dataset: %w", err)
		}
		streamRecordLookup := make(map[int64]streams.Stream, len(streamRecords))
		for _, stream := range streamRecords {
			streamRecordLookup[stream.ID] = stream
		}

		var (
			lastID int64
			batch  logproto.Stream
		)

		for result := range logs.Iter(ctx, dec) {
			record, err := result.Value()
			if err != nil {
				return fmt.Errorf("iterating over logs: %w", err)
			}

			if lastID != record.StreamID {
				if lastID != 0 && !yield(batch) {
					return nil
				}

				streamRecord := streamRecordLookup[record.StreamID]

				batch = logproto.Stream{
					Labels: streamRecord.Labels.String(),
					Hash:   streamRecord.Labels.Hash(),
				}

				lastID = record.StreamID
			}

			batch.Entries = append(batch.Entries, logproto.Entry{
				Timestamp:          record.Timestamp,
				Line:               record.Line,
				StructuredMetadata: record.Metadata,
			})
		}
		if len(batch.Entries) > 0 {
			if !yield(batch) {
				return nil
			}
		}

		return nil
	})
}
