package zaplogfmt

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"go.uber.org/zap"
	"go.uber.org/zap/buffer"
	"go.uber.org/zap/zapcore"
)

const (
	// For JSON-escaping; see logfmtEncoder.safeAddString below.
	_hex = "0123456789abcdef"
)

var (
	_logfmtPool = sync.Pool{New: func() interface{} {
		return &logfmtEncoder{}
	}}

	bufferpool = buffer.NewPool()
)

var ErrUnsupportedValueType = errors.New("unsupported value type")

func getEncoder() *logfmtEncoder {
	return _logfmtPool.Get().(*logfmtEncoder)
}

func putEncoder(enc *logfmtEncoder) {
	enc.EncoderConfig = nil
	enc.buf = nil
	_logfmtPool.Put(enc)
}

type logfmtEncoder struct {
	*zapcore.EncoderConfig
	buf        *buffer.Buffer
	namespaces []string
}

func NewEncoder(cfg zapcore.EncoderConfig) zapcore.Encoder {
	return &logfmtEncoder{
		EncoderConfig: &cfg,
		buf:           bufferpool.Get(),
	}
}

func (enc *logfmtEncoder) AddArray(key string, arr zapcore.ArrayMarshaler) error {
	enc.addKey(key)
	return enc.AppendArray(arr)
}

func (enc *logfmtEncoder) AddObject(key string, obj zapcore.ObjectMarshaler) error {
	return ErrUnsupportedValueType
}

func (enc *logfmtEncoder) AddBinary(key string, value []byte) {
	enc.AddString(key, base64.StdEncoding.EncodeToString(value))
}

func (enc *logfmtEncoder) AddByteString(key string, value []byte) {
	enc.addKey(key)
	enc.AppendByteString(value)
}

func (enc *logfmtEncoder) AddBool(key string, value bool) {
	enc.addKey(key)
	enc.AppendBool(value)
}

func (enc *logfmtEncoder) AddComplex128(key string, value complex128) {
	enc.addKey(key)
	enc.AppendComplex128(value)
}

func (enc *logfmtEncoder) AddDuration(key string, value time.Duration) {
	enc.addKey(key)
	enc.AppendDuration(value)
}

func (enc *logfmtEncoder) AddFloat64(key string, value float64) {
	enc.addKey(key)
	enc.AppendFloat64(value)
}

func (enc *logfmtEncoder) AddInt64(key string, value int64) {
	enc.addKey(key)
	enc.AppendInt64(value)
}

func (enc *logfmtEncoder) AddReflected(key string, value interface{}) error {
	rvalue := reflect.ValueOf(value)
	switch rvalue.Kind() {
	case reflect.Chan, reflect.Func, reflect.Map, reflect.Struct:
		return ErrUnsupportedValueType
	case reflect.Array, reflect.Slice, reflect.Ptr:
		if rvalue.IsNil() {
			enc.AddByteString(key, nil)
			return nil
		}
		return enc.AddReflected(key, rvalue.Elem().Interface())
	}
	enc.AddString(key, fmt.Sprint(value))
	return nil
}

func (enc *logfmtEncoder) OpenNamespace(key string) {
	enc.namespaces = append(enc.namespaces, key)
}

func (enc *logfmtEncoder) AddString(key, value string) {
	enc.addKey(key)
	enc.AppendString(value)
}

func (enc *logfmtEncoder) AddTime(key string, value time.Time) {
	enc.addKey(key)
	enc.AppendTime(value)
}

func (enc *logfmtEncoder) AddUint64(key string, value uint64) {
	enc.addKey(key)
	enc.AppendUint64(value)
}

func (enc *logfmtEncoder) AppendArray(arr zapcore.ArrayMarshaler) error {
	marshaler := literalEncoder{
		EncoderConfig: enc.EncoderConfig,
		buf:           bufferpool.Get(),
	}

	err := arr.MarshalLogArray(&marshaler)
	if err == nil {
		enc.AppendByteString(marshaler.buf.Bytes())
	} else {
		enc.AppendByteString(nil)
	}
	marshaler.buf.Free()
	return err
}

func (enc *logfmtEncoder) AppendObject(obj zapcore.ObjectMarshaler) error {
	return ErrUnsupportedValueType
}

func (enc *logfmtEncoder) AppendBool(value bool) {
	if value {
		enc.AppendString("true")
	} else {
		enc.AppendString("false")
	}
}

func (enc *logfmtEncoder) AppendByteString(value []byte) {
	needsQuotes := bytes.IndexFunc(value, needsQuotedValueRune) != -1
	if needsQuotes {
		enc.buf.AppendByte('"')
	}
	enc.safeAddByteString(value)
	if needsQuotes {
		enc.buf.AppendByte('"')
	}
}

