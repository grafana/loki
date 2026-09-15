// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package otlploghttp

import (
	"context"
	"sync/atomic"

	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp/internal/transform"
	"go.opentelemetry.io/otel/sdk/log"
)

// Exporter is an OpenTelemetry log exporter. It transports log data encoded as
// OTLP protobufs using HTTP.
// Exporter must be created with [New].
type Exporter struct {
	client  atomic.Pointer[client]
	stopped atomic.Bool
}

// This is a compile-time check that Exporter implements [log.Exporter].
var _ log.Exporter = (*Exporter)(nil)

// New returns a new [Exporter].
//
// Use the Exporter with a [log.BatchProcessor] or another processor that
// exports records asynchronously.
func New(ctx context.Context, options ...Option) (*Exporter, error) {
	cfg := newConfig(options)
	c, err := newHTTPClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return newExporter(c, cfg)
}

func newExporter(c *client, _ config) (*Exporter, error) {
	e := &Exporter{}
	e.client.Store(c)
	return e, nil
}

// Used for testing.
var transformResourceLogs = transform.ResourceLogs

// Export transforms and transmits log records to an OTLP receiver. It returns
// [log.ErrExporterShutdown] if called after Shutdown.
func (e *Exporter) Export(ctx context.Context, records []log.Record) error {
	if e.stopped.Load() {
		return log.ErrExporterShutdown
	}
	otlp := transformResourceLogs(records)
	if otlp == nil {
		return nil
	}

	c := e.client.Load()
	if e.stopped.Load() {
		return log.ErrExporterShutdown
	}
	return c.UploadLogs(ctx, otlp)
}

// Shutdown shuts down the Exporter. Calls to Export after Shutdown return
// [log.ErrExporterShutdown]. Calls to ForceFlush perform no operation.
func (e *Exporter) Shutdown(context.Context) error {
	if e.stopped.Swap(true) {
		return nil
	}

	e.client.Store(newNoopClient())
	return nil
}

// ForceFlush does nothing. The Exporter holds no state.
func (*Exporter) ForceFlush(context.Context) error {
	return nil
}
