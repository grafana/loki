package worker

import (
	"errors"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/executor"
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
	"github.com/grafana/loki/v3/pkg/util/arrowtest"
)

// encodeRecords encodes one or more Arrow record batches into a single cache payload.
func encodeRecords(t *testing.T, recs ...arrow.RecordBatch) []byte {
	t.Helper()
	enc := executor.NewCacheEntryEncoder("")
	for _, rec := range recs {
		require.NoError(t, enc.Append(rec))
	}
	payload, err := enc.Commit()
	require.NoError(t, err)
	return payload
}

// drainNodeSource reads all records from a nodeSource until EOF or error.
func drainNodeSource(t *testing.T, src *nodeSource) ([]arrow.RecordBatch, error) {
	t.Helper()
	var records []arrow.RecordBatch
	for {
		rec, err := src.Read(t.Context())
		if errors.Is(err, executor.EOF) {
			return records, nil
		}
		if err != nil {
			return records, err
		}
		records = append(records, rec)
	}
}

func TestDrainCachedSources(t *testing.T) {
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "a", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		{Name: "b", Type: arrow.BinaryTypes.String, Nullable: true},
	}, nil)

	makeRec := func(a int64) arrow.RecordBatch {
		return arrowtest.Rows{{"a": a, "b": "v"}}.Record(memory.DefaultAllocator, schema)
	}

	tests := []struct {
		name     string
		srcs     workflow.CachedSources
		wantRecs int
		wantErr  string
	}{
		{
			name:     "single buffer with two records",
			srcs:     workflow.CachedSources{encodeRecords(t, makeRec(1), makeRec(2))},
			wantRecs: 2,
		},
		{
			name:     "multiple buffers each with one record",
			srcs:     workflow.CachedSources{encodeRecords(t, makeRec(10)), encodeRecords(t, makeRec(20))},
			wantRecs: 2,
		},
		{
			name:    "corrupt buffer causes decode error",
			srcs:    workflow.CachedSources{[]byte("not valid arrow data")},
			wantErr: "decode failed",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			input := new(nodeSource)
			input.Add(1)
			go drainCachedSources(t.Context(), input, tc.srcs, log.NewNopLogger())

			got, err := drainNodeSource(t, input)

			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
				require.Len(t, got, tc.wantRecs)
			}
		})
	}
}
