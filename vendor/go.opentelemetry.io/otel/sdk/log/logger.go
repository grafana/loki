// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package log // import "go.opentelemetry.io/otel/sdk/log"

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/log/embedded"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/sdk"
	"go.opentelemetry.io/otel/sdk/instrumentation"
	"go.opentelemetry.io/otel/sdk/log/internal/x"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/semconv/v1.37.0/otelconv"
	"go.opentelemetry.io/otel/trace"
)

var now = time.Now

// Compile-time check logger implements log.Logger.
var _ log.Logger = (*logger)(nil)

type logger struct {
	embedded.Logger

	provider             *LoggerProvider
	instrumentationScope instrumentation.Scope

	selfObservabilityEnabled bool
	logCreatedMetric         otelconv.SDKLogCreated
}

func newLogger(p *LoggerProvider, scope instrumentation.Scope) *logger {
	l := &logger{
		provider:             p,
		instrumentationScope: scope,
	}
	if !x.SelfObservability.Enabled() {
		return l
	}
	l.selfObservabilityEnabled = true
	mp := otel.GetMeterProvider()
	m := mp.Meter("go.opentelemetry.io/otel/sdk/log",
		metric.WithInstrumentationVersion(sdk.Version()),
		metric.WithSchemaURL(semconv.SchemaURL))

	var err error
	if l.logCreatedMetric, err = otelconv.NewSDKLogCreated(m); err != nil {
		err = fmt.Errorf("failed to create log created metric: %w", err)
		otel.Handle(err)
	}
	return l
}

func (l *logger) Emit(ctx context.Context, r log.Record) {
	newRecord := l.newRecord(ctx, r)
	for _, p := range l.provider.processors {
		if err := p.OnEmit(ctx, &newRecord); err != nil {
			otel.Handle(err)
		}
	}
}

// Enabled returns true if at least one Processor held by the LoggerProvider
// that created the logger will process param for the provided context and param.
//
// If it is not possible to definitively determine the param will be
// processed, true will be returned by default. A value of false will only be
// returned if it can be positively verified that no Processor will process.
func (l *logger) Enabled(ctx context.Context, param log.EnabledParameters) bool {
	p := EnabledParameters{
		InstrumentationScope: l.instrumentationScope,
		Severity:             param.Severity,
		EventName:            param.EventName,
	}

	// If there are more Processors than FilterProcessors,
	// which means not all Processors are FilterProcessors,
	// we cannot be sure that all Processors will drop the record.
	// Therefore, return true.
	//
	// If all Processors are FilterProcessors, check if any is enabled.
	return len(l.provider.processors) > len(l.provider.fltrProcessors) || anyEnabled(ctx, p, l.provider.fltrProcessors)
}

func anyEnabled(ctx context.Context, param EnabledParameters, fltrs []FilterProcessor) bool {
	for _, f := range fltrs {
		if f.Enabled(ctx, param) {
			// At least one Processor will process the Record.
			return true
		}
	}
	// No Processor will process the record
	return false
}

func (l *logger) newRecord(ctx context.Context, r log.Record) Record {
	sc := trace.SpanContextFromContext(ctx)

	newRecord := Record{
		eventName:         r.EventName(),
		timestamp:         r.Timestamp(),
		observedTimestamp: r.ObservedTimestamp(),
		severity:          r.Severity(),
		severityText:      r.SeverityText(),

		traceID:    sc.TraceID(),
		spanID:     sc.SpanID(),
		traceFlags: sc.TraceFlags(),

		resource:                  l.provider.resource,
		scope:                     &l.instrumentationScope,
		attributeValueLengthLimit: l.provider.attributeValueLengthLimit,
		attributeCountLimit:       l.provider.attributeCountLimit,
		allowDupKeys:              l.provider.allowDupKeys,
	}
	if l.selfObservabilityEnabled {
		l.logCreatedMetric.Add(ctx, 1)
	}

	// This ensures we deduplicate key-value collections in the log body
	newRecord.SetBody(r.Body())

	// This field SHOULD be set once the event is observed by OpenTelemetry.
	if newRecord.observedTimestamp.IsZero() {
		newRecord.observedTimestamp = now()
	}

	r.WalkAttributes(func(kv log.KeyValue) bool {
		newRecord.AddAttributes(kv)
		return true
	})

	return newRecord
}
