// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package stdoutlog // import "go.opentelemetry.io/otel/exporters/stdout/stdoutlog"

import (
	"context"
	"encoding/json"
	"sync/atomic"

	"go.opentelemetry.io/otel/exporters/stdout/stdoutlog/internal/counter"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutlog/internal/observ"

	"go.opentelemetry.io/otel/sdk/log"
)

var _ log.Exporter = &Exporter{}

// Exporter writes JSON-encoded log records to an [io.Writer] ([os.Stdout] by default).
// Exporter must be created with [New].
type Exporter struct {
	encoder    atomic.Pointer[json.Encoder]
	timestamps bool
	inst       *observ.Instrumentation
}

// New creates an [Exporter].
func New(options ...Option) (*Exporter, error) {
	cfg := newConfig(options)

	enc := json.NewEncoder(cfg.Writer)
	if cfg.PrettyPrint {
		enc.SetIndent("", "\t")
	}

	e := &Exporter{
		timestamps: cfg.Timestamps,
	}
	e.encoder.Store(enc)

	var err error
	e.inst, err = observ.NewInstrumentation(counter.NextExporterID())
	return e, err
}

// Export exports log records to writer.
func (e *Exporter) Export(ctx context.Context, records []log.Record) (err error) {
	enc := e.encoder.Load()
	if enc == nil {
		return nil
	}

	var success int64
	if e.inst != nil {
		op := e.inst.ExportLogs(ctx, int64(len(records)))
		defer func() {
			op.End(success, err)
		}()
	}

	for _, record := range records {
		// Honor context cancellation.
		if err := ctx.Err(); err != nil {
			return err
		}

		// Encode record, one by one.
		recordJSON := e.newRecordJSON(record)
		if err := enc.Encode(recordJSON); err != nil {
			return err
		}
		success++
	}
	return nil
}

// Shutdown shuts down the Exporter.
// Calls to Export will perform no operation after this is called.
func (e *Exporter) Shutdown(context.Context) error {
	e.encoder.Store(nil)
	return nil
}

// ForceFlush performs no action.
func (*Exporter) ForceFlush(context.Context) error {
	return nil
}
