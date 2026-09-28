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
	"github.com/grafana/loki/v3/pkg/memory"
)

type layoutDecoder struct {
	dict   *dictionary
	source buffer.Source
	alloc  *memory.Allocator
}

func (d *layoutDecoder) decodeType(pb *typemd.Type) (types.Type, error) {
	switch k := pb.GetKind().(type) {
	case *typemd.Type_Null:
		return &types.Null{}, nil
	case *typemd.Type_Uint32:
		return &types.Uint32{Nullable: k.Uint32.Nullable}, nil
	case *typemd.Type_Uint64:
		return &types.Uint64{Nullable: k.Uint64.Nullable}, nil
	case *typemd.Type_Int32:
		return &types.Int32{Nullable: k.Int32.Nullable}, nil
	case *typemd.Type_Int64:
		return &types.Int64{Nullable: k.Int64.Nullable}, nil
	case *typemd.Type_Bool:
		return &types.Bool{Nullable: k.Bool.Nullable}, nil
	case *typemd.Type_Utf8:
		return &types.UTF8{Nullable: k.Utf8.Nullable}, nil
	case *typemd.Type_Struct:
		fields := make([]types.StructField, len(k.Struct.Fields))
		for i, f := range k.Struct.Fields {
			ft, err := d.decodeType(f.Type)
			if err != nil {
				return nil, fmt.Errorf("decoding struct field %d: %w", i, err)
			}
			name, err := d.dict.lookup(f.NameRef)
			if err != nil {
				return nil, fmt.Errorf("decoding struct field %d name: %w", i, err)
			}
			fields[i] = types.StructField{
				Name: name,
				Type: ft,
			}
		}
		return &types.Struct{Fields: fields, Nullable: k.Struct.Nullable}, nil
	case *typemd.Type_List:
		elem, err := d.decodeType(k.List.Element)
		if err != nil {
			return nil, fmt.Errorf("decoding list element: %w", err)
		}
		return &types.List{Element: elem, Nullable: k.List.Nullable}, nil
	default:
		return nil, fmt.Errorf("wirecodec: unsupported type kind %T", k)
	}
}

func unmarshalMetadata(ctx context.Context, md *datasetmd.Metadata, source buffer.Source) (layout.Layout, error) {
	var alloc memory.Allocator

	// Read the dictionary.
	dictBufs, err := source.ReadBuffers(ctx, &alloc, []buffer.ID{
		buffer.ID(md.DictionaryBufferId),
	})
	if err != nil {
		return nil, fmt.Errorf("reading dictionary buffer: %w", err)
	}
	var dictProto datasetmd.Dictionary
	if err := proto.Unmarshal(dictBufs[0], &dictProto); err != nil {
		return nil, fmt.Errorf("unmarshaling dictionary: %w", err)
	}
	dict := newDictionary(dictProto.Keys)

	dec := &layoutDecoder{
		dict:   dict,
		source: source,
		alloc:  &alloc,
	}

	// Read and decode the type.
	typeBufs, err := source.ReadBuffers(ctx, &alloc, []buffer.ID{
		buffer.ID(md.TypeBufferId),
	})
	if err != nil {
		return nil, fmt.Errorf("reading type buffer: %w", err)
	}
	var typeProto typemd.Type
	if err := proto.Unmarshal(typeBufs[0], &typeProto); err != nil {
		return nil, fmt.Errorf("unmarshaling type: %w", err)
	}
	typ, err := dec.decodeType(&typeProto)
	if err != nil {
		return nil, fmt.Errorf("decoding type: %w", err)
	}

	// Read and decode the layout.
	layoutBufs, err := source.ReadBuffers(ctx, &alloc, []buffer.ID{
		buffer.ID(md.LayoutBufferId),
	})
	if err != nil {
		return nil, fmt.Errorf("reading layout buffer: %w", err)
	}
	var layoutProto layoutmd.Layout
	if err := proto.Unmarshal(layoutBufs[0], &layoutProto); err != nil {
		return nil, fmt.Errorf("unmarshaling layout: %w", err)
	}

	return dec.decodeLayout(ctx, &layoutProto, typ)
}

