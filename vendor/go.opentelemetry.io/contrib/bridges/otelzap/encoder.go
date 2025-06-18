// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package otelzap // import "go.opentelemetry.io/contrib/bridges/otelzap"

import (
	"time"

	"go.uber.org/zap/zapcore"

	"go.opentelemetry.io/otel/log"
)

var (
	_ zapcore.ObjectEncoder = (*objectEncoder)(nil)
	_ zapcore.ArrayEncoder  = (*arrayEncoder)(nil)
)

type namespace struct {
	name  string
	attrs []log.KeyValue
	next  *namespace
}

// objectEncoder implements zapcore.ObjectEncoder.
// It encodes given fields to OTel key-values.
type objectEncoder struct {
	// root is a pointer to the default namespace
	root *namespace
	// cur is a pointer to the namespace we're currently writing to.
	cur *namespace
}

func newObjectEncoder(n int) *objectEncoder {
	keyval := make([]log.KeyValue, 0, n)
	m := &namespace{
		attrs: keyval,
	}
	return &objectEncoder{
		root: m,
		cur:  m,
	}
}

// It iterates to the end of the linked list and appends namespace data.
// Run this function before accessing complete result.
func (m *objectEncoder) calculate(o *namespace) {
	if o.next == nil {
		return
	}
	m.calculate(o.next)
	o.attrs = append(o.attrs, log.Map(o.next.name, o.next.attrs...))
}

func (m *objectEncoder) AddArray(key string, v zapcore.ArrayMarshaler) error {
	arr := newArrayEncoder()
	err := v.MarshalLogArray(arr)
	m.cur.attrs = append(m.cur.attrs, log.Slice(key, arr.elems...))
	return err
}

func (m *objectEncoder) AddObject(k string, v zapcore.ObjectMarshaler) error {
	// Similar to console_encoder which uses capacity of 2:
	// https://github.com/uber-go/zap/blob/bd0cf0447951b77aa98dcfc1ac19e6f58d3ee64f/zapcore/console_encoder.go#L33.
	newobj := newObjectEncoder(2)
	err := v.MarshalLogObject(newobj)
	newobj.calculate(newobj.root)
	m.cur.attrs = append(m.cur.attrs, log.Map(k, newobj.root.attrs...))
	return err
}

func (m *objectEncoder) AddBinary(k string, v []byte) {
	m.cur.attrs = append(m.cur.attrs, log.Bytes(k, v))
}

func (m *objectEncoder) AddByteString(k string, v []byte) {
	m.cur.attrs = append(m.cur.attrs, log.String(k, string(v)))
}

func (m *objectEncoder) AddBool(k string, v bool) {
	m.cur.attrs = append(m.cur.attrs, log.Bool(k, v))
}

func (m *objectEncoder) AddDuration(k string, v time.Duration) {
	m.AddInt64(k, v.Nanoseconds())
}

func (m *objectEncoder) AddComplex128(k string, v complex128) {
	r := log.Float64("r", real(v))
	i := log.Float64("i", imag(v))
	m.cur.attrs = append(m.cur.attrs, log.Map(k, r, i))
}

func (m *objectEncoder) AddFloat64(k string, v float64) {
	m.cur.attrs = append(m.cur.attrs, log.Float64(k, v))
}

func (m *objectEncoder) AddInt64(k string, v int64) {
	m.cur.attrs = append(m.cur.attrs, log.Int64(k, v))
}

func (m *objectEncoder) AddInt(k string, v int) {
	m.cur.attrs = append(m.cur.attrs, log.Int(k, v))
}

func (m *objectEncoder) AddString(k string, v string) {
	m.cur.attrs = append(m.cur.attrs, log.String(k, v))
}

func (m *objectEncoder) AddUint64(k string, v uint64) {
	m.cur.attrs = append(m.cur.attrs,
		log.KeyValue{
			Key:   k,
			Value: assignUintValue(v),
		})
}

func (m *objectEncoder) AddReflected(k string, v interface{}) error {
	m.cur.attrs = append(m.cur.attrs,
		log.KeyValue{
			Key:   k,
			Value: convertValue(v),
		})
	return nil
}

// OpenNamespace opens an isolated namespace where all subsequent fields will
// be added.
func (m *objectEncoder) OpenNamespace(k string) {
	keyValue := make([]log.KeyValue, 0, 5)
	s := &namespace{
		name:  k,
		attrs: keyValue,
	}
	m.cur.next = s
	m.cur = s
}

func (m *objectEncoder) AddComplex64(k string, v complex64) {
	m.AddComplex128(k, complex128(v))
}

func (m *objectEncoder) AddTime(k string, v time.Time) {
	m.AddInt64(k, v.UnixNano())
}

func (m *objectEncoder) AddFloat32(k string, v float32) {
	m.AddFloat64(k, float64(v))
}

func (m *objectEncoder) AddInt32(k string, v int32) {
	m.AddInt64(k, int64(v))
}

