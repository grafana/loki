package dataset_test

import (
	"context"
	"errors"
	"math"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/columnar/columnartest"
	"github.com/grafana/loki/v3/pkg/columnar/types"
	"github.com/grafana/loki/v3/pkg/dataset"
	"github.com/grafana/loki/v3/pkg/dataset/array"
	"github.com/grafana/loki/v3/pkg/dataset/buffer"
	"github.com/grafana/loki/v3/pkg/dataset/layout"
	"github.com/grafana/loki/v3/pkg/expr"
	"github.com/grafana/loki/v3/pkg/memory"
)

func Test(t *testing.T) {
	var alloc memory.Allocator
	var store buffer.MemoryStore

	type Record struct {
		Timestamp int64
		Metadata  map[string]string
		Line      string
	}

	spec := testSpec()

	records := []Record{
		{Timestamp: 1000, Metadata: map[string]string{"service": "web", "env": "prod"}, Line: "hello world"},
		{Timestamp: 2000, Metadata: map[string]string{"service": "api"}, Line: "request started"},
		{Timestamp: 3000, Metadata: map[string]string{"env": "dev"}, Line: "debug info"},
		{Timestamp: 4000, Metadata: map[string]string{}, Line: "no metadata"},
		{Timestamp: 5000, Metadata: map[string]string{"service": "db", "env": "staging"}, Line: "query executed"},
	}

	w, err := dataset.NewWriter(&alloc, &store, spec)
	require.NoError(t, err)

	ctx := t.Context()
	for _, record := range records {
		rec := buildRecord(t, &alloc, record.Timestamp, record.Metadata, record.Line)
		err := w.Append(ctx, rec)
		require.NoError(t, err)
	}

	dset, err := w.Flush(ctx)
	require.NoError(t, err)

	r, err := dataset.NewReader(&alloc, &store, dataset.ReaderOptions{
		Dataset: dset,

		Projection: &expr.Include{
			Names: []string{"timestamp", "metadata", "line"},
			Value: &expr.Identity{},
		},

		Filter: &expr.Binary{
			Left:  &expr.Column{Name: "timestamp"},
			Op:    expr.BinaryOpGTE,
			Right: &expr.Constant{Value: &columnar.NumberScalar[int64]{Value: 2000}},
		},

		Mask: memory.Bitmap{}, // No initial mask.
	})
	require.NoError(t, err)

	err = r.Open(ctx)
	require.NoError(t, err)
	defer r.Close()

	actual, err := r.Read(ctx, &alloc, math.MaxInt)
	require.NoError(t, err)

	// After filtering timestamp >= 2000, we expect records 2-5.
	// Dynamic metadata keys are [env, service] (sorted). Missing keys are NULL.
	expected := columnartest.Struct(t, &alloc,
		columnartest.Field("timestamp", types.KindInt64, int64(2000), int64(3000), int64(4000), int64(5000)),
		columnartest.Field("metadata", types.KindStruct,
			columnartest.Struct(t, &alloc,
				columnartest.Field("env", types.KindUTF8, nil, "dev", nil, "staging"),
				columnartest.Field("service", types.KindUTF8, "api", nil, nil, "db"),
			),
		),
		columnartest.Field("line", types.KindUTF8, "request started", "debug info", "no metadata", "query executed"),
	)

	columnartest.RequireArraysEqual(t, expected, actual, memory.Bitmap{})
}

func buildRecord(t *testing.T, alloc *memory.Allocator, ts int64, metadata map[string]string, line string) *columnar.Struct {
	t.Helper()

	tsBuilder := columnar.NewNumberBuilder[int64](alloc)
	tsBuilder.AppendValue(ts)

	lineBuilder := columnar.NewUTF8Builder(alloc)
	lineBuilder.AppendValue([]byte(line))

	outerSchema := columnar.NewSchema([]columnar.Column{
		{Name: "timestamp"},
		{Name: "metadata"},
		{Name: "line"},
	})
	return columnar.NewStruct(outerSchema, []columnar.Array{
		tsBuilder.Build(),
		buildMetadata(t, alloc, metadata),
		lineBuilder.Build(),
	}, 1, memory.Bitmap{})
}
func testSpec() dataset.Spec {
	return dataset.Spec{
		Fields: []dataset.FieldSpec{
			&dataset.StaticFieldSpec{
				Name: "timestamp",
				Type: &types.Int64{Nullable: false},
				Spec: &layout.SpecChunked{
					MaxRowCount: 2,
					Chunk: &layout.SpecArray{
						Spec: &array.SpecPlain{},
					},
				},
			},
			&dataset.DynamicFieldSpec{
				Name: "metadata",
				GetSpec: func(_ string, _ types.Type) layout.Spec {
					return &layout.SpecChunked{
						MaxRowCount: 2,
						Chunk: &layout.SpecArray{
							Spec: &array.SpecBinary{
								Validity: &array.SpecBool{},
								Offsets:  &array.SpecPlain{},
							},
						},
					}
				},
			},
			&dataset.StaticFieldSpec{
				Name: "line",
				Type: &types.UTF8{Nullable: false},
				Spec: &layout.SpecChunked{
					MaxRowCount: 2,
					Chunk: &layout.SpecArray{
						Spec: &array.SpecBinary{
							Offsets: &array.SpecPlain{},
						},
					},
				},
			},
		},
	}
}

func writeDataset(t *testing.T, alloc *memory.Allocator, sink buffer.Sink, rows ...columnar.Array) dataset.Dataset {
	t.Helper()

	w, err := dataset.NewWriter(alloc, sink, testSpec())
	require.NoError(t, err)
	for _, row := range rows {
		require.NoError(t, w.Append(t.Context(), row))
	}
	dset, err := w.Flush(t.Context())
	require.NoError(t, err)
	return dset
}

func readDataset(t *testing.T, alloc *memory.Allocator, source buffer.Source, options dataset.ReaderOptions) (columnar.Array, error) {
	t.Helper()

	r, err := dataset.NewReader(alloc, source, options)
	require.NoError(t, err)
	require.NoError(t, r.Open(t.Context()))
	t.Cleanup(func() { require.NoError(t, r.Close()) })
	return r.Read(t.Context(), alloc, math.MaxInt)
}

func buildMetadata(t *testing.T, alloc *memory.Allocator, metadata map[string]string) *columnar.Struct {
	t.Helper()

	keys := slices.Sorted(func(yield func(string) bool) {
		for key := range metadata {
			if !yield(key) {
				return
			}
		}
	})

	columns := make([]columnar.Column, len(keys))
	fields := make([]columnar.Array, len(keys))
	for i, key := range keys {
		columns[i] = columnar.Column{Name: key}
		builder := columnar.NewUTF8Builder(alloc)
		builder.AppendValue([]byte(metadata[key]))
		fields[i] = builder.Build()
	}
	return columnar.NewStruct(columnar.NewSchema(columns), fields, 1, memory.Bitmap{})
}

type errorSink struct{}

func (errorSink) WriteBuffers(context.Context, []buffer.Data) ([]buffer.ID, error) {
	return nil, errors.New("writing buffers")
}