func (d *layoutDecoder) decodeLayout(ctx context.Context, pb *layoutmd.Layout, typ types.Type) (layout.Layout, error) {
	ref, err := d.dict.lookup(pb.EncodingRef)
	if err != nil {
		return nil, fmt.Errorf("decoding layout encoding ref: %w", err)
	}

	switch ref {
	case "dataset.array":
		if len(pb.BufferIds) < 1 {
			return nil, fmt.Errorf("wirecodec: array layout has no buffer_ids")
		}
		arrayBufs, err := d.source.ReadBuffers(ctx, d.alloc, []buffer.ID{
			buffer.ID(pb.BufferIds[0]),
		})
		if err != nil {
			return nil, fmt.Errorf("reading array buffer: %w", err)
		}
		var arrayProto arraymd.Array
		if err := proto.Unmarshal(arrayBufs[0], &arrayProto); err != nil {
			return nil, fmt.Errorf("unmarshaling array: %w", err)
		}
		arr, err := d.decodeArray(&arrayProto, int(pb.RowCount), typ)
		if err != nil {
			return nil, err
		}
		return &layout.Array{Metadata: arr}, nil

	case "dataset.chunked":
		chunks := make([]layout.Layout, len(pb.Children))
		for i, child := range pb.Children {
			c, err := d.decodeLayout(ctx, child, typ)
			if err != nil {
				return nil, fmt.Errorf("decoding chunk %d: %w", i, err)
			}
			chunks[i] = c
		}
		return &layout.Chunked{Type: typ, Chunks: chunks}, nil

	case "dataset.struct":
		st, ok := typ.(*types.Struct)
		if !ok {
			return nil, fmt.Errorf("wirecodec: struct layout but type is %T", typ)
		}

		fieldCount := len(st.Fields)
		expectChildren := fieldCount
		if st.Nullable {
			expectChildren++
		}
		if len(pb.Children) != expectChildren {
			return nil, fmt.Errorf("wirecodec: struct has %d children, expected %d", len(pb.Children), expectChildren)
		}

		fields := make([]layout.Layout, fieldCount)
		for i := range fieldCount {
			f, err := d.decodeLayout(ctx, pb.Children[i], st.Fields[i].Type)
			if err != nil {
				return nil, fmt.Errorf("decoding field %d: %w", i, err)
			}
			fields[i] = f
		}

		var validity layout.Layout
		if st.Nullable {
			v, err := d.decodeLayout(ctx, pb.Children[fieldCount], &types.Bool{})
			if err != nil {
				return nil, fmt.Errorf("decoding validity: %w", err)
			}
			validity = v
		}

		return &layout.Struct{
			Type:     typ,
			Fields:   fields,
			Validity: validity,
		}, nil

	default:
		return nil, fmt.Errorf("wirecodec: unknown layout encoding ref %q", ref)
	}
}

func (d *layoutDecoder) decodeArray(pb *arraymd.Array, rowCount int, typ types.Type) (array.Array, error) {
	enc, err := d.decodeEncoding(pb)
	if err != nil {
		return array.Array{}, err
	}

	bufs := make([]buffer.ID, len(pb.BufferIds))
	for i, id := range pb.BufferIds {
		bufs[i] = buffer.ID(id)
	}

	var children []array.Array
	if len(pb.Children) > 0 {
		childTypes, childRowCounts := childArrayInfo(enc, typ, rowCount)
		children = make([]array.Array, len(pb.Children))
		for i, child := range pb.Children {
			ct, cr := typ, 0
			if i < len(childTypes) {
				ct, cr = childTypes[i], childRowCounts[i]
			}
			c, err := d.decodeArray(child, cr, ct)
			if err != nil {
				return array.Array{}, fmt.Errorf("decoding child array %d: %w", i, err)
			}
			children[i] = c
		}
	}

	var stats array.Stats
	if pb.Stats != nil {
		stats.NullCount = int(pb.Stats.NullCount)
	}

	return array.Array{
		Encoding: enc,
		Type:     typ,
		Buffers:  bufs,
		RowCount: rowCount,
		Stats:    stats,
		Children: children,
	}, nil
}

