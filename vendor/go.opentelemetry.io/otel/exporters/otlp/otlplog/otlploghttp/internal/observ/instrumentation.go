// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package observ // import "go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp/internal/observ"

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp/internal"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp/internal/x"
	"go.opentelemetry.io/otel/internal/global"
	"go.opentelemetry.io/otel/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/semconv/v1.37.0/otelconv"
)

const (
	// ScopeName is the unique name of the meter used for instrumentation.
	ScopeName = "go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp/internal/observ"

	// Version is the current version of this instrumentation
	//
	// This matches the version of the exporter.
	Version = internal.Version
)

var (
	attrsPool = &sync.Pool{
		New: func() any {
			const n = 1 + // component.name
				1 + // component.type
				1 + // server.addr
				1 + // server.port
				1 + // error.port
				1 // http.response.status.code
			s := make([]attribute.KeyValue, 0, n)
			return &s
		},
	}
	addOptPool = &sync.Pool{
		New: func() any {
			const n = 1 // WithAttributeSet
			s := make([]metric.AddOption, 0, n)
			return &s
		},
	}
	recordPool = &sync.Pool{
		New: func() any {
			const n = 1 // WithAttributeSet
			s := make([]metric.RecordOption, 0, n)
			return &s
		},
	}
)

func get[T any](pool *sync.Pool) *[]T {
	return pool.Get().(*[]T)
}

func put[T any](pool *sync.Pool, value *[]T) {
	*value = (*value)[:0]
	pool.Put(value)
}

// GetComponentName returns the constant name for the exporter with the
// provided id.
func GetComponentName(id int64) string {
	return fmt.Sprintf("%s/%d", otelconv.ComponentTypeOtlpHTTPLogExporter, id)
}

// Instrumentation is experimental instrumentation for the exporter.
type Instrumentation struct {
	inflightMetric    metric.Int64UpDownCounter
	exportedMetric    metric.Int64Counter
	operationDuration metric.Float64Histogram

	presetAttrs []attribute.KeyValue
	addOpt      metric.AddOption
	recordOpt   metric.RecordOption
}

// NewInstrumentation returns instrumentation for otlplog http exporter.
func NewInstrumentation(id int64, target string) (*Instrumentation, error) {
	if !x.Observability.Enabled() {
		return nil, nil
	}

	inst := &Instrumentation{}

	provider := otel.GetMeterProvider()
	m := provider.Meter(
		ScopeName,
		metric.WithSchemaURL(semconv.SchemaURL),
		metric.WithInstrumentationVersion(Version),
	)

	var e, err error
	logInflight, e := otelconv.NewSDKExporterLogInflight(m)
	if e != nil {
		e = fmt.Errorf("failed to create the inflight metric %w", e)
		err = errors.Join(err, e)
	}
	inst.inflightMetric = logInflight.Inst()

	exported, e := otelconv.NewSDKExporterLogExported(m)
	if e != nil {
		e = fmt.Errorf("failed to create the exported metric %w", e)
		err = errors.Join(err, e)
	}
	inst.exportedMetric = exported.Inst()

	operation, e := otelconv.NewSDKExporterOperationDuration(m)
	if e != nil {
		e = fmt.Errorf("failed to create the operation duration metric %w", e)
		err = errors.Join(err, e)
	}
	inst.operationDuration = operation.Inst()

	if err != nil {
		return nil, err
	}

	inst.presetAttrs = setPresetAttrs(GetComponentName(id), target)

	inst.addOpt = metric.WithAttributeSet(attribute.NewSet(inst.presetAttrs...))
	inst.recordOpt = metric.WithAttributeSet(attribute.NewSet(append(
		[]attribute.KeyValue{semconv.HTTPResponseStatusCode(http.StatusOK)},
		inst.presetAttrs...,
	)...))

	return inst, nil
}

func setPresetAttrs(name, target string) []attribute.KeyValue {
	addrAttrs := ServerAddrAttrs(target)

	attrs := make([]attribute.KeyValue, 0, 2+len(addrAttrs))
	attrs = append(
		attrs,
		semconv.OTelComponentName(name),
		semconv.OTelComponentTypeOtlpHTTPLogExporter,
	)
	attrs = append(attrs, addrAttrs...)
	return attrs
}

// ServerAddrAttrs is a function that extracts server address and port attributes
// from a target string.
func ServerAddrAttrs(target string) []attribute.KeyValue {
	host, port, err := parseTarget(target)
	if err != nil || (host == "" && port < 0) {
		if err != nil {
			global.Debug("failed to parse target", "target", target, "error", err)
		}
		return nil
	}

	if port < 0 {
		return []attribute.KeyValue{semconv.ServerAddress(host)}
	}

	if host == "" {
		return []attribute.KeyValue{
			semconv.ServerPort(port),
		}
	}
	return []attribute.KeyValue{
		semconv.ServerAddress(host),
		semconv.ServerPort(port),
	}
}

