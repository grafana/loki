package generic

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
)

// Iter iterates over records in the provided decoder. All logs sections are
// iterated over in order.
// Results objects returned to yield may be reused and must be copied for further use via DeepCopy().
func Iter(ctx context.Context, obj *dataobj.Object, kind string) result.Seq[arrow.RecordBatch] {
	return result.Iter(func(yield func(arrow.RecordBatch) bool) error {
		for i, section := range obj.Sections().Filter(CheckSection(kind)) {
			logsSection, err := Open(ctx, section, kind)
			if err != nil {
				return fmt.Errorf("opening section %d: %w", i, err)
			}

			for result := range IterSection(ctx, logsSection, kind) {
				if result.Err() != nil || !yield(result.MustValue()) {
					return result.Err()
				}
			}
		}

		return nil
	})
}

func IterSection(ctx context.Context, section *Section, kind string) result.Seq[arrow.RecordBatch] {
	return result.Iter(func(yield func(arrow.RecordBatch) bool) error {
		// Use the Reader to read Arrow RecordBatches directly
		reader := NewReader(ReaderOptions{
			Columns: section.Columns(),
		})
		defer reader.Close()

		const batchSize = 1024 // Read up to 1024 rows at a time

		for {
			record, err := reader.Read(ctx, batchSize)
			if err != nil && !errors.Is(err, io.EOF) {
				return err
			}

			// If we got a record, yield it
			if record != nil {
				if !yield(record) {
					// Caller stopped iteration, release the record and return
					record.Release()
					return nil
				}
				// Release the record after yielding
				record.Release()
			}

			// Check for EOF
			if errors.Is(err, io.EOF) {
				return nil
			}
		}
	})
}

