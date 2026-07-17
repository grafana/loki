package layout

import (
	"context"
	"fmt"
	"io"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/columnar/types"
	"github.com/grafana/loki/v3/pkg/compute"
	"github.com/grafana/loki/v3/pkg/dataset/buffer"
	"github.com/grafana/loki/v3/pkg/expr"
	"github.com/grafana/loki/v3/pkg/memory"
)

// structWriter writes struct-typed data by delegating each field to a child Writer.
type structWriter struct {
	alloc    *memory.Allocator
	typ      *types.Struct
	fields   []Writer
	validity Writer // nil when non-nullable.
}

func newStructWriter(alloc *memory.Allocator, sink buffer.Sink, spec Spec, typ types.Type) (*structWriter, error) {
	if got, want := spec.Kind(), KindStruct; got != want {
		return nil, fmt.Errorf("invalid spec kind for struct writer: got %s, want %s", got, want)
	}

	structSpec := spec.(*SpecStruct)
	structType := typ.(*types.Struct)

	if len(structSpec.Fields) != len(structType.Fields) {
		return nil, fmt.Errorf("spec has %d fields, type has %d", len(structSpec.Fields), len(structType.Fields))
	}

	hasValidity := structSpec.Validity != nil
	if structType.Nullable != hasValidity {
		return nil, fmt.Errorf("expected %s to have validity %t, got %t", structType, structType.Nullable, hasValidity)
	}

	fields := make([]Writer, len(structSpec.Fields))
	for i, fieldSpec := range structSpec.Fields {
		w, err := NewWriter(alloc, sink, fieldSpec, structType.Fields[i].Type)
		if err != nil {
			return nil, fmt.Errorf("creating writer for field %q: %w", structType.Fields[i].Name, err)
		}
		fields[i] = w
	}

	var validity Writer
	if hasValidity {
		w, err := NewWriter(alloc, sink, structSpec.Validity, &types.Bool{})
		if err != nil {
			return nil, fmt.Errorf("creating validity writer: %w", err)
		}
		validity = w
	}

	return &structWriter{
		alloc:    alloc,
		typ:      structType,
		fields:   fields,
		validity: validity,
	}, nil
}

func (w *structWriter) Append(ctx context.Context, arr columnar.Array) error {
	s, ok := arr.(*columnar.Struct)
	if !ok {
		return fmt.Errorf("expected *columnar.Struct, got %T", arr)
	}
	validity := s.Validity()
	if w.validity == nil && validity.Len() > 0 {
		return fmt.Errorf("cannot append struct validity to a non-nullable writer")
	}

	for i, fw := range w.fields {
		if err := fw.Append(ctx, s.Field(i)); err != nil {
			return fmt.Errorf("appending field %q: %w", w.typ.Fields[i].Name, err)
		}
	}

	// Write validity bitmap as a Bool array.
	if w.validity != nil {
		var boolArr *columnar.Bool
		if validity.Len() == 0 {
			// All valid: create an all-true Bool array.
			builder := columnar.NewBoolBuilder(w.alloc)
			builder.AppendValueCount(true, s.Len())
			boolArr = builder.Build()
		} else {
			boolArr = columnar.NewBool(validity, memory.Bitmap{})
		}
		if err := w.validity.Append(ctx, boolArr); err != nil {
			return fmt.Errorf("appending validity: %w", err)
		}
	}

	return nil
}

func (w *structWriter) Flush(ctx context.Context) (Layout, error) {
	fields := make([]Layout, len(w.fields))
	for i, fw := range w.fields {
		l, err := fw.Flush(ctx)
		if err != nil {
			return nil, fmt.Errorf("flushing field %q: %w", w.typ.Fields[i].Name, err)
		}
		fields[i] = l
	}

	var validity Layout
	if w.validity != nil {
		l, err := w.validity.Flush(ctx)
		if err != nil {
			return nil, fmt.Errorf("flushing validity: %w", err)
		}
		validity = l
	}

	return &Struct{
		Type:     w.typ,
		Fields:   fields,
		Validity: validity,
	}, nil
}