func (enc *logfmtEncoder) AppendComplex128(value complex128) {
	// Cast to a platform-independent, fixed-size type.
	r, i := float64(real(value)), float64(imag(value))
	enc.buf.AppendFloat(r, 64)
	enc.buf.AppendByte('+')
	enc.buf.AppendFloat(i, 64)
	enc.buf.AppendByte('i')
}

func (enc *logfmtEncoder) AppendDuration(value time.Duration) {
	cur := enc.buf.Len()
	enc.EncodeDuration(value, enc)
	if cur == enc.buf.Len() {
		// User-supplied EncodeDuration is a no-op. Fall back to nanoseconds.
		enc.AppendInt64(int64(value))
	}
}

func (enc *logfmtEncoder) AppendInt64(value int64) {
	enc.buf.AppendInt(value)
}

func (enc *logfmtEncoder) AppendReflected(value interface{}) error {
	rvalue := reflect.ValueOf(value)
	switch rvalue.Kind() {
	case reflect.Array, reflect.Chan, reflect.Func, reflect.Map, reflect.Slice, reflect.Struct:
		return ErrUnsupportedValueType
	case reflect.Ptr:
		if rvalue.IsNil() {
			enc.AppendByteString(nil)
			return nil
		}
		return enc.AppendReflected(rvalue.Elem().Interface())
	}
	enc.AppendString(fmt.Sprint(value))
	return nil
}

func (enc *logfmtEncoder) AppendString(value string) {
	needsQuotes := strings.IndexFunc(value, needsQuotedValueRune) != -1
	if needsQuotes {
		enc.buf.AppendByte('"')
	}
	enc.safeAddString(value)
	if needsQuotes {
		enc.buf.AppendByte('"')
	}
}

func (enc *logfmtEncoder) AppendTime(value time.Time) {
	cur := enc.buf.Len()
	enc.EncodeTime(value, enc)
	if cur == enc.buf.Len() {
		enc.AppendInt64(value.UnixNano())
	}
}

func (enc *logfmtEncoder) AppendUint64(value uint64) {
	enc.buf.AppendUint(value)
}

func (enc *logfmtEncoder) AddComplex64(k string, v complex64) { enc.AddComplex128(k, complex128(v)) }
func (enc *logfmtEncoder) AddFloat32(k string, v float32)     { enc.AddFloat64(k, float64(v)) }
func (enc *logfmtEncoder) AddInt(k string, v int)             { enc.AddInt64(k, int64(v)) }
func (enc *logfmtEncoder) AddInt32(k string, v int32)         { enc.AddInt64(k, int64(v)) }
func (enc *logfmtEncoder) AddInt16(k string, v int16)         { enc.AddInt64(k, int64(v)) }
func (enc *logfmtEncoder) AddInt8(k string, v int8)           { enc.AddInt64(k, int64(v)) }
func (enc *logfmtEncoder) AddUint(k string, v uint)           { enc.AddUint64(k, uint64(v)) }
func (enc *logfmtEncoder) AddUint32(k string, v uint32)       { enc.AddUint64(k, uint64(v)) }
func (enc *logfmtEncoder) AddUint16(k string, v uint16)       { enc.AddUint64(k, uint64(v)) }
func (enc *logfmtEncoder) AddUint8(k string, v uint8)         { enc.AddUint64(k, uint64(v)) }
func (enc *logfmtEncoder) AddUintptr(k string, v uintptr)     { enc.AddUint64(k, uint64(v)) }
func (enc *logfmtEncoder) AppendComplex64(v complex64)        { enc.AppendComplex128(complex128(v)) }
func (enc *logfmtEncoder) AppendFloat64(v float64)            { enc.appendFloat(v, 64) }
func (enc *logfmtEncoder) AppendFloat32(v float32)            { enc.appendFloat(float64(v), 32) }
func (enc *logfmtEncoder) AppendInt(v int)                    { enc.AppendInt64(int64(v)) }
func (enc *logfmtEncoder) AppendInt32(v int32)                { enc.AppendInt64(int64(v)) }
func (enc *logfmtEncoder) AppendInt16(v int16)                { enc.AppendInt64(int64(v)) }
func (enc *logfmtEncoder) AppendInt8(v int8)                  { enc.AppendInt64(int64(v)) }
func (enc *logfmtEncoder) AppendUint(v uint)                  { enc.AppendUint64(uint64(v)) }
func (enc *logfmtEncoder) AppendUint32(v uint32)              { enc.AppendUint64(uint64(v)) }
func (enc *logfmtEncoder) AppendUint16(v uint16)              { enc.AppendUint64(uint64(v)) }
func (enc *logfmtEncoder) AppendUint8(v uint8)                { enc.AppendUint64(uint64(v)) }
func (enc *logfmtEncoder) AppendUintptr(v uintptr)            { enc.AppendUint64(uint64(v)) }

