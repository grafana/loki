package otelzap

import (
	"fmt"
	"time"

	"go.uber.org/zap/zapcore"
)

// bufferArrayEncoder implements zapcore.bufferArrayEncoder.
// It represents all added objects to their string values and
// adds them to the stringsSlice buffer.
type bufferArrayEncoder struct {
	stringsSlice []string
}

var _ zapcore.ArrayEncoder = (*bufferArrayEncoder)(nil)

func (t *bufferArrayEncoder) AppendComplex128(v complex128) {
	t.stringsSlice = append(t.stringsSlice, fmt.Sprintf("%v", v))
}

func (t *bufferArrayEncoder) AppendComplex64(v complex64) {
	t.stringsSlice = append(t.stringsSlice, fmt.Sprintf("%v", v))
}

func (t *bufferArrayEncoder) AppendArray(v zapcore.ArrayMarshaler) error {
	enc := &bufferArrayEncoder{}
	err := v.MarshalLogArray(enc)
	t.stringsSlice = append(t.stringsSlice, fmt.Sprintf("%v", enc.stringsSlice))
	return err
}

func (t *bufferArrayEncoder) AppendObject(v zapcore.ObjectMarshaler) error {
	m := zapcore.NewMapObjectEncoder()
	err := v.MarshalLogObject(m)
	t.stringsSlice = append(t.stringsSlice, fmt.Sprintf("%v", m.Fields))
	return err
}

func (t *bufferArrayEncoder) AppendReflected(v interface{}) error {
	t.stringsSlice = append(t.stringsSlice, fmt.Sprintf("%v", v))
	return nil
}

func (t *bufferArrayEncoder) AppendBool(v bool) {
	t.stringsSlice = append(t.stringsSlice, fmt.Sprintf("%v", v))
}

func (t *bufferArrayEncoder) AppendByteString(v []byte) {
	t.stringsSlice = append(t.stringsSlice, fmt.Sprintf("%v", v))
}

func (t *bufferArrayEncoder) AppendDuration(v time.Duration) {
	t.stringsSlice = append(t.stringsSlice, fmt.Sprintf("%v", v))
}

func (t *bufferArrayEncoder) AppendFloat64(v float64) {
	t.stringsSlice = append(t.stringsSlice, fmt.Sprintf("%v", v))
}

func (t *bufferArrayEncoder) AppendFloat32(v float32) {
	t.stringsSlice = append(t.stringsSlice, fmt.Sprintf("%v", v))
}

func (t *bufferArrayEncoder) AppendInt(v int) {
	t.stringsSlice = append(t.stringsSlice, fmt.Sprintf("%v", v))
}

func (t *bufferArrayEncoder) AppendInt64(v int64) {
	t.stringsSlice = append(t.stringsSlice, fmt.Sprintf("%v", v))
}

func (t *bufferArrayEncoder) AppendInt32(v int32) {
	t.stringsSlice = append(t.stringsSlice, fmt.Sprintf("%v", v))
}

func (t *bufferArrayEncoder) AppendInt16(v int16) {
	t.stringsSlice = append(t.stringsSlice, fmt.Sprintf("%v", v))
}

func (t *bufferArrayEncoder) AppendInt8(v int8) {
	t.stringsSlice = append(t.stringsSlice, fmt.Sprintf("%v", v))
}

func (t *bufferArrayEncoder) AppendString(v string) {
	t.stringsSlice = append(t.stringsSlice, v)
}

func (t *bufferArrayEncoder) AppendTime(v time.Time) {
	t.stringsSlice = append(t.stringsSlice, fmt.Sprintf("%v", v))
}

func (t *bufferArrayEncoder) AppendUint(v uint) {
	t.stringsSlice = append(t.stringsSlice, fmt.Sprintf("%v", v))
}

func (t *bufferArrayEncoder) AppendUint64(v uint64) {
	t.stringsSlice = append(t.stringsSlice, fmt.Sprintf("%v", v))
}

func (t *bufferArrayEncoder) AppendUint32(v uint32) {
	t.stringsSlice = append(t.stringsSlice, fmt.Sprintf("%v", v))
}

func (t *bufferArrayEncoder) AppendUint16(v uint16) {
	t.stringsSlice = append(t.stringsSlice, fmt.Sprintf("%v", v))
}

func (t *bufferArrayEncoder) AppendUint8(v uint8) {
	t.stringsSlice = append(t.stringsSlice, fmt.Sprintf("%v", v))
}

func (t *bufferArrayEncoder) AppendUintptr(v uintptr) {
	t.stringsSlice = append(t.stringsSlice, fmt.Sprintf("%v", v))
}
