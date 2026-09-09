// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package otlploggrpc

import (
	"context"
	"sync"
	"sync/atomic"

	logpb "go.opentelemetry.io/proto/otlp/logs/v1"

	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc/internal/transform"
	"go.opentelemetry.io/otel/sdk/log"
)

type logClient interface {
	UploadLogs(ctx context.Context, rl []*logpb.ResourceLogs) error
	Shutdown(context.Context) error
}

// Exporter is an OpenTelemetry log exporter. It transports log data encoded as
// OTLP protobufs using gRPC.
// All Exporter values must be created with [New].
type Exporter struct {
	// Ensure synchronous access to the client across all functionality.
	clientMu sync.Mutex
	client   logClient

	stopped atomic.Bool
}

// This is a compile-time check that Exporter implements [log.Exporter].
var _ log.Exporter = (*Exporter)(nil)

// New returns a new [Exporter].
//
// Use the Exporter with a [log.BatchProcessor] or another processor that
// exports records asynchronously.
func New(_ context.Context, options ...Option) (*Exporter, error) {
	cfg := newConfig(options)
	c, err := newClient(cfg)
	if err != nil {
		return nil, err
	}
	return newExporter(c), nil
}

func newExporter(c logClient) *Exporter {
	var e Exporter
	e.client = c
	return &e
}

var transformResourceLogs = transform.ResourceLogs

// Export transforms and transmits log records to an OTLP receiver.
//
// This method returns [log.ErrExporterShutdown] if called after Shutdown.
// This method returns an error if the method is canceled by the passed context.
func (e *Exporter) Export(ctx context.Context, records []log.Record) error {
	if e.stopped.Load() {
		return log.ErrExporterShutdown
	}

	otlp := transformResourceLogs(records)
	if otlp == nil {
		return nil
	}

	e.clientMu.Lock()
	defer e.clientMu.Unlock()

	if e.stopped.Load() {
		return log.ErrExporterShutdown
	}
	return e.client.UploadLogs(ctx, otlp)
}

// Shutdown shuts down the Exporter. Calls to Export after Shutdown return
// [log.ErrExporterShutdown]. Calls to ForceFlush perform no operation.
func (e *Exporter) Shutdown(ctx context.Context) error {
	if e.stopped.Swap(true) {
		return nil
	}

	e.clientMu.Lock()
	defer e.clientMu.Unlock()

	err := e.client.Shutdown(ctx)
	e.client = newNoopClient()
	return err
}

// ForceFlush does nothing. The Exporter holds no state.
func (*Exporter) ForceFlush(context.Context) error {
	return nil
}
