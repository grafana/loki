// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package stdouttrace // import "go.opentelemetry.io/otel/exporters/stdout/stdouttrace"

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace/internal/counter"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace/internal/x"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/sdk"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/semconv/v1.37.0/otelconv"
)

// otelComponentType is a name identifying the type of the OpenTelemetry
// component. It is not a standardized OTel component type, so it uses the
// Go package prefixed type name to ensure uniqueness and identity.
const otelComponentType = "go.opentelemetry.io/otel/exporters/stdout/stdouttrace.Exporter"

var zeroTime time.Time

var _ trace.SpanExporter = &Exporter{}

// New creates an Exporter with the passed options.
func New(options ...Option) (*Exporter, error) {
	cfg := newConfig(options...)

	enc := json.NewEncoder(cfg.Writer)
	if cfg.PrettyPrint {
		enc.SetIndent("", "\t")
	}

	exporter := &Exporter{
		encoder:    enc,
		timestamps: cfg.Timestamps,
	}

	if !x.SelfObservability.Enabled() {
		return exporter, nil
	}

	exporter.selfObservabilityEnabled = true
	exporter.selfObservabilityAttrs = []attribute.KeyValue{
		semconv.OTelComponentName(fmt.Sprintf("%s/%d", otelComponentType, counter.NextExporterID())),
		semconv.OTelComponentTypeKey.String(otelComponentType),
	}
	s := attribute.NewSet(exporter.selfObservabilityAttrs...)
	exporter.selfObservabilitySetOpt = metric.WithAttributeSet(s)

	mp := otel.GetMeterProvider()
	m := mp.Meter(
		"go.opentelemetry.io/otel/exporters/stdout/stdouttrace",
		metric.WithInstrumentationVersion(sdk.Version()),
		metric.WithSchemaURL(semconv.SchemaURL),
	)

	var err, e error
	if exporter.spanInflightMetric, e = otelconv.NewSDKExporterSpanInflight(m); e != nil {
		e = fmt.Errorf("failed to create span inflight metric: %w", e)
		err = errors.Join(err, e)
	}
	if exporter.spanExportedMetric, e = otelconv.NewSDKExporterSpanExported(m); e != nil {
		e = fmt.Errorf("failed to create span exported metric: %w", e)
		err = errors.Join(err, e)
	}
	if exporter.operationDurationMetric, e = otelconv.NewSDKExporterOperationDuration(m); e != nil {
		e = fmt.Errorf("failed to create operation duration metric: %w", e)
		err = errors.Join(err, e)
	}

	return exporter, err
}

// Exporter is an implementation of trace.SpanSyncer that writes spans to stdout.
type Exporter struct {
	encoder    *json.Encoder
	encoderMu  sync.Mutex
	timestamps bool

	stoppedMu sync.RWMutex
	stopped   bool

	selfObservabilityEnabled bool
	selfObservabilityAttrs   []attribute.KeyValue // selfObservability common attributes
	selfObservabilitySetOpt  metric.MeasurementOption
	spanInflightMetric       otelconv.SDKExporterSpanInflight
	spanExportedMetric       otelconv.SDKExporterSpanExported
	operationDurationMetric  otelconv.SDKExporterOperationDuration
}

var (
	measureAttrsPool = sync.Pool{
		New: func() any {
			// "component.name" + "component.type" + "error.type"
			const n = 1 + 1 + 1
			s := make([]attribute.KeyValue, 0, n)
			// Return a pointer to a slice instead of a slice itself
			// to avoid allocations on every call.
			return &s
		},
	}

	addOptPool = &sync.Pool{
		New: func() any {
			const n = 1 // WithAttributeSet
			o := make([]metric.AddOption, 0, n)
			return &o
		},
	}

	recordOptPool = &sync.Pool{
		New: func() any {
			const n = 1 // WithAttributeSet
			o := make([]metric.RecordOption, 0, n)
			return &o
		},
	}
)

// ExportSpans writes spans in json format to stdout.
func (e *Exporter) ExportSpans(ctx context.Context, spans []trace.ReadOnlySpan) (err error) {
	var success int64
	if e.selfObservabilityEnabled {
		count := int64(len(spans))

		addOpt := addOptPool.Get().(*[]metric.AddOption)
		defer func() {
			*addOpt = (*addOpt)[:0]
			addOptPool.Put(addOpt)
		}()

		*addOpt = append(*addOpt, e.selfObservabilitySetOpt)

		e.spanInflightMetric.Inst().Add(ctx, count, *addOpt...)
		defer func(starting time.Time) {
			e.spanInflightMetric.Inst().Add(ctx, -count, *addOpt...)

			// Record the success and duration of the operation.
			//
			// Do not exclude 0 values, as they are valid and indicate no spans
			// were exported which is meaningful for certain aggregations.
			e.spanExportedMetric.Inst().Add(ctx, success, *addOpt...)

			mOpt := e.selfObservabilitySetOpt
			if err != nil {
				// additional attributes for self-observability,
				// only spanExportedMetric and operationDurationMetric are supported.
				attrs := measureAttrsPool.Get().(*[]attribute.KeyValue)
				defer func() {
					*attrs = (*attrs)[:0] // reset the slice for reuse
					measureAttrsPool.Put(attrs)
				}()
				*attrs = append(*attrs, e.selfObservabilityAttrs...)
				*attrs = append(*attrs, semconv.ErrorType(err))

				// Do not inefficiently make a copy of attrs by using
				// WithAttributes instead of WithAttributeSet.
				set := attribute.NewSet(*attrs...)
				mOpt = metric.WithAttributeSet(set)

				// Reset addOpt with new attribute set.
				*addOpt = append((*addOpt)[:0], mOpt)

				e.spanExportedMetric.Inst().Add(
					ctx,
					count-success,
					*addOpt...,
				)
			}

			recordOpt := recordOptPool.Get().(*[]metric.RecordOption)
			defer func() {
				*recordOpt = (*recordOpt)[:0]
				recordOptPool.Put(recordOpt)
			}()

			*recordOpt = append(*recordOpt, mOpt)
			e.operationDurationMetric.Inst().Record(
				ctx,
				time.Since(starting).Seconds(),
				*recordOpt...,
			)
		}(time.Now())
	}

	if err := ctx.Err(); err != nil {
		return err
	}
	e.stoppedMu.RLock()
	stopped := e.stopped
	e.stoppedMu.RUnlock()
	if stopped {
		return nil
	}

	if len(spans) == 0 {
		return nil
	}

	stubs := tracetest.SpanStubsFromReadOnlySpans(spans)

	e.encoderMu.Lock()
	defer e.encoderMu.Unlock()
	for i := range stubs {
		stub := &stubs[i]
		// Remove timestamps
		if !e.timestamps {
			stub.StartTime = zeroTime
			stub.EndTime = zeroTime
			for j := range stub.Events {
				ev := &stub.Events[j]
				ev.Time = zeroTime
			}
		}

		// Encode span stubs, one by one
		if e := e.encoder.Encode(stub); e != nil {
			err = errors.Join(err, fmt.Errorf("failed to encode span %d: %w", i, e))
			continue
		}
		success++
	}
	return err
}

// Shutdown is called to stop the exporter, it performs no action.
func (e *Exporter) Shutdown(context.Context) error {
	e.stoppedMu.Lock()
	e.stopped = true
	e.stoppedMu.Unlock()

	return nil
}

// MarshalLog is the marshaling function used by the logging system to represent this Exporter.
func (e *Exporter) MarshalLog() any {
	return struct {
		Type           string
		WithTimestamps bool
	}{
		Type:           "stdout",
		WithTimestamps: e.timestamps,
	}
}
