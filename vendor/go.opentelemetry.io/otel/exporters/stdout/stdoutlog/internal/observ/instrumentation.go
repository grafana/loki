// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package observ provides observability metrics for OTLP log exporters.
// This is an experimental feature controlled by the x.Observability feature flag.
package observ // import "go.opentelemetry.io/otel/exporters/stdout/stdoutlog/internal/observ"

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"go.opentelemetry.io/otel/exporters/stdout/stdoutlog/internal/x"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutlog/internal"
	"go.opentelemetry.io/otel/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
	"go.opentelemetry.io/otel/semconv/v1.41.0/otelconv"
)

const (
	// ScopeName is the unique name of the meter used for instrumentation.
	ScopeName = "go.opentelemetry.io/otel/exporters/stdout/stdoutlog/internal/observ"

	// ComponentType uniquely identifies the OpenTelemetry Exporter component
	// being instrumented.
	//
	// The STDOUT log exporter is not a standardized OTel component type, so
	// it uses the Go package prefixed type name to ensure uniqueness and
	// identity.
	ComponentType = "go.opentelemetry.io/otel/exporters/stdout/stdoutlog.Exporter"

	// Version is the current version of this instrumentation.
	//
	// This matches the version of the exporter.
	Version = internal.Version
)

var (
	addOptPool = &sync.Pool{
		New: func() any {
			const n = 1
			s := make([]metric.AddOption, 0, n)
			return &s
		},
	}
	attrsPool = &sync.Pool{
		New: func() any {
			const n = 1 + // component.name
				1 + // component.type
				1 // error.type
			s := make([]attribute.KeyValue, 0, n)
			return &s
		},
	}
	recordOptPool = &sync.Pool{
		New: func() any {
			const n = 1
			s := make([]metric.RecordOption, 0, n)
			return &s
		},
	}
)

func get[T any](pool *sync.Pool) *[]T {
	return pool.Get().(*[]T)
}

func put[T any](pool *sync.Pool, value *[]T) {
	clear(*value) // erase elements to allow GC to collect what they refer to.
	*value = (*value)[:0]
	pool.Put(value)
}

// Instrumentation is experimental instrumentation for the exporter.
type Instrumentation struct {
	inflight metric.Int64UpDownCounter
	exported metric.Int64Counter
	duration metric.Float64Histogram

	attrs  []attribute.KeyValue
	setOpt metric.MeasurementOption
}

// GetComponentName returns the constant name for the exporter with the
// provided id.
func GetComponentName(id int64) string {
	return fmt.Sprintf("%s/%d", ComponentType, id)
}

func getAttrs(id int64) []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, 2)
	attrs = append(attrs,
		semconv.OTelComponentName(GetComponentName(id)),
		semconv.OTelComponentTypeKey.String(ComponentType))

	return attrs
}

// NewInstrumentation returns instrumentation for stdlog exporter.
func NewInstrumentation(id int64) (*Instrumentation, error) {
	if !x.Observability.Enabled() {
		return nil, nil
	}

	inst := &Instrumentation{}

	mp := otel.GetMeterProvider()
	m := mp.Meter(
		ScopeName,
		metric.WithInstrumentationVersion(Version),
		metric.WithSchemaURL(semconv.SchemaURL),
	)

	var err error

	inflight, e := otelconv.NewSDKExporterLogInflight(m)
	if e != nil {
		e = fmt.Errorf("failed to create the inflight metric: %w", e)
		err = errors.Join(err, e)
	}
	inst.inflight = inflight.Inst()

	exported, e := otelconv.NewSDKExporterLogExported(m)
	if e != nil {
		e = fmt.Errorf("failed to create the exported metric: %w", e)
		err = errors.Join(err, e)
	}
	inst.exported = exported.Inst()

	duration, e := otelconv.NewSDKExporterOperationDuration(m)
	if e != nil {
		e = fmt.Errorf("failed to create the duration metric: %w", e)
		err = errors.Join(err, e)
	}
	inst.duration = duration.Inst()

	if err != nil {
		return nil, err
	}
	inst.attrs = getAttrs(id)
	inst.setOpt = metric.WithAttributeSet(attribute.NewSet(inst.attrs...))
	return inst, nil
}

// ExportLogs instruments the ExportLogs method of the exporter. It returns
// an [ExportOp] that must have its [ExportOp.End] method called when the
// ExportLogs method returns.
func (i *Instrumentation) ExportLogs(ctx context.Context, count int64) ExportOp {
	start := time.Now()

	if i.inflight.Enabled(ctx) {
		addOpt := get[metric.AddOption](addOptPool)
		defer put(addOptPool, addOpt)
		*addOpt = append(*addOpt, i.setOpt)
		i.inflight.Add(ctx, count, *addOpt...)
	}

	return ExportOp{
		count: count,
		ctx:   ctx,
		inst:  i,
		start: start,
	}
}

// ExportOp tracks the operation being observed by [Instrumentation.ExportLogs].
type ExportOp struct {
	count int64
	ctx   context.Context
	inst  *Instrumentation
	start time.Time
}

// End completes the observation of the operation being observed by a call to
// [Instrumentation.ExportLogs].
// The success parameter is the number of logs exported successfully.
// Any error encountered during export is provided as err.
//
// If err is not nil, End records failed log exports as count-success with the
// error.type attribute set from err.
func (e ExportOp) End(success int64, err error) {
	inflightLogsEnable := e.inst.inflight.Enabled(e.ctx)
	exportedLogsEnable := e.inst.exported.Enabled(e.ctx)
	opDurationEnable := e.inst.duration.Enabled(e.ctx)

	if !inflightLogsEnable && !exportedLogsEnable && !opDurationEnable {
		return
	}

	addOpt := get[metric.AddOption](addOptPool)
	defer put(addOptPool, addOpt)
	*addOpt = append(*addOpt, e.inst.setOpt)

	if inflightLogsEnable {
		e.inst.inflight.Add(e.ctx, -e.count, *addOpt...)
	}

	if exportedLogsEnable {
		e.inst.exported.Add(e.ctx, success, *addOpt...)
	}

	mOpt := e.inst.setOpt
	if err != nil && (exportedLogsEnable || opDurationEnable) {
		// Add the error.type attribute to the attribute set.
		attrs := get[attribute.KeyValue](attrsPool)
		defer put(attrsPool, attrs)
		*attrs = append(*attrs, e.inst.attrs...)
		*attrs = append(*attrs, semconv.ErrorType(err))

		mOpt = metric.WithAttributeSet(attribute.NewSet(*attrs...))

		if exportedLogsEnable {
			*addOpt = append((*addOpt)[:0], mOpt)
			e.inst.exported.Add(e.ctx, e.count-success, *addOpt...)
		}
	}

	if opDurationEnable {
		recordOpt := get[metric.RecordOption](recordOptPool)
		defer put(recordOptPool, recordOpt)

		*recordOpt = append(*recordOpt, mOpt)
		e.inst.duration.Record(e.ctx, time.Since(e.start).Seconds(), *recordOpt...)
	}
}