func (enc *logfmtEncoder) Clone() zapcore.Encoder {
	clone := enc.clone()
	clone.buf.Write(enc.buf.Bytes())
	return clone
}

func (enc *logfmtEncoder) clone() *logfmtEncoder {
	clone := getEncoder()
	clone.EncoderConfig = enc.EncoderConfig
	clone.buf = bufferpool.Get()
	clone.namespaces = enc.namespaces
	return clone
}

func (enc *logfmtEncoder) EncodeEntry(ent zapcore.Entry, fields []zapcore.Field) (*buffer.Buffer, error) {
	final := enc.clone()
	if final.TimeKey != "" {
		final.AddTime(final.TimeKey, ent.Time)
	}
	if final.LevelKey != "" {
		final.addKey(final.LevelKey)
		cur := final.buf.Len()
		final.EncodeLevel(ent.Level, final)
		if cur == final.buf.Len() {
			// User-supplied EncodeLevel was a no-op. Fall back to strings to keep
			// output valid.
			final.AppendString(ent.Level.String())
		}
	}
	if ent.Caller.Defined && final.CallerKey != "" {
		final.addKey(final.CallerKey)
		cur := final.buf.Len()
		final.EncodeCaller(ent.Caller, final)
		if cur == final.buf.Len() {
			// User-supplied EncodeCaller was a no-op. Fall back to strings to
			// keep output valid.
			final.AppendString(ent.Caller.String())
		}
	}
	if final.MessageKey != "" {
		final.addKey(enc.MessageKey)
		final.AppendString(ent.Message)
	}
	if enc.buf.Len() > 0 {
		final.buf.AppendByte(' ')
		final.buf.Write(enc.buf.Bytes())
	}
	addFields(final, fields)
	if ent.Stack != "" && final.StacktraceKey != "" {
		final.AddString(final.StacktraceKey, ent.Stack)
	}
	if final.LineEnding != "" {
		final.buf.AppendString(final.LineEnding)
	} else {
		final.buf.AppendString(zapcore.DefaultLineEnding)
	}

	ret := final.buf
	putEncoder(final)
	return ret, nil
}

func (enc *logfmtEncoder) truncate() {
	enc.buf.Reset()
	enc.namespaces = nil
}

func (enc *logfmtEncoder) addKey(key string) {
	if enc.buf.Len() > 0 {
		enc.buf.AppendByte(' ')
	}
	for _, ns := range enc.namespaces {
		enc.safeAddString(ns)
		enc.buf.AppendByte('.')
	}
	enc.safeAddString(key)
	enc.buf.AppendByte('=')
}

func (enc *logfmtEncoder) appendFloat(val float64, bitSize int) {
	switch {
	case math.IsNaN(val):
		enc.buf.AppendString(`NaN`)
	case math.IsInf(val, 1):
		enc.buf.AppendString(`+Inf`)
	case math.IsInf(val, -1):
		enc.buf.AppendString(`-Inf`)
	default:
		enc.buf.AppendFloat(val, bitSize)
	}
}

