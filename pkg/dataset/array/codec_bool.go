package array

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/dataset/types"
	"github.com/grafana/loki/v3/pkg/memory"
)

type boolWriter struct {
	alloc *memory.Allocator
	typ   *types.Bool

	bmap     memory.Bitmap
	validity Writer
	nulls    int
}

func newBoolWriter(alloc *memory.Allocator, spec Spec, typ types.Type) (*boolWriter, error) {
	if got, want := spec.Kind(), EncodingKindBool; got != want {
		return nil, fmt.Errorf("expected spec kind %s, got %s", want, got)
	} else if got, want := typ.Kind(), types.KindBool; got != want {
		return nil, fmt.Errorf("expected type %s, got %s", want, got)
	}

	var (
		boolTyp  = typ.(*types.Bool)
		boolSpec = spec.(*SpecBool)
	)

	hasValidity := boolSpec.Validity != nil
	if boolTyp.Nullable != hasValidity {
		return nil, fmt.Errorf("expected %s to have validity %t, got %t", boolTyp, boolTyp.Nullable, hasValidity)
	}

	var validityWriter Writer
	if hasValidity {
		var err error
		validityWriter, err = NewWriter(alloc, boolSpec.Validity, &types.Bool{Nullable: false})
		if err != nil {
			return nil, err
		}
	}

	return &boolWriter{
		alloc: alloc,
		typ:   boolTyp,

		bmap:     memory.NewBitmap(alloc, 0),
		validity: validityWriter,
	}, nil
}

func (w *boolWriter) Append(arr columnar.Array) error {
	boolArr, ok := arr.(*columnar.Bool)
	if !ok {
		return fmt.Errorf("expected *columnar.Bool, got %T", arr)
	}

	values := boolArr.Values()
	w.bmap.AppendBitmap(values)

	// Fall through to the utility method to handle nulls (including whether our
	// type is not nullable).
	nulls, err := appendNulls(w.alloc, w.validity, boolArr, values.Len())
	if err != nil {
		return err
	}
	w.nulls += nulls
	return nil
}

func (w *boolWriter) Flush(ctx context.Context, sink Sink) (Array, error) {
	defer w.reset()

	var children []Array
	var validityArray Array
	if validity := w.validity; validity != nil {
		var err error
		validityArray, err = validity.Flush(ctx, sink)
		if err != nil {
			return Array{}, fmt.Errorf("flushing validity writer: %w", err)
		}

		children = append(children, validityArray)
	}

	data, _ := w.bmap.Bytes()
	bufs, err := sink.WriteBuffers(ctx, []BufferData{data})
	if err != nil {
		return Array{}, fmt.Errorf("writing bool data to a buffer: %w", err)
	}

	return Array{
		Encoding: &EncodingBool{},
		Type:     w.typ,
		Buffers:  bufs,
		Stats: Stats{
			RowCount:  w.bmap.Len(),
			NullCount: w.nulls,
		},
		Children: children,
	}, nil
}

func (w *boolWriter) reset() {
	w.bmap = memory.NewBitmap(w.alloc, 0)
	w.nulls = 0
}

type boolReader struct {
	alloc  *memory.Allocator
	typ    *types.Bool
	arr    Array
	source Source

	validity Reader

	initialized bool
	data        memory.Bitmap
	off         int // Offset into data
}

func newBoolReader(alloc *memory.Allocator, arr Array, source Source) (*boolReader, error) {
	if got, want := arr.Encoding.Kind(), EncodingKindBool; got != want {
		return nil, fmt.Errorf("expected spec kind %s, got %s", want, got)
	} else if got, want := arr.Type.Kind(), types.KindBool; got != want {
		return nil, fmt.Errorf("expected type %s, got %s", want, got)
	}

	var (
		boolTyp = arr.Type.(*types.Bool)
	)

	var validityReader Reader
	switch {
	case boolTyp.Nullable && len(arr.Children) == 0:
		return nil, errors.New("nullable bool array must have a validity child array")
	case boolTyp.Nullable && len(arr.Children) > 1:
		return nil, fmt.Errorf("expected 1 child for nullable bool array, got %d", len(arr.Children))
	case !boolTyp.Nullable && len(arr.Children) != 0:
		return nil, fmt.Errorf("expected 0 children for non-nullable bool array, got %d", len(arr.Children))
	}
	if boolTyp.Nullable {
		var err error
		validityReader, err = NewReader(alloc, arr.Children[0], source)
		if err != nil {
			return nil, fmt.Errorf("creating validity reader: %w", err)
		}
	}

	return &boolReader{
		alloc:  alloc,
		typ:    boolTyp,
		arr:    arr,
		source: source,

		validity: validityReader,
	}, nil
}

func (r *boolReader) Read(ctx context.Context, alloc *memory.Allocator, count int) (columnar.Array, error) {
	if !r.initialized {
		// We use the reader's allocator for initializing since the data
		// persists across calls to Read.
		if err := r.init(ctx, r.alloc); err != nil {
			return nil, err
		}
		r.initialized = true
	}

	endOff := min(r.off+count, r.data.Len())
	if endOff-r.off == 0 {
		return nil, io.EOF
	}
	values := r.data.Slice(r.off, endOff)

	// Read validity bytes now.
	var validity memory.Bitmap
	if r.validity != nil {
		validityArr, err := r.validity.Read(ctx, alloc, count)
		if err != nil {
			return nil, fmt.Errorf("reading validity: %w", err)
		}

		validityBoolArr := validityArr.(*columnar.Bool)
		validity = validityBoolArr.Values()
	}

	r.off += values.Len()
	return columnar.NewBool(*values, validity), nil
}

func (r *boolReader) init(ctx context.Context, alloc *memory.Allocator) error {
	data, err := r.source.ReadBuffers(ctx, alloc, r.arr.Buffers)
	if err != nil {
		return fmt.Errorf("fetching buffer data: %w", err)
	} else if len(data) != 1 {
		return fmt.Errorf("expected 1 buffer, got %d", len(data))
	}

	r.data = memory.BitmapFrom(data[0], r.arr.Stats.RowCount, 0)
	return nil
}

func (r *boolReader) Close() error {
	if r.validity != nil {
		return r.validity.Close()
	}
	return nil
}
