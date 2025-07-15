/*
Copyright 2024 Google LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package bigtable

import (
	btapb "cloud.google.com/go/bigtable/admin/apiv2/adminpb"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// Type wraps the protobuf representation of a type. See the protobuf definition
// for more details on types.
type Type interface {
	proto() *btapb.Type
}

var marshalOptions = protojson.MarshalOptions{AllowPartial: true, UseEnumNumbers: true}
var unmarshalOptions = protojson.UnmarshalOptions{AllowPartial: true}

// MarshalJSON returns the string representation of the Type protobuf.
func MarshalJSON(t Type) ([]byte, error) {
	return marshalOptions.Marshal(t.proto())
}

// UnmarshalJSON returns a Type object from json bytes.
func UnmarshalJSON(data []byte) (Type, error) {
	result := &btapb.Type{}
	if err := unmarshalOptions.Unmarshal(data, result); err != nil {
		return nil, err
	}
	return ProtoToType(result), nil
}

// Equal compares Type objects.
func Equal(a, b Type) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return proto.Equal(a.proto(), b.proto())
}

// TypeUnspecified represents the absence of a type.
type TypeUnspecified struct{}

func (n TypeUnspecified) proto() *btapb.Type {
	return &btapb.Type{}
}

type unknown[T interface{}] struct {
	wrapped *T
}

func (u unknown[T]) proto() *T {
	return u.wrapped
}

// BytesEncoding represents the encoding of a Bytes type.
type BytesEncoding interface {
	proto() *btapb.Type_Bytes_Encoding
}

// RawBytesEncoding represents a Bytes encoding with no additional encodings.
type RawBytesEncoding struct {
}

func (encoding RawBytesEncoding) proto() *btapb.Type_Bytes_Encoding {
	return &btapb.Type_Bytes_Encoding{
		Encoding: &btapb.Type_Bytes_Encoding_Raw_{
			Raw: &btapb.Type_Bytes_Encoding_Raw{}}}
}

// BytesType represents a string of bytes.
type BytesType struct {
	Encoding BytesEncoding
}

func (bytes BytesType) proto() *btapb.Type {
	var encoding *btapb.Type_Bytes_Encoding
	if bytes.Encoding != nil {
		encoding = bytes.Encoding.proto()
	} else {
		encoding = RawBytesEncoding{}.proto()
	}
	return &btapb.Type{Kind: &btapb.Type_BytesType{BytesType: &btapb.Type_Bytes{Encoding: encoding}}}
}

// StringEncoding represents the encoding of a String.
type StringEncoding interface {
	proto() *btapb.Type_String_Encoding
}

// StringUtf8Encoding represents an UTF-8 raw encoding for a string.
// DEPRECATED: Please use StringUtf8BytesEncoding.
type StringUtf8Encoding struct{}

func (encoding StringUtf8Encoding) proto() *btapb.Type_String_Encoding {
	return &btapb.Type_String_Encoding{
		Encoding: &btapb.Type_String_Encoding_Utf8Raw_{},
	}
}

// StringUtf8BytesEncoding represents an UTF-8 bytes encoding for a string.
type StringUtf8BytesEncoding struct{}

func (encoding StringUtf8BytesEncoding) proto() *btapb.Type_String_Encoding {
	return &btapb.Type_String_Encoding{
		Encoding: &btapb.Type_String_Encoding_Utf8Bytes_{},
	}
}

// StringType represents a string
type StringType struct {
	Encoding StringEncoding
}

func (str StringType) proto() *btapb.Type {
	var encoding *btapb.Type_String_Encoding
	if str.Encoding != nil {
		encoding = str.Encoding.proto()
	} else {
		encoding = StringUtf8Encoding{}.proto()
	}
	return &btapb.Type{Kind: &btapb.Type_StringType{StringType: &btapb.Type_String{Encoding: encoding}}}
}

// Int64Encoding represents the encoding of an Int64 type.
type Int64Encoding interface {
	proto() *btapb.Type_Int64_Encoding
}

// BigEndianBytesEncoding represents an Int64 encoding where the value is encoded
// as an 8-byte big-endian value.
type BigEndianBytesEncoding struct {
}

func (beb BigEndianBytesEncoding) proto() *btapb.Type_Int64_Encoding {
	return &btapb.Type_Int64_Encoding{
		Encoding: &btapb.Type_Int64_Encoding_BigEndianBytes_{
			BigEndianBytes: &btapb.Type_Int64_Encoding_BigEndianBytes{},
		},
	}
}

// Int64OrderedCodeBytesEncoding represents an Int64 encoding where the value is
// encoded to a variable length binary format of up to 10 bytes.
type Int64OrderedCodeBytesEncoding struct {
}

func (Int64OrderedCodeBytesEncoding) proto() *btapb.Type_Int64_Encoding {
	return &btapb.Type_Int64_Encoding{
		Encoding: &btapb.Type_Int64_Encoding_OrderedCodeBytes_{
			OrderedCodeBytes: &btapb.Type_Int64_Encoding_OrderedCodeBytes{},
		},
	}
}

// Int64Type represents an 8-byte integer.
type Int64Type struct {
	Encoding Int64Encoding
}

func (it Int64Type) proto() *btapb.Type {
	var encoding *btapb.Type_Int64_Encoding
	if it.Encoding != nil {
		encoding = it.Encoding.proto()
	} else {
		// default encoding to BigEndianBytes
		encoding = BigEndianBytesEncoding{}.proto()
	}

	return &btapb.Type{
		Kind: &btapb.Type_Int64Type{
			Int64Type: &btapb.Type_Int64{
				Encoding: encoding,
			},
		},
	}
}

// TimestampEncoding represents the encoding of the Timestamp type.
type TimestampEncoding interface {
	proto() *btapb.Type_Timestamp_Encoding
}

// TimestampUnixMicrosInt64Encoding represents a timestamp encoding where the
// the number of microseconds in the timestamp value since the Unix epoch
// is encoded by the given Int64Encoding. Values must be microsecond-aligned.
type TimestampUnixMicrosInt64Encoding struct {
	UnixMicrosInt64Encoding Int64Encoding
}

func (tsumi TimestampUnixMicrosInt64Encoding) proto() *btapb.Type_Timestamp_Encoding {
	return &btapb.Type_Timestamp_Encoding{
		Encoding: &btapb.Type_Timestamp_Encoding_UnixMicrosInt64{
			UnixMicrosInt64: tsumi.UnixMicrosInt64Encoding.proto(),
		},
	}
}

// TimestampType represents the timestamp.
type TimestampType struct {
	Encoding TimestampEncoding
}

func (tt TimestampType) proto() *btapb.Type {
	var encoding *btapb.Type_Timestamp_Encoding
	if tt.Encoding != nil {
		encoding = tt.Encoding.proto()
	}

	return &btapb.Type{
		Kind: &btapb.Type_TimestampType{
			TimestampType: &btapb.Type_Timestamp{
				Encoding: encoding,
			},
		},
	}
}

// StructField represents a field within a StructType.
type StructField struct {
	FieldName string
	FieldType Type
}

func (sf StructField) proto() *btapb.Type_Struct_Field {
	return &btapb.Type_Struct_Field{
		FieldName: sf.FieldName,
		Type:      sf.FieldType.proto(),
	}
}

// StructEncoding represents the encoding of a struct type.
type StructEncoding interface {
	proto() *btapb.Type_Struct_Encoding
}

// StructSingletonEncoding represents a singleton struct encoding where this mode
// only accepts a single field.
type StructSingletonEncoding struct{}

func (sse StructSingletonEncoding) proto() *btapb.Type_Struct_Encoding {
	return &btapb.Type_Struct_Encoding{
		Encoding: &btapb.Type_Struct_Encoding_Singleton_{
			Singleton: &btapb.Type_Struct_Encoding_Singleton{},
		},
	}
}

// StructDelimitedBytesEncoding represents a delimited bytes struct encoding,
// where each field will be individually encoded and concatenated by the
// delimiter in the middle.
type StructDelimitedBytesEncoding struct {
	Delimiter []byte
}

func (sdbe StructDelimitedBytesEncoding) proto() *btapb.Type_Struct_Encoding {
	return &btapb.Type_Struct_Encoding{
		Encoding: &btapb.Type_Struct_Encoding_DelimitedBytes_{
			DelimitedBytes: &btapb.Type_Struct_Encoding_DelimitedBytes{
				Delimiter: sdbe.Delimiter,
			},
		},
	}
}

// StructOrderedCodeBytesEncoding an encoding where each field will be
// individually encoded. Each null byte (0x00) in the encoded fields will be
// escaped by 0xff before concatenate together by this {0x00, 0x01} byte sequence.
type StructOrderedCodeBytesEncoding struct{}

func (socbe StructOrderedCodeBytesEncoding) proto() *btapb.Type_Struct_Encoding {
	return &btapb.Type_Struct_Encoding{
		Encoding: &btapb.Type_Struct_Encoding_OrderedCodeBytes_{
			OrderedCodeBytes: &btapb.Type_Struct_Encoding_OrderedCodeBytes{},
		},
	}
}

// StructType represents a struct type.
type StructType struct {
	Fields   []StructField
	Encoding StructEncoding
}

func (st StructType) proto() *btapb.Type {
	var encoding *btapb.Type_Struct_Encoding
	if st.Encoding != nil {
		encoding = st.Encoding.proto()
	}

	var structFieldsProto []*btapb.Type_Struct_Field
	for _, sf := range st.Fields {
		structFieldsProto = append(structFieldsProto, sf.proto())
	}
	return &btapb.Type{
		Kind: &btapb.Type_StructType{
			StructType: &btapb.Type_Struct{
				Fields:   structFieldsProto,
				Encoding: encoding,
			},
		},
	}
}

// Aggregator represents an aggregation function for an aggregate type.
type Aggregator interface {
	fillProto(proto *btapb.Type_Aggregate)
}

// SumAggregator is an aggregation function that sums inputs together into its
// accumulator.
type SumAggregator struct{}

func (sum SumAggregator) fillProto(proto *btapb.Type_Aggregate) {
	proto.Aggregator = &btapb.Type_Aggregate_Sum_{Sum: &btapb.Type_Aggregate_Sum{}}
}

// MinAggregator is an aggregation function that finds the minimum between the input and the accumulator.
type MinAggregator struct{}

func (min MinAggregator) fillProto(proto *btapb.Type_Aggregate) {
	proto.Aggregator = &btapb.Type_Aggregate_Min_{Min: &btapb.Type_Aggregate_Min{}}
}

// MaxAggregator is an aggregation function that finds the maximum between the input and the accumulator.
type MaxAggregator struct{}

func (max MaxAggregator) fillProto(proto *btapb.Type_Aggregate) {
	proto.Aggregator = &btapb.Type_Aggregate_Max_{Max: &btapb.Type_Aggregate_Max{}}
}

// HllppUniqueCountAggregator is an aggregation function that calculates the unique count of inputs and the accumulator.
type HllppUniqueCountAggregator struct{}

func (hll HllppUniqueCountAggregator) fillProto(proto *btapb.Type_Aggregate) {
	proto.Aggregator = &btapb.Type_Aggregate_HllppUniqueCount{HllppUniqueCount: &btapb.Type_Aggregate_HyperLogLogPlusPlusUniqueCount{}}
}

type unknownAggregator struct {
	wrapped *btapb.Type_Aggregate
}

func (ua unknownAggregator) fillProto(proto *btapb.Type_Aggregate) {
	if ua.wrapped == nil {
		return
	}
	proto.Aggregator = ua.wrapped.Aggregator
}

// AggregateType represents an aggregate.  See types.proto for more details
// on aggregate types.
type AggregateType struct {
	Input      Type
	Aggregator Aggregator
}

func (agg AggregateType) proto() *btapb.Type {
	protoAgg := &btapb.Type_Aggregate{
		InputType: agg.Input.proto(),
	}

	agg.Aggregator.fillProto(protoAgg)
	return &btapb.Type{Kind: &btapb.Type_AggregateType{AggregateType: protoAgg}}
}

// ProtoToType converts a protobuf *btapb.Type to an instance of the Type interface, for use of the admin API.
func ProtoToType(pb *btapb.Type) Type {
	if pb == nil {
		return unknown[btapb.Type]{wrapped: nil}
	}
	if pb.Kind == nil {
		return TypeUnspecified{}
	}
	switch t := pb.Kind.(type) {
	case *btapb.Type_Int64Type:
		return int64ProtoToType(t.Int64Type)
	case *btapb.Type_BytesType:
		return bytesProtoToType(t.BytesType)
	case *btapb.Type_StringType:
		return stringProtoToType(t.StringType)
	case *btapb.Type_TimestampType:
		return timestampProtoToType(t.TimestampType)
	case *btapb.Type_AggregateType:
		return aggregateProtoToType(t.AggregateType)
	case *btapb.Type_StructType:
		return structProtoToType(t.StructType)
	default:
		return unknown[btapb.Type]{wrapped: pb}
	}
}

func bytesEncodingProtoToType(be *btapb.Type_Bytes_Encoding) BytesEncoding {
	if be == nil {
		return unknown[btapb.Type_Bytes_Encoding]{wrapped: be}
	}

	switch be.Encoding.(type) {
	case *btapb.Type_Bytes_Encoding_Raw_:
		return RawBytesEncoding{}
	default:
		return unknown[btapb.Type_Bytes_Encoding]{wrapped: be}
	}
}

func bytesProtoToType(b *btapb.Type_Bytes) BytesType {
	return BytesType{Encoding: bytesEncodingProtoToType(b.Encoding)}
}

func stringEncodingProtoToType(se *btapb.Type_String_Encoding) StringEncoding {
	if se == nil {
		return unknown[btapb.Type_String_Encoding]{wrapped: se}
	}

	switch se.Encoding.(type) {
	case *btapb.Type_String_Encoding_Utf8Raw_:
		return StringUtf8Encoding{}
	case *btapb.Type_String_Encoding_Utf8Bytes_:
		return StringUtf8BytesEncoding{}
	default:
		return unknown[btapb.Type_String_Encoding]{wrapped: se}
	}
}

func stringProtoToType(s *btapb.Type_String) Type {
	return StringType{Encoding: stringEncodingProtoToType(s.Encoding)}
}

func int64EncodingProtoToEncoding(ie *btapb.Type_Int64_Encoding) Int64Encoding {
	if ie == nil {
		return unknown[btapb.Type_Int64_Encoding]{wrapped: ie}
	}

	switch ie.Encoding.(type) {
	case *btapb.Type_Int64_Encoding_BigEndianBytes_:
		return BigEndianBytesEncoding{}
	case *btapb.Type_Int64_Encoding_OrderedCodeBytes_:
		return Int64OrderedCodeBytesEncoding{}
	default:
		return unknown[btapb.Type_Int64_Encoding]{wrapped: ie}
	}
}

func int64ProtoToType(i *btapb.Type_Int64) Type {
	return Int64Type{Encoding: int64EncodingProtoToEncoding(i.Encoding)}
}

func timestampEncodingProtoToType(tse *btapb.Type_Timestamp_Encoding) TimestampEncoding {
	if tse == nil {
		return unknown[btapb.Type_Timestamp_Encoding]{wrapped: tse}
	}

	switch tse.Encoding.(type) {
	case *btapb.Type_Timestamp_Encoding_UnixMicrosInt64:
		return TimestampUnixMicrosInt64Encoding{
			UnixMicrosInt64Encoding: int64EncodingProtoToEncoding(tse.GetUnixMicrosInt64()),
		}
	default:
		return unknown[btapb.Type_Timestamp_Encoding]{wrapped: tse}
	}
}

func timestampProtoToType(tsp *btapb.Type_Timestamp) Type {
	return TimestampType{Encoding: timestampEncodingProtoToType(tsp.Encoding)}
}

func structEncodingProtoToType(se *btapb.Type_Struct_Encoding) StructEncoding {
	if se == nil {
		return unknown[btapb.Type_Struct_Encoding]{wrapped: se}
	}

	switch se.Encoding.(type) {
	case *btapb.Type_Struct_Encoding_DelimitedBytes_:
		return StructDelimitedBytesEncoding{
			Delimiter: se.GetDelimitedBytes().Delimiter,
		}
	case *btapb.Type_Struct_Encoding_OrderedCodeBytes_:
		return StructOrderedCodeBytesEncoding{}
	case *btapb.Type_Struct_Encoding_Singleton_:
		return StructSingletonEncoding{}
	default:
		return unknown[btapb.Type_Struct_Encoding]{wrapped: se}
	}
}

func structProtoToType(sp *btapb.Type_Struct) Type {
	var structFields []StructField
	for _, sfp := range sp.Fields {
		structFields = append(structFields, StructField{
			FieldName: sfp.FieldName, FieldType: ProtoToType(sfp.Type),
		})
	}
	return StructType{Fields: structFields, Encoding: structEncodingProtoToType(sp.Encoding)}
}

func aggregateProtoToType(agg *btapb.Type_Aggregate) AggregateType {
	if agg == nil {
		return AggregateType{Input: nil, Aggregator: unknownAggregator{wrapped: agg}}
	}

	it := ProtoToType(agg.InputType)
	var aggregator Aggregator
	switch agg.Aggregator.(type) {
	case *btapb.Type_Aggregate_Sum_:
		aggregator = SumAggregator{}
	case *btapb.Type_Aggregate_Min_:
		aggregator = MinAggregator{}
	case *btapb.Type_Aggregate_Max_:
		aggregator = MaxAggregator{}
	case *btapb.Type_Aggregate_HllppUniqueCount:
		aggregator = HllppUniqueCountAggregator{}
	default:
		aggregator = unknownAggregator{wrapped: agg}
	}
	return AggregateType{Input: it, Aggregator: aggregator}
}
