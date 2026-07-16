// Package wirecodec defines the wire format for datasets. This package is used
// for low-level operations to have full control over how data is stored and
// accessed.
//
// For higher-level operations, use [dataset.Build] and [dataset.Open].
package wirecodec

import (
	"context"
	"fmt"

	"github.com/gogo/protobuf/proto"

	"github.com/grafana/loki/v3/pkg/dataset/array"
	"github.com/grafana/loki/v3/pkg/dataset/buffer"
	"github.com/grafana/loki/v3/pkg/dataset/encoding/wirecodec/internal/arraymd"
	"github.com/grafana/loki/v3/pkg/dataset/encoding/wirecodec/internal/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataset/encoding/wirecodec/internal/layoutmd"
	"github.com/grafana/loki/v3/pkg/dataset/layout"
	"github.com/grafana/loki/v3/pkg/memory"
)

// Manifest holds buffer IDs for root information about a dataset. Callers can
// inspect individual buffer IDs for ordering or prefetching decisions.
type Manifest struct {
	TypeBuffer       buffer.ID
	LayoutBuffer     buffer.ID
	FileStatsBuffer  buffer.ID // Not yet populated; will be zero (absent).
	DictionaryBuffer buffer.ID
}

// Build traverses the root layout and serializes information about it to the
// provided sink. It returns a Manifest with the buffer IDs that were flushed.
func Build(ctx context.Context, root layout.Layout, sink buffer.Sink) (Manifest, error) {
	md, err := marshalMetadata(ctx, root, sink)
	if err != nil {
		return Manifest{}, fmt.Errorf("building dataset: %w", err)
	}
	return Manifest{
		TypeBuffer:       buffer.ID(md.TypeBufferId),
		LayoutBuffer:     buffer.ID(md.LayoutBufferId),
		FileStatsBuffer:  buffer.ID(md.FileStatsId),
		DictionaryBuffer: buffer.ID(md.DictionaryBufferId),
	}, nil
}

// Load reconstructs the layout from the buffers referenced by the Manifest.
func (m *Manifest) Load(ctx context.Context, source buffer.Source) (layout.Layout, error) {
	md := &datasetmd.Metadata{
		TypeBufferId:       uint64(m.TypeBuffer),
		LayoutBufferId:     uint64(m.LayoutBuffer),
		FileStatsId:        uint64(m.FileStatsBuffer),
		DictionaryBufferId: uint64(m.DictionaryBuffer),
	}
	return unmarshalMetadata(ctx, md, source)
}

// MarshalBinary serializes the buffer IDs in the Manifest to a byte slice.
func (m *Manifest) MarshalBinary() ([]byte, error) {
	return proto.Marshal(&datasetmd.Metadata{
		TypeBufferId:       uint64(m.TypeBuffer),
		LayoutBufferId:     uint64(m.LayoutBuffer),
		FileStatsId:        uint64(m.FileStatsBuffer),
		DictionaryBufferId: uint64(m.DictionaryBuffer),
	})
}

// UnmarshalBinary reads the manifest from the provided data and populates the
// buffer IDs in the Manifest.
func (m *Manifest) UnmarshalBinary(data []byte) error {
	var md datasetmd.Metadata
	if err := proto.Unmarshal(data, &md); err != nil {
		return err
	}
	m.TypeBuffer = buffer.ID(md.TypeBufferId)
	m.LayoutBuffer = buffer.ID(md.LayoutBufferId)
	m.FileStatsBuffer = buffer.ID(md.FileStatsId)
	m.DictionaryBuffer = buffer.ID(md.DictionaryBufferId)
	return nil
}

// Layout describes the wire-format metadata for a layout node. It provides
// structured access to the serialized layout tree without converting to
// runtime layout types.
type Layout struct {
	// Encoding is the resolved encoding name (e.g., "dataset.array",
	// "dataset.struct", "dataset.chunked").
	Encoding string

	// Metadata holds encoding-specific metadata bytes. Currently unused;
	// reserved for future layout-level metadata.
	Metadata []byte

	// Rows is the row count for this layout node.
	Rows uint64

	// Buffers holds buffer references for this node. For array-encoded
	// layouts, Buffers[0] is the buffer ID of the serialized array proto,
	// loadable via [Manifest.ArrayMetadata].
	Buffers []buffer.ID

	// Children holds child layout nodes.
	Children []Layout
}

// Array describes the wire-format metadata for an encoded array. It
// provides structured access to the serialized array metadata without
// converting to runtime array types.
type Array struct {
	// Encoding is the resolved encoding name (e.g., "dataset.plain",
	// "dataset.bitpacked", "dataset.binary").
	Encoding string

	// Metadata holds encoding-specific metadata (e.g., bitpacked block size,
	// zstd uncompressed size). Nil for encodings with no extra metadata.
	Metadata []byte

	// Buffers holds the data buffer references for this array.
	Buffers []buffer.ID

	// Children holds child arrays (e.g., validity bitmaps, offset arrays).
	Children []Array

	// Stats holds optional statistics. Nil when no stats are present.
	Stats *array.Stats
}

