package wirecodec

import (
	"context"
	"fmt"

	"github.com/gogo/protobuf/proto"

	"github.com/grafana/loki/v3/pkg/columnar/types"
	"github.com/grafana/loki/v3/pkg/dataset/array"
	"github.com/grafana/loki/v3/pkg/dataset/buffer"
	"github.com/grafana/loki/v3/pkg/dataset/encoding/wirecodec/internal/arraymd"
	"github.com/grafana/loki/v3/pkg/dataset/encoding/wirecodec/internal/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataset/encoding/wirecodec/internal/layoutmd"
	"github.com/grafana/loki/v3/pkg/dataset/encoding/wirecodec/internal/typemd"
	"github.com/grafana/loki/v3/pkg/dataset/layout"
)

type layoutEncoder struct {
	dict *dictionary
	sink buffer.Sink
}

func (e *layoutEncoder) encodeType(t types.Type) *typemd.Type {
	switch t := t.(type) {
	case *types.Null:
		return &typemd.Type{Kind: &typemd.Type_Null{Null: &typemd.Null{}}}
	case *types.Uint32:
		return &typemd.Type{Kind: &typemd.Type_Uint32{Uint32: &typemd.Uint32{Nullable: t.Nullable}}}
	case *types.Uint64:
		return &typemd.Type{Kind: &typemd.Type_Uint64{Uint64: &typemd.Uint64{Nullable: t.Nullable}}}
	case *types.Int32:
		return &typemd.Type{Kind: &typemd.Type_Int32{Int32: &typemd.Int32{Nullable: t.Nullable}}}
	case *types.Int64:
		return &typemd.Type{Kind: &typemd.Type_Int64{Int64: &typemd.Int64{Nullable: t.Nullable}}}
	case *types.Bool:
		return &typemd.Type{Kind: &typemd.Type_Bool{Bool: &typemd.Bool{Nullable: t.Nullable}}}
	case *types.UTF8:
		return &typemd.Type{Kind: &typemd.Type_Utf8{Utf8: &typemd.UTF8{Nullable: t.Nullable}}}
	case *types.Struct:
		fields := make([]*typemd.Struct_Field, len(t.Fields))
		for i, f := range t.Fields {
			fields[i] = &typemd.Struct_Field{
				NameRef: e.dict.add(f.Name),
				Type:    e.encodeType(f.Type),
			}
		}
		return &typemd.Type{Kind: &typemd.Type_Struct{Struct: &typemd.Struct{
			Fields:   fields,
			Nullable: t.Nullable,
		}}}
	case *types.List:
		return &typemd.Type{Kind: &typemd.Type_List{List: &typemd.List{
			Element:  e.encodeType(t.Element),
			Nullable: t.Nullable,
		}}}
	default:
		panic(fmt.Sprintf("wirecodec: unsupported type %T", t))
	}
}

func (e *layoutEncoder) encodeArray(a array.Array) (*arraymd.Array, error) {
	bufIDs := make([]uint64, len(a.Buffers))
	for i, buf := range a.Buffers {
		bufIDs[i] = uint64(buf)
	}

	var children []*arraymd.Array
	for i, child := range a.Children {
		c, err := e.encodeArray(child)
		if err != nil {
			return nil, fmt.Errorf("encoding child array %d: %w", i, err)
		}
		children = append(children, c)
	}

	encMeta, err := e.encodeEncodingMetadata(a.Encoding)
	if err != nil {
		return nil, fmt.Errorf("encoding metadata for %s: %w", a.Encoding.Kind(), err)
	}

	return &arraymd.Array{
		EncodingRef:      e.dict.add("dataset." + a.Encoding.Kind().String()),
		EncodingMetadata: encMeta,
		BufferIds:        bufIDs,
		Children:         children,
		Stats:            &arraymd.Stats{NullCount: int64(a.Stats.NullCount)},
	}, nil
}

func (e *layoutEncoder) encodeEncodingMetadata(enc array.Encoding) ([]byte, error) {
	switch enc := enc.(type) {
	case *array.EncodingBitpacked:
		data, err := proto.Marshal(&arraymd.BitpackedMetadata{
			BlockSize: uint64(enc.BlockSize),
		})
		return data, err
	case *array.EncodingZstd:
		data, err := proto.Marshal(&arraymd.ZstdMetadata{
			UncompressedSize: uint64(enc.UncompressedSize),
		})
		return data, err
	default:
		return nil, nil
	}
}