func (i *Instrumentation) ExportLogs(ctx context.Context, count int64) ExportOp {
	start := time.Now()

	addOpt := get[metric.AddOption](addOptPool)
	defer put(addOptPool, addOpt)
	*addOpt = append(*addOpt, i.addOpt)
	i.inflightMetric.Add(ctx, count, *addOpt...)

	return ExportOp{
		ctx:   ctx,
		start: start,
		inst:  i,
		count: count,
	}
}

// ExportOp tracks the operationDuration being observed by [Instrumentation.ExportLogs].
type ExportOp struct {
	ctx   context.Context
	start time.Time
	inst  *Instrumentation
	count int64
}

// End completes the observation of the operationDuration being observed by a call to
// [Instrumentation.ExportLogs].
// Any error that is encountered is provided as err.
//
// If err is not nil, all logs will be recorded as failures unless error is of
// type [internal.PartialSuccess]. In the case of a PartialSuccess, the number
// of successfully exported logs will be determined by inspecting the
// RejectedItems field of the PartialSuccess.
func (e ExportOp) End(err error, code int) {
	addOpt := get[metric.AddOption](addOptPool)
	defer put(addOptPool, addOpt)
	*addOpt = append(*addOpt, e.inst.addOpt)

	e.inst.inflightMetric.Add(e.ctx, -e.count, *addOpt...)
	success := successful(e.count, err)
	e.inst.exportedMetric.Add(e.ctx, success, *addOpt...)

	if err != nil {
		attrs := get[attribute.KeyValue](attrsPool)
		defer put(attrsPool, attrs)

		*attrs = append(*attrs, e.inst.presetAttrs...)
		*attrs = append(*attrs, semconv.ErrorType(err))

		a := metric.WithAttributeSet(attribute.NewSet(*attrs...))
		e.inst.exportedMetric.Add(e.ctx, e.count-success, a)
	}

	record := get[metric.RecordOption](recordPool)
	defer put(recordPool, record)
	*record = append(*record, e.recordOption(err, code))

	duration := time.Since(e.start).Seconds()
	e.inst.operationDuration.Record(e.ctx, duration, *record...)
}

func (e ExportOp) recordOption(err error, code int) metric.RecordOption {
	if err == nil {
		return e.inst.recordOpt
	}

	attrs := get[attribute.KeyValue](attrsPool)
	defer put(attrsPool, attrs)

	*attrs = append(*attrs, e.inst.presetAttrs...)
	*attrs = append(
		*attrs,
		semconv.HTTPResponseStatusCode(code),
		semconv.ErrorType(err),
	)
	return metric.WithAttributeSet(attribute.NewSet(*attrs...))
}

// successful returns the number of successfully exported logs out of the n
// that were exported based on the provided error.
//
// If err is nil, n is returned. All logs were successfully exported.
//
// If err is not nil and not an [internal.PartialSuccess] error, 0 is returned.
// It is assumed all logs failed to be exported.
//
// If err is an [internal.PartialSuccess] error, the number of successfully
// exported logs is computed by subtracting the RejectedItems field from n. If
// RejectedItems is negative, n is returned. If RejectedItems is greater than
// n, 0 is returned.
func successful(count int64, err error) int64 {
	if err == nil {
		return count
	}
	return count - rejected(count, err)
}

var errPool = sync.Pool{
	New: func() any {
		return new(internal.PartialSuccess)
	},
}

// rejected returns how many out of the n logs exporter were rejected based on
// the provided non-nil err.
func rejected(n int64, err error) int64 {
	ps := errPool.Get().(*internal.PartialSuccess)
	defer errPool.Put(ps)

	if errors.As(err, ps) {
		// Bound RejectedItems to [0, n]. This should not be needed,
		// but be defensive as this is from an external source.
		return min(max(ps.RejectedItems, 0), n)
	}
	// all logs exported
	return n
}

// parseEndpoint parses the host and port from target that has the form
// "host[:port]", or it returns an error if the target is not parsable.
//
// If no port is specified, -1 is returned.
//
// If no host is specified, an empty string is returned.
func parseTarget(endpoint string) (string, int, error) {
	if ip := parseIP(endpoint); ip != "" {
		return ip, -1, nil
	}

	// If there's no colon, there is no port (IPv6 with no port checked above).
	if !strings.Contains(endpoint, ":") {
		return endpoint, -1, nil
	}

	// Otherwise, parse as host:port.
	host, portStr, err := net.SplitHostPort(endpoint)
	if err != nil {
		return "", -1, fmt.Errorf("invalid host:port %q: %w", endpoint, err)
	}

	const base, bitSize = 10, 16
	port16, err := strconv.ParseUint(portStr, base, bitSize)
	if err != nil {
		return "", -1, fmt.Errorf("invalid port %q: %w", portStr, err)
	}
	port := int(port16)

	return host, port, nil
}

// parseIP attempts to parse the entire target as an IP address.
// It returns the normalized string form of the IP if successful,
// or an empty string if parsing fails.
func parseIP(ip string) string {
	// Strip leading and trailing brackets for IPv6 addresses.
	if len(ip) >= 2 && ip[0] == '[' && ip[len(ip)-1] == ']' {
		ip = ip[1 : len(ip)-1]
	}
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return ""
	}
	// Return the normalized string form of the IP.
	return addr.String()
}