// structReader reads struct-typed data by delegating to child readers.
type structReader struct {
	typ      *types.Struct
	fields   []Reader
	validity Reader // nil when non-nullable.

	totalRows int
	offset    int
	length    int
}

func newStructReader(alloc *memory.Allocator, layout Layout, source buffer.Source) (*structReader, error) {
	if got, want := layout.Kind(), KindStruct; got != want {
		return nil, fmt.Errorf("invalid layout kind for struct reader: got %s, want %s", got, want)
	}

	sl := layout.(*Struct)
	st := sl.Type.(*types.Struct)

	fields := make([]Reader, len(sl.Fields))
	for i, fl := range sl.Fields {
		r, err := NewReader(alloc, fl, source)
		if err != nil {
			return nil, fmt.Errorf("creating reader for field %q: %w", st.Fields[i].Name, err)
		}
		fields[i] = r
	}

	var validity Reader
	if sl.Validity != nil {
		r, err := NewReader(alloc, sl.Validity, source)
		if err != nil {
			return nil, fmt.Errorf("creating validity reader: %w", err)
		}
		validity = r
	}

	return &structReader{
		typ:       st,
		fields:    fields,
		validity:  validity,
		totalRows: sl.Len(),
	}, nil
}

func (r *structReader) Next(ctx context.Context, batchSize int) (int, error) {
	r.offset += r.length

	if r.offset >= r.totalRows {
		r.length = 0
		return 0, io.EOF
	}

	r.length = min(batchSize, r.totalRows-r.offset)

	for i, fr := range r.fields {
		if _, err := fr.Next(ctx, r.length); err != nil {
			return 0, fmt.Errorf("advancing field %q: %w", r.typ.Fields[i].Name, err)
		}
	}

	if r.validity != nil {
		if _, err := r.validity.Next(ctx, r.length); err != nil {
			return 0, fmt.Errorf("advancing validity: %w", err)
		}
	}

	return r.length, nil
}

func (r *structReader) AppendBuffers(buf []buffer.ID, filter expr.Expression, projection expr.Expression, mask memory.Bitmap) ([]buffer.ID, error) {
	fieldNames := make([]string, len(r.typ.Fields))
	for i, f := range r.typ.Fields {
		fieldNames[i] = f.Name
	}

	// Build per-field projection sub-expressions from the simplified tree.
	// After Simplify, the projection is a MakeStruct whose values are the
	// sub-expressions for each referenced field.
	simplified := simplifyProjection(projection, fieldNames)
	childProj := make(map[string]expr.Expression)
	if ms, ok := simplified.(*expr.MakeStruct); ok {
		for i, name := range ms.Names {
			childProj[name] = rewriteColumnsToIdentity(ms.Values[i], name)
		}
	}

	// Simplify the filter so nested-struct shapes (Identity, Extract over
	// MakeStruct, Include, Exclude) collapse into plain Column references.
	// Without this, collectColumns below would miss the field references and
	// under-count the buffers reachable through the filter.
	filter = simplifyProjection(filter, fieldNames)

	// Collect fields referenced by the filter.
	filterCols := make(map[string]struct{})
	if filter != nil {
		filterCols = collectColumns(filter)
	}

	// Union of projection and filter field references.
	needed := make(map[string]struct{})
	for name := range childProj {
		needed[name] = struct{}{}
	}
	for name := range collectColumns(simplified) {
		needed[name] = struct{}{}
	}
	for name := range filterCols {
		needed[name] = struct{}{}
	}

	// Iterate in struct field order so buffer collection is deterministic.
	for i, f := range r.typ.Fields {
		if _, ok := needed[f.Name]; !ok {
			continue
		}
		proj, ok := childProj[f.Name]
		if !ok {
			// The expression could not be decomposed into a projection for this
			// child. Use Identity to collect all of its buffers.
			proj = &expr.Identity{}
		}
		var err error
		buf, err = r.fields[i].AppendBuffers(buf, filter, proj, mask)
		if err != nil {
			return buf, fmt.Errorf("field %q: %w", f.Name, err)
		}
	}

	if r.validity != nil {
		var err error
		buf, err = r.validity.AppendBuffers(buf, filter, &expr.Identity{}, mask)
		if err != nil {
			return buf, fmt.Errorf("validity: %w", err)
		}
	}

	return buf, nil
}