func marshalMetadata(ctx context.Context, root layout.Layout, sink buffer.Sink) (*datasetmd.Metadata, error) {
	enc := &layoutEncoder{
		dict: &dictionary{},
		sink: sink,
	}

	// Encode the root type (called once for the whole dataset).
	typeProto := enc.encodeType(root.DataType())
	typeData, err := proto.Marshal(typeProto)
	if err != nil {
		return nil, fmt.Errorf("marshaling type: %w", err)
	}
	typeBufs, err := sink.WriteBuffers(ctx, []buffer.Data{typeData})
	if err != nil {
		return nil, fmt.Errorf("writing type buffer: %w", err)
	}

	// Encode the layout tree.
	layoutProto, err := enc.encodeLayout(ctx, root)
	if err != nil {
		return nil, fmt.Errorf("encoding layout: %w", err)
	}
	layoutData, err := proto.Marshal(layoutProto)
	if err != nil {
		return nil, fmt.Errorf("marshaling layout: %w", err)
	}
	layoutBufs, err := sink.WriteBuffers(ctx, []buffer.Data{layoutData})
	if err != nil {
		return nil, fmt.Errorf("writing layout buffer: %w", err)
	}

	// Encode the dictionary (must happen last since earlier steps add entries).
	dictData, err := proto.Marshal(&datasetmd.Dictionary{Keys: enc.dict.keys})
	if err != nil {
		return nil, fmt.Errorf("marshaling dictionary: %w", err)
	}
	dictBufs, err := sink.WriteBuffers(ctx, []buffer.Data{dictData})
	if err != nil {
		return nil, fmt.Errorf("writing dictionary buffer: %w", err)
	}

	return &datasetmd.Metadata{
		TypeBufferId:       uint64(typeBufs[0]),
		LayoutBufferId:     uint64(layoutBufs[0]),
		DictionaryBufferId: uint64(dictBufs[0]),
	}, nil
}

func (e *layoutEncoder) encodeLayout(ctx context.Context, l layout.Layout) (*layoutmd.Layout, error) {
	switch l := l.(type) {
	case *layout.Array:
		arrayProto, err := e.encodeArray(l.Metadata)
		if err != nil {
			return nil, fmt.Errorf("encoding array: %w", err)
		}
		arrayData, err := proto.Marshal(arrayProto)
		if err != nil {
			return nil, fmt.Errorf("marshaling array: %w", err)
		}
		bufs, err := e.sink.WriteBuffers(ctx, []buffer.Data{arrayData})
		if err != nil {
			return nil, fmt.Errorf("writing array buffer: %w", err)
		}
		return &layoutmd.Layout{
			EncodingRef: e.dict.add("dataset.array"),
			RowCount:    uint64(l.Len()),
			BufferIds:   []uint64{uint64(bufs[0])},
		}, nil

	case *layout.Chunked:
		children := make([]*layoutmd.Layout, len(l.Chunks))
		for i, chunk := range l.Chunks {
			child, err := e.encodeLayout(ctx, chunk)
			if err != nil {
				return nil, fmt.Errorf("encoding chunk %d: %w", i, err)
			}
			children[i] = child
		}
		return &layoutmd.Layout{
			EncodingRef: e.dict.add("dataset.chunked"),
			RowCount:    uint64(l.Len()),
			Children:    children,
		}, nil

	case *layout.Struct:
		children := make([]*layoutmd.Layout, 0, len(l.Fields)+1)
		for i, field := range l.Fields {
			child, err := e.encodeLayout(ctx, field)
			if err != nil {
				return nil, fmt.Errorf("encoding field %d: %w", i, err)
			}
			children = append(children, child)
		}
		if l.Validity != nil {
			validity, err := e.encodeLayout(ctx, l.Validity)
			if err != nil {
				return nil, fmt.Errorf("encoding validity: %w", err)
			}
			children = append(children, validity)
		}
		return &layoutmd.Layout{
			EncodingRef: e.dict.add("dataset.struct"),
			RowCount:    uint64(l.Len()),
			Children:    children,
		}, nil

	default:
		return nil, fmt.Errorf("wirecodec: unsupported layout %T", l)
	}
}
