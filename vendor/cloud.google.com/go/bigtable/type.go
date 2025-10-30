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

import btapb "cloud.google.com/go/bigtable/admin/apiv2/adminpb"

// Type wraps the protobuf representation of a type. See the protobuf definition
// for more details on types.
type Type interface {
	proto() *btapb.Type
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

// StringUtf8Encoding represents a string with UTF-8 encoding.
type StringUtf8Encoding struct {
}

func (encoding StringUtf8Encoding) proto() *btapb.Type_String_Encoding {
	return &btapb.Type_String_Encoding{
		Encoding: &btapb.Type_String_Encoding_Utf8Raw_{},
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

	switch t := pb.Kind.(type) {
	case *btapb.Type_Int64Type:
		return int64ProtoToType(t.Int64Type)
	case *btapb.Type_BytesType:
		return bytesProtoToType(t.BytesType)
	case *btapb.Type_AggregateType:
		return aggregateProtoToType(t.AggregateType)
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

func int64EncodingProtoToEncoding(ie *btapb.Type_Int64_Encoding) Int64Encoding {
	if ie == nil {
		return unknown[btapb.Type_Int64_Encoding]{wrapped: ie}
	}

	switch ie.Encoding.(type) {
	case *btapb.Type_Int64_Encoding_BigEndianBytes_:
		return BigEndianBytesEncoding{}
	default:
		return unknown[btapb.Type_Int64_Encoding]{wrapped: ie}
	}
}

func int64ProtoToType(i *btapb.Type_Int64) Type {
	return Int64Type{Encoding: int64EncodingProtoToEncoding(i.Encoding)}
}

func aggregateProtoToType(agg *btapb.Type_Aggregate) Type {
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
