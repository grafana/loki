package dataset

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/columnar/types"
	"github.com/grafana/loki/v3/pkg/dataset/buffer"
	"github.com/grafana/loki/v3/pkg/dataset/layout"
	"github.com/grafana/loki/v3/pkg/memory"
)

// A Writer accumulates rows and flushes them into a [Dataset].
type Writer struct {
	alloc *memory.Allocator
	sink  buffer.Sink
	spec  Spec

	fields map[string]layout.Writer
}

// NewWriter creates a [Writer] for constructing a [Dataset] from the given
// [Spec].
//
// The provided alloc may be used to allocate memory across the entire Writer's
// lifetime. Callers must ensure that the allocator is valid for the entire
// lifetime of the Writer.
//
// Dynamic fields must be Struct arrays containing only UTF8 fields.
//
// NewWriter returns an error if spec contains duplicate field names.
func NewWriter(alloc *memory.Allocator, sink buffer.Sink, spec Spec) (*Writer, error) {
	fields := make(map[string]layout.Writer, len(spec.Fields))

	for _, field := range spec.Fields {
		if _, exist := fields[field.FieldName()]; exist {
			return nil, fmt.Errorf("duplicate field name: %s", field.FieldName())
		}

		switch f := field.(type) {
		case *StaticFieldSpec:
			w, err := layout.NewWriter(alloc, sink, f.Spec, f.Type)
			if err != nil {
				return nil, err
			}
			fields[f.Name] = w

		case *DynamicFieldSpec:
			fields[f.Name] = newDynamicWriter(alloc, sink, f)
		}
	}

	return &Writer{
		alloc: alloc,
		sink:  sink,
		spec:  spec,

		fields: fields,
	}, nil
}

