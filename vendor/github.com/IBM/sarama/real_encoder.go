package sarama

import (
	"encoding/binary"
	"errors"
	"math"
	"time"

	"github.com/rcrowley/go-metrics"
)

type realEncoder struct {
	raw      []byte
	off      int
	stack    []pushEncoder
	registry metrics.Registry
}

type realFlexibleEncoder struct {
	*realEncoder
}

// primitives

func (re *realEncoder) putInt8(in int8) {
	re.raw[re.off] = byte(in)
	re.off++
}

func (re *realEncoder) putInt16(in int16) {
	binary.BigEndian.PutUint16(re.raw[re.off:], uint16(in))
	re.off += 2
}

func (re *realEncoder) putInt32(in int32) {
	binary.BigEndian.PutUint32(re.raw[re.off:], uint32(in))
	re.off += 4
}

func (re *realEncoder) putInt64(in int64) {
	binary.BigEndian.PutUint64(re.raw[re.off:], uint64(in))
	re.off += 8
}

func (re *realEncoder) putVarint(in int64) {
	re.off += binary.PutVarint(re.raw[re.off:], in)
}

func (re *realEncoder) putUVarint(in uint64) {
	re.off += binary.PutUvarint(re.raw[re.off:], in)
}

func (re *realEncoder) putFloat64(in float64) {
	binary.BigEndian.PutUint64(re.raw[re.off:], math.Float64bits(in))
	re.off += 8
}

func (re *realEncoder) putArrayLength(in int) error {
	re.putInt32(int32(in))
	return nil
}

func (re *realEncoder) putBool(in bool) {
	if in {
		re.putInt8(1)
		return
	}
	re.putInt8(0)
}

func (re *realEncoder) putKError(in KError) {
	re.putInt16(int16(in))
}

func (re *realEncoder) putDurationMs(in time.Duration) {
	re.putInt32(int32(in / time.Millisecond))
}

// collection

func (re *realEncoder) putRawBytes(in []byte) error {
	copy(re.raw[re.off:], in)
	re.off += len(in)
	return nil
}

func (re *realEncoder) putBytes(in []byte) error {
	if in == nil {
		re.putInt32(-1)
		return nil
	}
	re.putInt32(int32(len(in)))
	return re.putRawBytes(in)
}

func (re *realEncoder) putVarintBytes(in []byte) error {
	if in == nil {
		re.putVarint(-1)
		return nil
	}
	re.putVarint(int64(len(in)))
	return re.putRawBytes(in)
}

func (re *realEncoder) putString(in string) error {
	re.putInt16(int16(len(in)))
	copy(re.raw[re.off:], in)
	re.off += len(in)
	return nil
}

func (re *realEncoder) putNullableString(in *string) error {
	if in == nil {
		re.putInt16(-1)
		return nil
	}
	return re.putString(*in)
}

func (re *realEncoder) putStringArray(in []string) error {
	err := re.putArrayLength(len(in))
	if err != nil {
		return err
	}

	for _, val := range in {
		if err := re.putString(val); err != nil {
			return err
		}
	}

	return nil
}

func (re *realEncoder) putInt32Array(in []int32) error {
	err := re.putArrayLength(len(in))
	if err != nil {
		return err
	}
	for _, val := range in {
		re.putInt32(val)
	}
	return nil
}

func (re *realEncoder) putNullableInt32Array(in []int32) error {
	if in == nil {
		re.putInt32(-1)
		return nil
	}
	err := re.putArrayLength(len(in))
	if err != nil {
		return err
	}
	for _, val := range in {
		re.putInt32(val)
	}
	return nil
}

func (re *realEncoder) putInt64Array(in []int64) error {
	err := re.putArrayLength(len(in))
	if err != nil {
		return err
	}
	for _, val := range in {
		re.putInt64(val)
	}
	return nil
}

func (re *realEncoder) putEmptyTaggedFieldArray() {
}

func (re *realEncoder) offset() int {
	return re.off
}

// stacks

func (re *realEncoder) push(in pushEncoder) {
	in.saveOffset(re.off)
	re.off += in.reserveLength()
	re.stack = append(re.stack, in)
}

func (re *realEncoder) pop() error {
	// this is go's ugly pop pattern (the inverse of append)
	in := re.stack[len(re.stack)-1]
	re.stack = re.stack[:len(re.stack)-1]

	return in.run(re.off, re.raw)
}

// we do record metrics during the real encoder pass
func (re *realEncoder) metricRegistry() metrics.Registry {
	return re.registry
}

func (re *realFlexibleEncoder) putArrayLength(in int) error {
	// 0 represents a null array, so +1 has to be added
	re.putUVarint(uint64(in + 1))
	return nil
}

func (re *realFlexibleEncoder) putBytes(in []byte) error {
	re.putUVarint(uint64(len(in) + 1))
	return re.putRawBytes(in)
}

func (re *realFlexibleEncoder) putString(in string) error {
	if err := re.putArrayLength(len(in)); err != nil {
		return err
	}
	return re.putRawBytes([]byte(in))
}

func (re *realFlexibleEncoder) putNullableString(in *string) error {
	if in == nil {
		re.putInt8(0)
		return nil
	}
	return re.putString(*in)
}

func (re *realFlexibleEncoder) putStringArray(in []string) error {
	err := re.putArrayLength(len(in))
	if err != nil {
		return err
	}

	for _, val := range in {
		if err := re.putString(val); err != nil {
			return err
		}
	}

	return nil
}

func (re *realFlexibleEncoder) putInt32Array(in []int32) error {
	if in == nil {
		return errors.New("expected int32 array to be non null")
	}
	// 0 represents a null array, so +1 has to be added
	re.putUVarint(uint64(len(in)) + 1)
	for _, val := range in {
		re.putInt32(val)
	}
	return nil
}

func (re *realFlexibleEncoder) putNullableInt32Array(in []int32) error {
	if in == nil {
		re.putUVarint(0)
		return nil
	}
	// 0 represents a null array, so +1 has to be added
	re.putUVarint(uint64(len(in)) + 1)
	for _, val := range in {
		re.putInt32(val)
	}
	return nil
}

func (re *realFlexibleEncoder) putEmptyTaggedFieldArray() {
	re.putUVarint(0)
}
