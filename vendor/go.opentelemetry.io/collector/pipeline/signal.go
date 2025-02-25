// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package pipeline // import "go.opentelemetry.io/collector/pipeline"

import (
	"errors"

	"go.opentelemetry.io/collector/pipeline/internal/globalsignal"
)

// Signal represents the signals supported by the collector. We currently support
// collecting metrics, traces and logs, this can expand in the future.
type Signal = globalsignal.Signal

var ErrSignalNotSupported = errors.New("telemetry type is not supported")

var (
	SignalTraces  = globalsignal.MustNewSignal("traces")
	SignalMetrics = globalsignal.MustNewSignal("metrics")
	SignalLogs    = globalsignal.MustNewSignal("logs")
)