// Append appends the rows in arr to the Writer. arr must be a struct array
// whose fields match the Writer's [Spec].
func (w *Writer) Append(ctx context.Context, arr columnar.Array) error {
	if got, want := arr.Kind(), types.KindStruct; got != want {
		return fmt.Errorf("got %s kind, wanted %s", got, want)
	}

	structArr := arr.(*columnar.Struct)
	structSchema := structArr.Schema()

	if err := w.validate(structSchema); err != nil {
		return err
	}

	var errs []error
	for fieldIdx := range structArr.NumFields() {
		field := structSchema.Column(fieldIdx)
		fieldArr := structArr.Field(fieldIdx)

		inner := w.fields[field.Name]
		if err := inner.Append(ctx, fieldArr); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (w *Writer) validate(s *columnar.Schema) error {
	var errs []error

	// We do top-level validation only; inner fields are validated by the field
	// writers.

	schemaFields := make(map[string]struct{}, s.NumColumns())
	for i := range s.NumColumns() {
		col := s.Column(i)
		schemaFields[col.Name] = struct{}{}

		// Check to make sure the field exists in w.
		if _, found := w.fields[col.Name]; !found {
			errs = append(errs, fmt.Errorf("struct field %s not found in dataset spec", col.Name))
		}
	}

	// Now check to make sure all the required fields were present.
	for _, reqField := range w.spec.Fields {
		reqName := reqField.FieldName()

		_, exist := schemaFields[reqName]
		if !exist {
			// This usually indicates a bug in application code where the
			// payload didn't have any keys for a dynamic field, and the caller
			// forgot to construct a struct{} (no fields) with the appropriate
			// length.
			errs = append(errs, fmt.Errorf("required field %s not found in input", reqName))
		}
	}

	return errors.Join(errs...)
}

// Flush flushes buffered data and returns a [Dataset] representing the
// encoded data.
func (w *Writer) Flush(ctx context.Context) (Dataset, error) {
	// The overall dataset is not nullable.
	var typ types.Struct
	var structLayout layout.Struct

	var errs []error

	// Flush all fields.
	for _, field := range w.spec.Fields {
		name := field.FieldName()

		fw, ok := w.fields[name]
		if !ok {
			errs = append(errs, fmt.Errorf("field %s not found in dataset spec", name))
			continue
		}
		fieldLayout, err := fw.Flush(ctx)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		structLayout.Fields = append(structLayout.Fields, fieldLayout)

		typ.Fields = append(typ.Fields, types.StructField{
			Name: name,
			Type: fieldLayout.DataType(),
		})
	}

	if len(errs) > 0 {
		return Dataset{}, errors.Join(errs...)
	}

	structLayout.Type = &typ
	return Dataset{Type: &typ, Layout: &structLayout}, nil
}

type dynamicWriter struct {
	alloc  *memory.Allocator
	spec   *DynamicFieldSpec
	sink   buffer.Sink
	fields map[string]layout.Writer
	rows   int
}

var _ layout.Writer = (*dynamicWriter)(nil)

func newDynamicWriter(alloc *memory.Allocator, sink buffer.Sink, spec *DynamicFieldSpec) *dynamicWriter {
	return &dynamicWriter{
		alloc:  alloc,
		spec:   spec,
		sink:   sink,
		fields: make(map[string]layout.Writer),
	}
}

func (dw *dynamicWriter) Append(ctx context.Context, arr columnar.Array) error {
	if got, want := arr.Kind(), types.KindStruct; got != want {
		return fmt.Errorf("dynamic field %s must be %s, got %s", dw.spec.Name, want, got)
	}

	structArr := arr.(*columnar.Struct)
	structSchema := structArr.Schema()
	defer func() { dw.rows += arr.Len() }()

	nullAlloc := memory.NewAllocator(dw.alloc)
	defer nullAlloc.Free()

	var errs []error
	gotFields := make(map[string]struct{}, structArr.NumFields())
	for fieldIndex := range structArr.NumFields() {
		field := structSchema.Column(fieldIndex)
		fieldArr := structArr.Field(fieldIndex)

		if got, want := fieldArr.Kind(), types.KindUTF8; got != want {
			errs = append(errs, fmt.Errorf("dynamic field %s.%s must be %s, got %s", dw.spec.Name, field.Name, want, got))
			continue
		}

		if _, ok := dw.fields[field.Name]; !ok {
			typ := fieldArr.Type()
			spec := dw.spec.GetSpec(field.Name, typ)
			if spec == nil {
				errs = append(errs, fmt.Errorf("GetSpec returned nil for dynamic field %s.%s", dw.spec.Name, field.Name))
				continue
			}
			l, err := layout.NewWriter(dw.alloc, dw.sink, spec, typ)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			if dw.rows > 0 {
				if err := l.Append(ctx, newNullUTF8(nullAlloc, dw.rows)); err != nil {
					errs = append(errs, err)
					continue
				}
			}
			dw.fields[field.Name] = l
		}

		gotFields[field.Name] = struct{}{}

		inner := dw.fields[field.Name]
		if err := inner.Append(ctx, fieldArr); err != nil {
			errs = append(errs, err)
		}
	}

	if len(gotFields) == len(dw.fields) {
		// We appended all the known fields, so we can stop early.
		return errors.Join(errs...)
	}

	// There are some fields which weren't seen in the incoming array, so we
	// have to pad them with NULLs.
	//
	// TODO(rfratto): Since we require all dynamic fields to be strings, we can
	// use a UTF8Builder to construct the null array. This should ideally be a
	// columnar.Null, but this would require updating all of the layout.Writer
	// and array.Writer implementations to support type coersion first.
	nullArr := newNullUTF8(nullAlloc, arr.Len())

	for fieldName := range dw.fields {
		if _, ok := gotFields[fieldName]; ok {
			continue
		}
		if err := dw.fields[fieldName].Append(ctx, nullArr); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func newNullUTF8(alloc *memory.Allocator, rows int) *columnar.UTF8 {
	builder := columnar.NewUTF8Builder(alloc)
	builder.AppendNulls(rows)
	return builder.Build()
}

func (dw *dynamicWriter) Flush(ctx context.Context) (layout.Layout, error) {
	if len(dw.fields) == 0 && dw.rows > 0 {
		return nil, fmt.Errorf("dynamic field %s has %d rows but no fields", dw.spec.Name, dw.rows)
	}

	names := slices.Collect(maps.Keys(dw.fields))
	slices.Sort(names)

	var (
		typ types.Struct
		l   layout.Struct
	)

	var errs []error
	for _, name := range names {
		fieldLayout, err := dw.fields[name].Flush(ctx)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		typ.Fields = append(typ.Fields, types.StructField{
			Name: name,
			Type: &types.UTF8{Nullable: true},
		})
		l.Fields = append(l.Fields, fieldLayout)
	}

	l.Type = &typ
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return &l, nil
}
