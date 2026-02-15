package otelzap

import (
	"fmt"
	"math"
	"reflect"
	"strconv"
	"time"

	"go.opentelemetry.io/otel/log"
	"go.uber.org/zap/zapcore"
)

func convertLevel(level zapcore.Level) log.Severity {
	switch level {
	case zapcore.DebugLevel:
		return log.SeverityDebug
	case zapcore.InfoLevel:
		return log.SeverityInfo
	case zapcore.WarnLevel:
		return log.SeverityWarn
	case zapcore.ErrorLevel:
		return log.SeverityError
	case zapcore.DPanicLevel:
		return log.SeverityFatal1
	case zapcore.PanicLevel:
		return log.SeverityFatal2
	case zapcore.FatalLevel:
		return log.SeverityFatal3
	default:
		return log.SeverityUndefined
	}
}

func convertFields(fields []zapcore.Field) []log.KeyValue {
	kvs := make([]log.KeyValue, 0, len(fields)+numExtraAttr)
	for _, field := range fields {
		kvs = appendField(kvs, field)
	}
	return kvs
}

func appendField(kvs []log.KeyValue, f zapcore.Field) []log.KeyValue {
	switch f.Type {
	case zapcore.BoolType:
		return append(kvs, log.Bool(f.Key, f.Integer == 1))

	case zapcore.Int8Type, zapcore.Int16Type, zapcore.Int32Type, zapcore.Int64Type,
		zapcore.Uint32Type, zapcore.Uint8Type, zapcore.Uint16Type, zapcore.Uint64Type,
		zapcore.UintptrType:
		return append(kvs, log.Int64(f.Key, f.Integer))

	case zapcore.Float64Type:
		num := math.Float64frombits(uint64(f.Integer))
		return append(kvs, log.Float64(f.Key, num))
	case zapcore.Float32Type:
		num := math.Float32frombits(uint32(f.Integer))
		return append(kvs, log.Float64(f.Key, float64(num)))

	case zapcore.Complex64Type:
		str := strconv.FormatComplex(complex128(f.Interface.(complex64)), 'E', -1, 64)
		return append(kvs, log.String(f.Key, str))
	case zapcore.Complex128Type:
		str := strconv.FormatComplex(f.Interface.(complex128), 'E', -1, 128)
		return append(kvs, log.String(f.Key, str))

	case zapcore.StringType:
		return append(kvs, log.String(f.Key, f.String))
	case zapcore.BinaryType, zapcore.ByteStringType:
		bs := f.Interface.([]byte)
		return append(kvs, log.Bytes(f.Key, bs))
	case zapcore.StringerType:
		str := f.Interface.(fmt.Stringer).String()
		return append(kvs, log.String(f.Key, str))

	case zapcore.DurationType, zapcore.TimeType:
		return append(kvs, log.Int64(f.Key, f.Integer))
	case zapcore.TimeFullType:
		str := f.Interface.(time.Time).Format(time.RFC3339Nano)
		return append(kvs, log.String(f.Key, str))
	case zapcore.ErrorType:
		err := f.Interface.(error)
		typ := reflect.TypeOf(err).String()
		kvs = append(kvs, log.String("exception.type", typ))
		kvs = append(kvs, log.String("exception.message", err.Error()))
		return kvs
	case zapcore.ReflectType:
		str := fmt.Sprint(f.Interface)
		return append(kvs, log.String(f.Key, str))
	case zapcore.SkipType:
		return kvs

	case zapcore.ArrayMarshalerType:
		kv := log.String(f.Key+"_error", "otelzap: zapcore.ArrayMarshalerType is not implemented")
		return append(kvs, kv)
	case zapcore.ObjectMarshalerType:
		kv := log.String(f.Key+"_error", "otelzap: zapcore.ObjectMarshalerType is not implemented")
		return append(kvs, kv)

	default:
		kv := log.String(f.Key+"_error", fmt.Sprintf("otelzap: unknown field type: %v", f))
		return append(kvs, kv)
	}
}