// LayoutMetadata reads and deserializes a layout metadata tree from the given
// buffer. Callers typically pass m.LayoutBuffer; the parameter is explicit for
// call-pattern consistency with [Manifest.ArrayMetadata].
func (m *Manifest) LayoutMetadata(ctx context.Context, source buffer.Source, buf buffer.ID) (Layout, error) {
	dict, err := m.loadDictionary(ctx, source)
	if err != nil {
		return Layout{}, fmt.Errorf("wirecodec: loading dictionary: %w", err)
	}

	var alloc memory.Allocator
	bufs, err := source.ReadBuffers(ctx, &alloc, []buffer.ID{buf})
	if err != nil {
		return Layout{}, fmt.Errorf("wirecodec: reading layout buffer: %w", err)
	}

	var pb layoutmd.Layout
	if err := proto.Unmarshal(bufs[0], &pb); err != nil {
		return Layout{}, fmt.Errorf("wirecodec: unmarshaling layout: %w", err)
	}

	return convertLayout(&pb, dict)
}

// ArrayMetadata reads and deserializes array metadata from the given buffer.
// Callers pass a [Layout.Buffers] entry from a layout node whose Encoding
// is "dataset.array".
func (m *Manifest) ArrayMetadata(ctx context.Context, source buffer.Source, buf buffer.ID) (Array, error) {
	dict, err := m.loadDictionary(ctx, source)
	if err != nil {
		return Array{}, fmt.Errorf("wirecodec: loading dictionary: %w", err)
	}

	var alloc memory.Allocator
	bufs, err := source.ReadBuffers(ctx, &alloc, []buffer.ID{buf})
	if err != nil {
		return Array{}, fmt.Errorf("wirecodec: reading array buffer: %w", err)
	}

	var pb arraymd.Array
	if err := proto.Unmarshal(bufs[0], &pb); err != nil {
		return Array{}, fmt.Errorf("wirecodec: unmarshaling array: %w", err)
	}

	return convertArray(&pb, dict)
}

func (m *Manifest) loadDictionary(ctx context.Context, source buffer.Source) (*dictionary, error) {
	var alloc memory.Allocator
	bufs, err := source.ReadBuffers(ctx, &alloc, []buffer.ID{m.DictionaryBuffer})
	if err != nil {
		return nil, fmt.Errorf("reading dictionary buffer: %w", err)
	}
	var dictProto datasetmd.Dictionary
	if err := proto.Unmarshal(bufs[0], &dictProto); err != nil {
		return nil, fmt.Errorf("unmarshaling dictionary: %w", err)
	}
	return newDictionary(dictProto.Keys), nil
}

func convertLayout(pb *layoutmd.Layout, dict *dictionary) (Layout, error) {
	encoding, err := dict.lookup(pb.EncodingRef)
	if err != nil {
		return Layout{}, fmt.Errorf("decoding layout encoding ref: %w", err)
	}

	children := make([]Layout, len(pb.Children))
	for i, child := range pb.Children {
		c, err := convertLayout(child, dict)
		if err != nil {
			return Layout{}, fmt.Errorf("converting child layout %d: %w", i, err)
		}
		children[i] = c
	}

	buffers := make([]buffer.ID, len(pb.BufferIds))
	for i, id := range pb.BufferIds {
		buffers[i] = buffer.ID(id)
	}

	return Layout{
		Encoding: encoding,
		Metadata: pb.EncodingMetadata,
		Rows:     pb.RowCount,
		Buffers:  buffers,
		Children: children,
	}, nil
}

func convertArray(pb *arraymd.Array, dict *dictionary) (Array, error) {
	encoding, err := dict.lookup(pb.EncodingRef)
	if err != nil {
		return Array{}, fmt.Errorf("decoding array encoding ref: %w", err)
	}

	buffers := make([]buffer.ID, len(pb.BufferIds))
	for i, id := range pb.BufferIds {
		buffers[i] = buffer.ID(id)
	}

	children := make([]Array, len(pb.Children))
	for i, child := range pb.Children {
		c, err := convertArray(child, dict)
		if err != nil {
			return Array{}, fmt.Errorf("converting child array %d: %w", i, err)
		}
		children[i] = c
	}

	var stats *array.Stats
	if pb.Stats != nil {
		stats = &array.Stats{NullCount: int(pb.Stats.NullCount)}
	}

	return Array{
		Encoding: encoding,
		Metadata: pb.EncodingMetadata,
		Buffers:  buffers,
		Children: children,
		Stats:    stats,
	}, nil
}
