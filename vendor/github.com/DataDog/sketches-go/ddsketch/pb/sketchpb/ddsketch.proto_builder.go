// THIS IS A GENERATED FILE
// DO NOT EDIT
package sketchpb

import (
	bytes "bytes"
	protowire "google.golang.org/protobuf/encoding/protowire"
	io "io"
	math "math"
)

type DDSketchBuilder struct {
	writer              io.Writer
	buf                 bytes.Buffer
	scratch             []byte
	indexMappingBuilder IndexMappingBuilder
	storeBuilder        StoreBuilder
}

func NewDDSketchBuilder(writer io.Writer) *DDSketchBuilder {
	return &DDSketchBuilder{
		writer: writer,
	}
}
func (x *DDSketchBuilder) Reset(writer io.Writer) {
	x.buf.Reset()
	x.writer = writer
}
func (x *DDSketchBuilder) SetMapping(cb func(w *IndexMappingBuilder)) {
	x.buf.Reset()
	x.indexMappingBuilder.writer = &x.buf
	x.indexMappingBuilder.scratch = x.scratch
	cb(&x.indexMappingBuilder)
	x.scratch = protowire.AppendVarint(x.scratch[:0], 0xa)
	x.scratch = protowire.AppendVarint(x.scratch, uint64(x.buf.Len()))
	x.writer.Write(x.scratch)
	x.writer.Write(x.buf.Bytes())
}
func (x *DDSketchBuilder) SetPositiveValues(cb func(w *StoreBuilder)) {
	x.buf.Reset()
	x.storeBuilder.writer = &x.buf
	x.storeBuilder.scratch = x.scratch
	cb(&x.storeBuilder)
	x.scratch = protowire.AppendVarint(x.scratch[:0], 0x12)
	x.scratch = protowire.AppendVarint(x.scratch, uint64(x.buf.Len()))
	x.writer.Write(x.scratch)
	x.writer.Write(x.buf.Bytes())
}
func (x *DDSketchBuilder) SetNegativeValues(cb func(w *StoreBuilder)) {
	x.buf.Reset()
	x.storeBuilder.writer = &x.buf
	x.storeBuilder.scratch = x.scratch
	cb(&x.storeBuilder)
	x.scratch = protowire.AppendVarint(x.scratch[:0], 0x1a)
	x.scratch = protowire.AppendVarint(x.scratch, uint64(x.buf.Len()))
	x.writer.Write(x.scratch)
	x.writer.Write(x.buf.Bytes())
}
func (x *DDSketchBuilder) SetZeroCount(v float64) {
	x.scratch = protowire.AppendVarint(x.scratch[:0], 0x21)
	x.scratch = protowire.AppendFixed64(x.scratch, math.Float64bits(v))
	x.writer.Write(x.scratch)
}

type IndexMappingBuilder struct {
	writer  io.Writer
	buf     bytes.Buffer
	scratch []byte
}

func NewIndexMappingBuilder(writer io.Writer) *IndexMappingBuilder {
	return &IndexMappingBuilder{
		writer: writer,
	}
}
func (x *IndexMappingBuilder) Reset(writer io.Writer) {
	x.buf.Reset()
	x.writer = writer
}
func (x *IndexMappingBuilder) SetGamma(v float64) {
	x.scratch = protowire.AppendVarint(x.scratch[:0], 0x9)
	x.scratch = protowire.AppendFixed64(x.scratch, math.Float64bits(v))
	x.writer.Write(x.scratch)
}
func (x *IndexMappingBuilder) SetIndexOffset(v float64) {
	x.scratch = protowire.AppendVarint(x.scratch[:0], 0x11)
	x.scratch = protowire.AppendFixed64(x.scratch, math.Float64bits(v))
	x.writer.Write(x.scratch)
}
func (x *IndexMappingBuilder) SetInterpolation(v uint64) {
	if v != 0 {
		x.scratch = protowire.AppendVarint(x.scratch[:0], 0x18)
		x.scratch = protowire.AppendVarint(x.scratch, v)
		x.writer.Write(x.scratch)
	}
}

type StoreBuilder struct {
	writer                      io.Writer
	buf                         bytes.Buffer
	scratch                     []byte
	store_BinCountsEntryBuilder Store_BinCountsEntryBuilder
}

func NewStoreBuilder(writer io.Writer) *StoreBuilder {
	return &StoreBuilder{
		writer: writer,
	}
}
func (x *StoreBuilder) Reset(writer io.Writer) {
	x.buf.Reset()
	x.writer = writer
}
func (x *StoreBuilder) AddBinCounts(cb func(w *Store_BinCountsEntryBuilder)) {
	x.buf.Reset()
	x.store_BinCountsEntryBuilder.writer = &x.buf
	x.store_BinCountsEntryBuilder.scratch = x.scratch
	cb(&x.store_BinCountsEntryBuilder)
	x.scratch = protowire.AppendVarint(x.scratch[:0], 0xa)
	x.scratch = protowire.AppendVarint(x.scratch, uint64(x.buf.Len()))
	x.writer.Write(x.scratch)
	x.writer.Write(x.buf.Bytes())
}
func (x *StoreBuilder) AddContiguousBinCounts(v float64) {
	x.scratch = protowire.AppendVarint(x.scratch[:0], 0x11)
	x.scratch = protowire.AppendFixed64(x.scratch, math.Float64bits(v))
	x.writer.Write(x.scratch)
}
func (x *StoreBuilder) SetContiguousBinIndexOffset(v int32) {
	x.scratch = x.scratch[:0]
	x.scratch = protowire.AppendVarint(x.scratch, 0x18)
	x.scratch = protowire.AppendVarint(x.scratch, protowire.EncodeZigZag(int64(v)))
	x.writer.Write(x.scratch)
}

type Store_BinCountsEntryBuilder struct {
	writer  io.Writer
	buf     bytes.Buffer
	scratch []byte
}

func NewStore_BinCountsEntryBuilder(writer io.Writer) *Store_BinCountsEntryBuilder {
	return &Store_BinCountsEntryBuilder{
		writer: writer,
	}
}
func (x *Store_BinCountsEntryBuilder) Reset(writer io.Writer) {
	x.buf.Reset()
	x.writer = writer
}
func (x *Store_BinCountsEntryBuilder) SetKey(v int32) {
	x.scratch = x.scratch[:0]
	x.scratch = protowire.AppendVarint(x.scratch, 0x8)
	x.scratch = protowire.AppendVarint(x.scratch, protowire.EncodeZigZag(int64(v)))
	x.writer.Write(x.scratch)
}
func (x *Store_BinCountsEntryBuilder) SetValue(v float64) {
	x.scratch = protowire.AppendVarint(x.scratch[:0], 0x11)
	x.scratch = protowire.AppendFixed64(x.scratch, math.Float64bits(v))
	x.writer.Write(x.scratch)
}