func (m *objectEncoder) AddInt16(k string, v int16) {
	m.AddInt64(k, int64(v))
}

func (m *objectEncoder) AddInt8(k string, v int8) {
	m.AddInt64(k, int64(v))
}

func (m *objectEncoder) AddUint(k string, v uint) {
	m.AddUint64(k, uint64(v))
}

func (m *objectEncoder) AddUint32(k string, v uint32) {
	m.AddInt64(k, int64(v))
}

func (m *objectEncoder) AddUint16(k string, v uint16) {
	m.AddInt64(k, int64(v))
}

func (m *objectEncoder) AddUint8(k string, v uint8) {
	m.AddInt64(k, int64(v))
}

func (m *objectEncoder) AddUintptr(k string, v uintptr) {
	m.AddUint64(k, uint64(v))
}

func assignUintValue(v uint64) log.Value {
	const maxInt64 = ^uint64(0) >> 1
	if v > maxInt64 {
		return log.Float64Value(float64(v))
	}
	return log.Int64Value(int64(v)) // nolint:gosec  // Overflow checked above.
}

// arrayEncoder implements [zapcore.ArrayEncoder].
type arrayEncoder struct {
	elems []log.Value
}

func newArrayEncoder() *arrayEncoder {
	return &arrayEncoder{
		// Similar to console_encoder which uses capacity of 2:
		// https://github.com/uber-go/zap/blob/bd0cf0447951b77aa98dcfc1ac19e6f58d3ee64f/zapcore/console_encoder.go#L33.
		elems: make([]log.Value, 0, 2),
	}
}

func (a *arrayEncoder) AppendArray(v zapcore.ArrayMarshaler) error {
	arr := newArrayEncoder()
	err := v.MarshalLogArray(arr)
	a.elems = append(a.elems, log.SliceValue(arr.elems...))
	return err
}

func (a *arrayEncoder) AppendObject(v zapcore.ObjectMarshaler) error {
	// Similar to console_encoder which uses capacity of 2:
	// https://github.com/uber-go/zap/blob/bd0cf0447951b77aa98dcfc1ac19e6f58d3ee64f/zapcore/console_encoder.go#L33.
	m := newObjectEncoder(2)
	err := v.MarshalLogObject(m)
	m.calculate(m.root)
	a.elems = append(a.elems, log.MapValue(m.root.attrs...))
	return err
}

func (a *arrayEncoder) AppendReflected(v interface{}) error {
	a.elems = append(a.elems, convertValue(v))
	return nil
}

func (a *arrayEncoder) AppendByteString(v []byte) {
	a.elems = append(a.elems, log.StringValue(string(v)))
}

func (a *arrayEncoder) AppendBool(v bool) {
	a.elems = append(a.elems, log.BoolValue(v))
}

func (a *arrayEncoder) AppendFloat64(v float64) {
	a.elems = append(a.elems, log.Float64Value(v))
}

func (a *arrayEncoder) AppendFloat32(v float32) {
	a.AppendFloat64(float64(v))
}

func (a *arrayEncoder) AppendInt(v int) {
	a.elems = append(a.elems, log.IntValue(v))
}

func (a *arrayEncoder) AppendInt64(v int64) {
	a.elems = append(a.elems, log.Int64Value(v))
}

func (a *arrayEncoder) AppendString(v string) {
	a.elems = append(a.elems, log.StringValue(v))
}

func (a *arrayEncoder) AppendComplex128(v complex128) {
	r := log.Float64("r", real(v))
	i := log.Float64("i", imag(v))
	a.elems = append(a.elems, log.MapValue(r, i))
}

func (a *arrayEncoder) AppendUint64(v uint64) {
	a.elems = append(a.elems, assignUintValue(v))
}

func (a *arrayEncoder) AppendComplex64(v complex64)    { a.AppendComplex128(complex128(v)) }
func (a *arrayEncoder) AppendDuration(v time.Duration) { a.AppendInt64(v.Nanoseconds()) }
func (a *arrayEncoder) AppendInt32(v int32)            { a.AppendInt64(int64(v)) }
func (a *arrayEncoder) AppendInt16(v int16)            { a.AppendInt64(int64(v)) }
func (a *arrayEncoder) AppendInt8(v int8)              { a.AppendInt64(int64(v)) }
func (a *arrayEncoder) AppendTime(v time.Time)         { a.AppendInt64(v.UnixNano()) }
func (a *arrayEncoder) AppendUint(v uint)              { a.AppendUint64(uint64(v)) }
func (a *arrayEncoder) AppendUint32(v uint32)          { a.AppendInt64(int64(v)) }
func (a *arrayEncoder) AppendUint16(v uint16)          { a.AppendInt64(int64(v)) }
func (a *arrayEncoder) AppendUint8(v uint8)            { a.AppendInt64(int64(v)) }
func (a *arrayEncoder) AppendUintptr(v uintptr)        { a.AppendUint64(uint64(v)) }