func (r *structReader) Prune(ctx context.Context, alloc *memory.Allocator, e expr.Expression, mask memory.Bitmap) (memory.Bitmap, error) {
	if mask.Len() != 0 && mask.Len() != r.length {
		return memory.Bitmap{}, fmt.Errorf("mask length %d does not match row range %d", mask.Len(), r.length)
	}
	fieldNames := make([]string, len(r.typ.Fields))
	for i, f := range r.typ.Fields {
		fieldNames[i] = f.Name
	}
	simplified := simplifyProjection(e, fieldNames)
	return r.walkFilter(ctx, alloc, simplified, mask, pruneOp)
}

func (r *structReader) Filter(ctx context.Context, alloc *memory.Allocator, e expr.Expression, mask memory.Bitmap) (memory.Bitmap, error) {
	if mask.Len() != 0 && mask.Len() != r.length {
		return memory.Bitmap{}, fmt.Errorf("mask length %d does not match row range %d", mask.Len(), r.length)
	}

	fieldNames := make([]string, len(r.typ.Fields))
	for i, f := range r.typ.Fields {
		fieldNames[i] = f.Name
	}
	simplified := simplifyProjection(e, fieldNames)

	result, err := r.walkFilter(ctx, alloc, simplified, mask, filterOp)
	if err != nil {
		return memory.Bitmap{}, err
	}

	if r.validity != nil {
		vArr, err := r.validity.Project(ctx, alloc, &expr.Identity{}, mask)
		if err != nil {
			return memory.Bitmap{}, fmt.Errorf("projecting validity for filter: %w", err)
		}
		vMask := vArr.(*columnar.Bool).Values()
		if result.Len() == 0 {
			result = vMask
		} else {
			result = intersectMasks(alloc, result, vMask)
		}
	}

	return result, nil
}

func (r *structReader) walkFilter(ctx context.Context, alloc *memory.Allocator, e expr.Expression, mask memory.Bitmap, op decomposeOp) (memory.Bitmap, error) {
	if e == nil {
		return mask, nil
	}

	// AND/OR get special treatment for short-circuit evaluation.
	if bin, ok := e.(*expr.Binary); ok {
		switch bin.Op {
		case expr.BinaryOpAND:
			leftMask, err := r.walkFilter(ctx, alloc, bin.Left, mask, op)
			if err != nil {
				return memory.Bitmap{}, err
			}
			if leftMask.Len() > 0 && leftMask.SetCount() == 0 {
				return leftMask, nil
			}
			return r.walkFilter(ctx, alloc, bin.Right, leftMask, op)

		case expr.BinaryOpOR:
			leftMask, err := r.walkFilter(ctx, alloc, bin.Left, mask, op)
			if err != nil {
				return memory.Bitmap{}, err
			}
			if leftMask.Len() > 0 && leftMask.SetCount() == leftMask.Len() {
				return leftMask, nil
			}
			rightMask, err := r.walkFilter(ctx, alloc, bin.Right, mask, op)
			if err != nil {
				return memory.Bitmap{}, err
			}
			return unionMasks(alloc, leftMask, rightMask, r.length), nil
		}
	}

	// Annotation-based push-down.
	cols := collectColumns(e)
	if len(cols) == 1 {
		var colName string
		for name := range cols {
			colName = name
		}
		idx := r.fieldIndex(colName)
		if idx < 0 {
			// Unknown columns can't be pushed down.
			goto Fallback
		}
		rewritten := rewriteColumnsToIdentity(e, colName)
		return op.call(ctx, r.fields[idx], alloc, rewritten, mask)
	}

Fallback:
	// Multi-column or zero-column expression that can't be decomposed
	// into single-field sub-expressions (e.g., a + b < 5).
	//
	// Prune returns the mask unchanged here. Cross-column pruning would
	// require aligned per-zone statistics across columns (zone maps) to
	// reason about combined expressions. The current layout has
	// independent per-column chunking with no alignment guarantee, so
	// cross-column metadata reasoning is not possible at this level.
	//
	// Filter falls back to projecting the referenced columns and
	// evaluating the full expression.
	if op == pruneOp {
		return mask, nil
	}
	return r.evalFallbackFilter(ctx, alloc, e, mask)
}