// childArrayInfo returns the expected types and row counts for the children of
// an array with the given encoding, parent type, and parent row count. Each
// encoding defines its own child structure:
//
//   - bool/plain: optional validity child (Bool, same row count)
//   - binary/zstd: offsets child (Int32, rowCount+1), optional validity child
//   - bitpacked: widths child (Uint32, ceil(rowCount/blockSize)), optional validity child
//   - zigzag: data child (unsigned variant of parent type, same row count)
//   - delta: data child (same type, same row count)
func childArrayInfo(enc array.Encoding, parentType types.Type, parentRowCount int) (childTypes []types.Type, childRowCounts []int) {
	// Validity, offsets, and widths are structural children: they always have a
	// value for each expected position and are therefore non-nullable. Data
	// children preserve the parent type's nullability.
	boolType := &types.Bool{Nullable: false}

	switch enc := enc.(type) {
	case *array.EncodingBool, *array.EncodingPlain:
		// Optional validity child.
		return []types.Type{boolType}, []int{parentRowCount}

	case *array.EncodingBinary, *array.EncodingZstd:
		// Offsets (Int32, N+1 rows) + optional validity.
		return []types.Type{
				&types.Int32{Nullable: false},
				boolType,
			}, []int{
				parentRowCount + 1,
				parentRowCount,
			}

	case *array.EncodingBitpacked:
		// Widths (Uint32, one per block) + optional validity.
		numBlocks := 0
		if enc.BlockSize > 0 && parentRowCount > 0 {
			numBlocks = (parentRowCount + enc.BlockSize - 1) / enc.BlockSize
		}
		return []types.Type{
				&types.Uint32{Nullable: false},
				boolType,
			}, []int{
				numBlocks,
				parentRowCount,
			}

	case *array.EncodingZigZag:
		// Data child is the unsigned variant of the parent type.
		return []types.Type{unsignedVariant(parentType)}, []int{parentRowCount}

	case *array.EncodingDelta:
		// Data child has the same type.
		return []types.Type{parentType}, []int{parentRowCount}

	default:
		return nil, nil
	}
}

// unsignedVariant returns the unsigned counterpart of a signed integer type.
func unsignedVariant(typ types.Type) types.Type {
	switch typ := typ.(type) {
	case *types.Int32:
		return &types.Uint32{Nullable: typ.Nullable}
	case *types.Int64:
		return &types.Uint64{Nullable: typ.Nullable}
	default:
		return typ
	}
}

func (d *layoutDecoder) decodeEncoding(pb *arraymd.Array) (array.Encoding, error) {
	ref, err := d.dict.lookup(pb.EncodingRef)
	if err != nil {
		return nil, fmt.Errorf("decoding array encoding ref: %w", err)
	}

	switch ref {
	case "dataset.bool":
		return &array.EncodingBool{}, nil
	case "dataset.plain":
		return &array.EncodingPlain{}, nil
	case "dataset.binary":
		return &array.EncodingBinary{}, nil
	case "dataset.bitpacked":
		var md arraymd.BitpackedMetadata
		if err := proto.Unmarshal(pb.EncodingMetadata, &md); err != nil {
			return nil, fmt.Errorf("unmarshaling bitpacked metadata: %w", err)
		}
		return &array.EncodingBitpacked{BlockSize: int(md.BlockSize)}, nil
	case "dataset.zigzag":
		return &array.EncodingZigZag{}, nil
	case "dataset.delta":
		return &array.EncodingDelta{}, nil
	case "dataset.zstd":
		var md arraymd.ZstdMetadata
		if err := proto.Unmarshal(pb.EncodingMetadata, &md); err != nil {
			return nil, fmt.Errorf("unmarshaling zstd metadata: %w", err)
		}
		return &array.EncodingZstd{UncompressedSize: int(md.UncompressedSize)}, nil
	default:
		return nil, fmt.Errorf("wirecodec: unknown encoding ref %q", ref)
	}
}
