// Copyright The OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//       http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package pdata // import "go.opentelemetry.io/collector/model/pdata"

// This file contains aliases for log data structures.

import "go.opentelemetry.io/collector/pdata/plog"

// LogsMarshaler is an alias for plog.Marshaler interface.
// Deprecated: [v0.49.0] Use plog.Marshaler instead.
type LogsMarshaler = plog.Marshaler

// LogsUnmarshaler is an alias for plog.Unmarshaler interface.
// Deprecated: [v0.49.0] Use plog.Unmarshaler instead.
type LogsUnmarshaler = plog.Unmarshaler

// LogsSizer is an alias for plog.Sizer interface.
// Deprecated: [v0.49.0] Use plog.Sizer instead.
type LogsSizer = plog.Sizer

// Logs is an alias for plog.Logs struct.
// Deprecated: [v0.49.0] Use plog.Logs instead.
type Logs = plog.Logs

// NewLogs is an alias for a function to create new Logs.
// Deprecated: [v0.49.0] Use plog.NewLogs instead.
var NewLogs = plog.NewLogs

// SeverityNumber is an alias for plog.SeverityNumber type.
// Deprecated: [v0.49.0] Use plog.SeverityNumber instead.
type SeverityNumber = plog.SeverityNumber

const (

	// Deprecated: [v0.49.0] Use plog.SeverityNumberUNDEFINED instead.
	SeverityNumberUNDEFINED = plog.SeverityNumberUNDEFINED

	// Deprecated: [v0.49.0] Use plog.SeverityNumberTRACE instead.
	SeverityNumberTRACE = plog.SeverityNumberTRACE

	// Deprecated: [v0.49.0] Use plog.SeverityNumberTRACE2 instead.
	SeverityNumberTRACE2 = plog.SeverityNumberTRACE2

	// Deprecated: [v0.49.0] Use plog.SeverityNumberTRACE3 instead.
	SeverityNumberTRACE3 = plog.SeverityNumberTRACE3

	// Deprecated: [v0.49.0] Use plog.SeverityNumberTRACE4 instead.
	SeverityNumberTRACE4 = plog.SeverityNumberTRACE4

	// Deprecated: [v0.49.0] Use plog.SeverityNumberDEBUG instead.
	SeverityNumberDEBUG = plog.SeverityNumberDEBUG

	// Deprecated: [v0.49.0] Use plog.SeverityNumberDEBUG2 instead.
	SeverityNumberDEBUG2 = plog.SeverityNumberDEBUG2

	// Deprecated: [v0.49.0] Use plog.SeverityNumberDEBUG3 instead.
	SeverityNumberDEBUG3 = plog.SeverityNumberDEBUG3

	// Deprecated: [v0.49.0] Use plog.SeverityNumberDEBUG4 instead.
	SeverityNumberDEBUG4 = plog.SeverityNumberDEBUG4

	// Deprecated: [v0.49.0] Use plog.SeverityNumberINFO instead.
	SeverityNumberINFO = plog.SeverityNumberINFO

	// Deprecated: [v0.49.0] Use plog.SeverityNumberINFO2 instead.
	SeverityNumberINFO2 = plog.SeverityNumberINFO2

	// Deprecated: [v0.49.0] Use plog.SeverityNumberINFO3 instead.
	SeverityNumberINFO3 = plog.SeverityNumberINFO3

	// Deprecated: [v0.49.0] Use plog.SeverityNumberINFO4 instead.
	SeverityNumberINFO4 = plog.SeverityNumberINFO4

	// Deprecated: [v0.49.0] Use plog.SeverityNumberWARN instead.
	SeverityNumberWARN = plog.SeverityNumberWARN

	// Deprecated: [v0.49.0] Use plog.SeverityNumberWARN2 instead.
	SeverityNumberWARN2 = plog.SeverityNumberWARN2

	// Deprecated: [v0.49.0] Use plog.SeverityNumberWARN3 instead.
	SeverityNumberWARN3 = plog.SeverityNumberWARN3

	// Deprecated: [v0.49.0] Use plog.SeverityNumberWARN4 instead.
	SeverityNumberWARN4 = plog.SeverityNumberWARN4

	// Deprecated: [v0.49.0] Use plog.SeverityNumberERROR instead.
	SeverityNumberERROR = plog.SeverityNumberERROR

	// Deprecated: [v0.49.0] Use plog.SeverityNumberERROR2 instead.
	SeverityNumberERROR2 = plog.SeverityNumberERROR2

	// Deprecated: [v0.49.0] Use plog.SeverityNumberERROR3 instead.
	SeverityNumberERROR3 = plog.SeverityNumberERROR3

	// Deprecated: [v0.49.0] Use plog.SeverityNumberERROR4 instead.
	SeverityNumberERROR4 = plog.SeverityNumberERROR4

	// Deprecated: [v0.49.0] Use plog.SeverityNumberFATAL instead.
	SeverityNumberFATAL = plog.SeverityNumberFATAL

	// Deprecated: [v0.49.0] Use plog.SeverityNumberFATAL2 instead.
	SeverityNumberFATAL2 = plog.SeverityNumberFATAL2

	// Deprecated: [v0.49.0] Use plog.SeverityNumberFATAL3 instead.
	SeverityNumberFATAL3 = plog.SeverityNumberFATAL3

	// Deprecated: [v0.49.0] Use plog.SeverityNumberFATAL4 instead.
	SeverityNumberFATAL4 = plog.SeverityNumberFATAL4
)