// safeAddString JSON-escapes a string and appends it to the internal buffer.
// Unlike the standard library's encoder, it doesn't attempt to protect the
// user from browser vulnerabilities or JSONP-related problems.
func (enc *logfmtEncoder) safeAddString(s string) {
	for i := 0; i < len(s); {
		if enc.tryAddRuneSelf(s[i]) {
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if enc.tryAddRuneError(r, size) {
			i++
			continue
		}
		enc.buf.AppendString(s[i : i+size])
		i += size
	}
}

// safeAddByteString is no-alloc equivalent of safeAddString(string(s)) for s []byte.
func (enc *logfmtEncoder) safeAddByteString(s []byte) {
	for i := 0; i < len(s); {
		if enc.tryAddRuneSelf(s[i]) {
			i++
			continue
		}
		r, size := utf8.DecodeRune(s[i:])
		if enc.tryAddRuneError(r, size) {
			i++
			continue
		}
		enc.buf.Write(s[i : i+size])
		i += size
	}
}

// tryAddRuneSelf appends b if it is valid UTF-8 character represented in a single byte.
func (enc *logfmtEncoder) tryAddRuneSelf(b byte) bool {
	if b >= utf8.RuneSelf {
		return false
	}
	if 0x20 <= b && b != '\\' && b != '"' {
		enc.buf.AppendByte(b)
		return true
	}
	switch b {
	case '\\', '"':
		enc.buf.AppendByte('\\')
		enc.buf.AppendByte(b)
	case '\n':
		enc.buf.AppendByte('\\')
		enc.buf.AppendByte('n')
	case '\r':
		enc.buf.AppendByte('\\')
		enc.buf.AppendByte('r')
	case '\t':
		enc.buf.AppendByte('\\')
		enc.buf.AppendByte('t')
	default:
		// Encode bytes < 0x20, except for the escape sequences above.
		enc.buf.AppendString(`\u00`)
		enc.buf.AppendByte(_hex[b>>4])
		enc.buf.AppendByte(_hex[b&0xF])
	}
	return true
}

func (enc *logfmtEncoder) tryAddRuneError(r rune, size int) bool {
	if r == utf8.RuneError && size == 1 {
		enc.buf.AppendString(`\ufffd`)
		return true
	}
	return false
}

type literalEncoder struct {
	*zapcore.EncoderConfig
	buf *buffer.Buffer
}

func (enc *literalEncoder) AppendBool(value bool) {
	enc.addSeparator()
	if value {
		enc.AppendString("true")
	} else {
		enc.AppendString("false")
	}
}

func (enc *literalEncoder) AppendByteString(value []byte) {
	enc.addSeparator()
	enc.buf.AppendString(string(value))
}

func (enc *literalEncoder) AppendComplex128(value complex128) {
	enc.addSeparator()
	// Cast to a platform-independent, fixed-size type.
	r, i := float64(real(value)), float64(imag(value))
	enc.buf.AppendFloat(r, 64)
	enc.buf.AppendByte('+')
	enc.buf.AppendFloat(i, 64)
	enc.buf.AppendByte('i')
}

func (enc *literalEncoder) AppendComplex64(value complex64) {
	enc.AppendComplex128(complex128(value))
}

func (enc *literalEncoder) AppendFloat64(value float64) {
	enc.addSeparator()
	enc.buf.AppendFloat(value, 64)
}

func (enc *literalEncoder) AppendFloat32(value float32) {
	enc.addSeparator()
	enc.buf.AppendFloat(float64(value), 32)
}

func (enc *literalEncoder) AppendInt64(value int64) {
	enc.addSeparator()
	enc.buf.AppendInt(value)
}

func (enc *literalEncoder) AppendInt(v int)     { enc.AppendInt64(int64(v)) }
func (enc *literalEncoder) AppendInt32(v int32) { enc.AppendInt64(int64(v)) }
func (enc *literalEncoder) AppendInt16(v int16) { enc.AppendInt64(int64(v)) }
func (enc *literalEncoder) AppendInt8(v int8)   { enc.AppendInt64(int64(v)) }

func (enc *literalEncoder) AppendString(value string) {
	enc.addSeparator()
	enc.buf.AppendString(value)
}

func (enc *literalEncoder) AppendUint64(value uint64) {
	enc.addSeparator()
	enc.buf.AppendUint(value)
}

func (enc *literalEncoder) AppendUint(v uint)       { enc.AppendUint64(uint64(v)) }
func (enc *literalEncoder) AppendUint32(v uint32)   { enc.AppendUint64(uint64(v)) }
func (enc *literalEncoder) AppendUint16(v uint16)   { enc.AppendUint64(uint64(v)) }
func (enc *literalEncoder) AppendUint8(v uint8)     { enc.AppendUint64(uint64(v)) }
func (enc *literalEncoder) AppendUintptr(v uintptr) { enc.AppendUint64(uint64(v)) }

func (enc *literalEncoder) AppendDuration(value time.Duration) {
	cur := enc.buf.Len()
	enc.EncodeDuration(value, enc)
	if cur == enc.buf.Len() {
		// User-supplied EncodeDuration is a no-op. Fall back to nanoseconds.
		enc.AppendInt64(int64(value))
	}
}

func (enc *literalEncoder) AppendTime(value time.Time) {
	cur := enc.buf.Len()
	enc.EncodeTime(value, enc)
	if cur == enc.buf.Len() {
		enc.AppendInt64(value.UnixNano())
	}
}

func (enc *literalEncoder) AppendArray(arr zapcore.ArrayMarshaler) error {
	return arr.MarshalLogArray(enc)
}

func (enc *literalEncoder) AppendObject(zapcore.ObjectMarshaler) error {
	return ErrUnsupportedValueType
}

func (enc *literalEncoder) AppendReflected(value interface{}) error {
	return ErrUnsupportedValueType
}

func (enc *literalEncoder) addSeparator() {
	if enc.buf.Len() > 0 {
		enc.buf.AppendByte(',')
	}
}

func needsQuotedValueRune(r rune) bool {
	return r <= ' ' || r == '=' || r == '"' || r == utf8.RuneError
}

func addFields(enc zapcore.ObjectEncoder, fields []zapcore.Field) {
	for i := range fields {
		fields[i].AddTo(enc)
	}
}

func init() {
	zap.RegisterEncoder("logfmt", func(cfg zapcore.EncoderConfig) (zapcore.Encoder, error) {
		enc := NewEncoder(cfg)
		return enc, nil
	})
}