// decomposeOp selects whether decomposition delegates to Filter or Prune on
// child readers.
type decomposeOp int

const (
	filterOp decomposeOp = iota
	pruneOp
)

func (op decomposeOp) call(ctx context.Context, r Reader, alloc *memory.Allocator, e expr.Expression, mask memory.Bitmap) (memory.Bitmap, error) {
	switch op {
	case filterOp:
		return r.Filter(ctx, alloc, e, mask)
	case pruneOp:
		return r.Prune(ctx, alloc, e, mask)
	default:
		panic("unreachable")
	}
}

func (r *structReader) evalFallbackFilter(ctx context.Context, alloc *memory.Allocator, e expr.Expression, mask memory.Bitmap) (memory.Bitmap, error) {
	cols := collectColumns(e)

	columns := make([]columnar.Column, 0, len(cols))
	arrays := make([]columnar.Array, 0, len(cols))

	for name := range cols {
		idx := r.fieldIndex(name)
		if idx < 0 {
			continue
		}
		arr, err := r.fields[idx].Project(ctx, alloc, &expr.Identity{}, memory.Bitmap{})
		if err != nil {
			return memory.Bitmap{}, fmt.Errorf("projecting field %q for fallback: %w", name, err)
		}
		columns = append(columns, columnar.Column{Name: name})
		arrays = append(arrays, arr)
	}

	input := columnar.NewStruct(columnar.NewSchema(columns), arrays, r.length, memory.Bitmap{})
	result, err := expr.Evaluate(alloc, e, input, mask)
	if err != nil {
		return memory.Bitmap{}, fmt.Errorf("evaluating fallback expression: %w", err)
	}

	resultMask, err := datumToMask(alloc, result, r.length)
	if err != nil {
		return memory.Bitmap{}, fmt.Errorf("converting fallback result to mask: %w", err)
	}
	if mask.Len() > 0 {
		resultMask = intersectMasks(alloc, resultMask, mask)
	}
	return resultMask, nil
}

func (r *structReader) fieldIndex(name string) int {
	for i, f := range r.typ.Fields {
		if f.Name == name {
			return i
		}
	}
	return -1
}

func (r *structReader) Project(ctx context.Context, alloc *memory.Allocator, projection expr.Expression, mask memory.Bitmap) (columnar.Array, error) {
	if projection == nil {
		return nil, fmt.Errorf("nil projection expression is invalid")
	}

	fieldNames := make([]string, len(r.typ.Fields))
	for i, field := range r.typ.Fields {
		fieldNames[i] = field.Name
	}

	var (
		simplified       = simplifyProjection(projection, fieldNames)
		preserveValidity = preservesInputStruct(projection)
	)

	var validity memory.Bitmap
	if r.validity != nil {
		vArr, err := r.validity.Project(ctx, alloc, &expr.Identity{}, mask)
		if err != nil {
			return nil, fmt.Errorf("projecting struct validity: %w", err)
		}
		validity = vArr.(*columnar.Bool).Values()
	}

	projectValidity := validity
	if preserveValidity {
		projectValidity = memory.Bitmap{}
	}
	result, err := r.walkProject(ctx, alloc, simplified, mask, projectValidity)
	if err != nil {
		return nil, err
	}
	if preserveValidity && validity.Len() > 0 {
		result, err = compute.PropagateNulls(alloc, result, validity)
		if err != nil {
			return nil, err
		}
	}

	arr, ok := result.(columnar.Array)
	if !ok {
		return nil, fmt.Errorf("projection produced %T, expected Array", result)
	}
	return arr, nil
}

