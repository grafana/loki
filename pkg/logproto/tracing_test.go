// SPDX-License-Identifier: AGPL-3.0-only

package mimir

import (
	crand "crypto/rand"
	"encoding/binary"
	"errors"
	"math/rand"
	"testing"

	"github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/uber/jaeger-client-go"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

func TestJaegerToOpenTelemetryTraceID(t *testing.T) {
	expected, _ := generateOpenTelemetryIDs()

	jaegerTraceID, err := jaeger.TraceIDFromString(expected.String())
	require.NoError(t, err)

	actual := jaegerToOpenTelemetryTraceID(jaegerTraceID)
	assert.Equal(t, expected, actual)
}

func TestJaegerToOpenTelemetrySpanID(t *testing.T) {
	_, expected := generateOpenTelemetryIDs()

	jaegerSpanID, err := jaeger.SpanIDFromString(expected.String())
	require.NoError(t, err)

	actual := jaegerToOpenTelemetrySpanID(jaegerSpanID)
	assert.Equal(t, expected, actual)
}

// generateOpenTelemetryIDs generated trace and span IDs. The implementation has been copied from open telemetry.
func generateOpenTelemetryIDs() (trace.TraceID, trace.SpanID) {
	var rngSeed int64
	_ = binary.Read(crand.Reader, binary.LittleEndian, &rngSeed)
	randSource := rand.New(rand.NewSource(rngSeed))

	tid := trace.TraceID{}
	randSource.Read(tid[:])
	sid := trace.SpanID{}
	randSource.Read(sid[:])
	return tid, sid
}

func TestOpenTelemetrySpanBridge_RecordError(t *testing.T) {
	err := errors.New("my application error")

	tests := map[string]struct {
		attrs    []attribute.KeyValue
		expected []log.Field
	}{
		"no attributes": {
			expected: []log.Field{
				log.Error(err),
			},
		},
		"one attribute": {
			attrs: []attribute.KeyValue{
				attribute.String("first", "value"),
			},
			expected: []log.Field{
				log.Error(err),
				log.Object("first", "value"),
			},
		},
		"multiple attributes": {
			attrs: []attribute.KeyValue{
				attribute.String("first", "value"),
				attribute.Int("second", 123),
			},
			expected: []log.Field{
				log.Error(err),
				log.Object("first", "value"),
				log.Object("second", int64(123)),
			},
		},
	}

	for testName, testData := range tests {
		t.Run(testName, func(t *testing.T) {
			m := &TracingSpanMock{}
			m.On("LogFields", mock.Anything).Return()

			s := NewOpenTelemetrySpanBridge(m, nil)
			s.recordError(err, testData.attrs)
			m.AssertCalled(t, "LogFields", testData.expected)
		})
	}
}

func TestOpenTelemetrySpanBridge_AddEvent(t *testing.T) {
	const eventName = "my application did something"

	tests := map[string]struct {
		attrs    []attribute.KeyValue
		expected []log.Field
	}{
		"no attributes": {
			expected: []log.Field{
				log.Event(eventName),
			},
		},
		"one attribute": {
			attrs: []attribute.KeyValue{
				attribute.String("first", "value"),
			},
			expected: []log.Field{
				log.Event(eventName),
				log.Object("first", "value"),
			},
		},
		"multiple attributes": {
			attrs: []attribute.KeyValue{
				attribute.String("first", "value"),
				attribute.Int("second", 123),
			},
			expected: []log.Field{
				log.Event(eventName),
				log.Object("first", "value"),
				log.Object("second", int64(123)),
			},
		},
	}

	for testName, testData := range tests {
		t.Run(testName, func(t *testing.T) {
			m := &TracingSpanMock{}
			m.On("LogFields", mock.Anything).Return()

			s := NewOpenTelemetrySpanBridge(m, nil)
			s.addEvent(eventName, testData.attrs)
			m.AssertCalled(t, "LogFields", testData.expected)
		})
	}
}

type TracingSpanMock struct {
	mock.Mock
}

func (m *TracingSpanMock) Finish() {
	m.Called()
}

func (m *TracingSpanMock) FinishWithOptions(opts opentracing.FinishOptions) {
	m.Called(opts)
}

func (m *TracingSpanMock) Context() opentracing.SpanContext {
	args := m.Called()
	return args.Get(0).(opentracing.SpanContext)
}

func (m *TracingSpanMock) SetOperationName(operationName string) opentracing.Span {
	args := m.Called(operationName)
	return args.Get(0).(opentracing.Span)
}

func (m *TracingSpanMock) SetTag(key string, value interface{}) opentracing.Span {
	args := m.Called(key, value)
	return args.Get(0).(opentracing.Span)
}

func (m *TracingSpanMock) LogFields(fields ...log.Field) {
	m.Called(fields)
}

func (m *TracingSpanMock) LogKV(alternatingKeyValues ...interface{}) {
	m.Called(alternatingKeyValues)
}

func (m *TracingSpanMock) SetBaggageItem(restrictedKey, value string) opentracing.Span {
	args := m.Called(restrictedKey, value)
	return args.Get(0).(opentracing.Span)
}

func (m *TracingSpanMock) BaggageItem(restrictedKey string) string {
	args := m.Called(restrictedKey)
	return args.String(0)
}

func (m *TracingSpanMock) Tracer() opentracing.Tracer {
	args := m.Called()
	return args.Get(0).(opentracing.Tracer)
}

func (m *TracingSpanMock) LogEvent(event string) {
	m.Called(event)
}

func (m *TracingSpanMock) LogEventWithPayload(event string, payload interface{}) {
	m.Called(event, payload)
}

func (m *TracingSpanMock) Log(data opentracing.LogData) {
	m.Called(data)
}