func preservesInputStruct(e expr.Expression) bool {
	switch e := e.(type) {
	case *expr.Identity, *expr.Column:
		return true
	case *expr.Include:
		return preservesInputStruct(e.Value)
	case *expr.Exclude:
		return preservesInputStruct(e.Value)
	default:
		return false
	}
}

func (r *structReader) walkProject(ctx context.Context, alloc *memory.Allocator, e expr.Expression, mask, validity memory.Bitmap) (columnar.Datum, error) {
	if e == nil {
		return nil, fmt.Errorf("nil projection expression is invalid")
	}

	switch e := e.(type) {
	case *expr.Constant:
		empty := columnar.NewStruct(columnar.NewSchema(nil), nil, r.length, memory.Bitmap{})
		return expr.Evaluate(alloc, e, empty, mask)

	case *expr.Identity:
		return r.projectIdentity(ctx, alloc, mask, validity)

	case *expr.Column:
		return r.projectField(ctx, alloc, e.Name, mask, validity)

	case *expr.MakeStruct:
		if len(e.Values) == 0 {
			empty := columnar.NewStruct(columnar.NewSchema(nil), nil, r.length, memory.Bitmap{})
			return expr.Evaluate(alloc, e, empty, mask)
		}
	}

	return expr.Reduce(alloc, e, func(child expr.Expression) (columnar.Datum, error) {
		return r.walkProject(ctx, alloc, child, mask, validity)
	}, mask)
}

func (r *structReader) projectIdentity(ctx context.Context, alloc *memory.Allocator, mask, validity memory.Bitmap) (columnar.Datum, error) {
	var (
		columns = make([]columnar.Column, len(r.typ.Fields))
		fields  = make([]columnar.Array, len(r.typ.Fields))
	)
	for i, fieldType := range r.typ.Fields {
		field, err := r.fields[i].Project(ctx, alloc, &expr.Identity{}, mask)
		if err != nil {
			return nil, fmt.Errorf("projecting field %q: %w", fieldType.Name, err)
		}
		columns[i] = columnar.Column{Name: fieldType.Name}
		fields[i] = field
	}
	return columnar.NewStruct(columnar.NewSchema(columns), fields, r.length, validity), nil
}

func (r *structReader) projectField(ctx context.Context, alloc *memory.Allocator, name string, mask, validity memory.Bitmap) (columnar.Datum, error) {
	idx := r.fieldIndex(name)
	if idx < 0 {
		missing := memory.NewBitmap(alloc, r.length)
		missing.AppendCount(false, r.length)
		return columnar.NewNull(missing), nil
	}

	field, err := r.fields[idx].Project(ctx, alloc, &expr.Identity{}, mask)
	if err != nil {
		return nil, fmt.Errorf("projecting field %q: %w", name, err)
	}
	if validity.Len() == 0 {
		return field, nil
	}
	return compute.PropagateNulls(alloc, field, validity)
}

func (r *structReader) Reset() {
	r.resetSelf()

	for _, fr := range r.fields {
		fr.Reset()
	}
	if r.validity != nil {
		r.validity.Reset()
	}
}

func (r *structReader) resetSelf() {
	r.offset = 0
	r.length = 0
}

func (r *structReader) Close() error {
	r.resetSelf()

	for _, fr := range r.fields {
		fr.Close()
	}
	if r.validity != nil {
		r.validity.Close()
	}
	return nil
}
