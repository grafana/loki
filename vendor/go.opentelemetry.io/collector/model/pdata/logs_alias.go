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

import (
	"go.opentelemetry.io/collector/model/internal/pdata"
)

// LogsMarshaler is an alias for pdata.LogsMarshaler interface.
type LogsMarshaler = pdata.LogsMarshaler

// LogsUnmarshaler is an alias for pdata.LogsUnmarshaler interface.
type LogsUnmarshaler = pdata.LogsUnmarshaler

// LogsSizer is an alias for pdata.LogsSizer interface.
type LogsSizer = pdata.LogsSizer

// Logs is an alias for pdata.Logs struct.
type Logs = pdata.Logs

// NewLogs is an alias for a function to create new Logs.
var NewLogs = pdata.NewLogs

// SeverityNumber is an alias for pdata.SeverityNumber type.
type SeverityNumber = pdata.SeverityNumber

const (
	SeverityNumberUNDEFINED = pdata.SeverityNumberUNDEFINED
	SeverityNumberTRACE     = pdata.SeverityNumberTRACE
	SeverityNumberTRACE2    = pdata.SeverityNumberTRACE2
	SeverityNumberTRACE3    = pdata.SeverityNumberTRACE3
	SeverityNumberTRACE4    = pdata.SeverityNumberTRACE4
	SeverityNumberDEBUG     = pdata.SeverityNumberDEBUG
	SeverityNumberDEBUG2    = pdata.SeverityNumberDEBUG2
	SeverityNumberDEBUG3    = pdata.SeverityNumberDEBUG3
	SeverityNumberDEBUG4    = pdata.SeverityNumberDEBUG4
	SeverityNumberINFO      = pdata.SeverityNumberINFO
	SeverityNumberINFO2     = pdata.SeverityNumberINFO2
	SeverityNumberINFO3     = pdata.SeverityNumberINFO3
	SeverityNumberINFO4     = pdata.SeverityNumberINFO4
	SeverityNumberWARN      = pdata.SeverityNumberWARN
	SeverityNumberWARN2     = pdata.SeverityNumberWARN2
	SeverityNumberWARN3     = pdata.SeverityNumberWARN3
	SeverityNumberWARN4     = pdata.SeverityNumberWARN4
	SeverityNumberERROR     = pdata.SeverityNumberERROR
	SeverityNumberERROR2    = pdata.SeverityNumberERROR2
	SeverityNumberERROR3    = pdata.SeverityNumberERROR3
	SeverityNumberERROR4    = pdata.SeverityNumberERROR4
	SeverityNumberFATAL     = pdata.SeverityNumberFATAL
	SeverityNumberFATAL2    = pdata.SeverityNumberFATAL2
	SeverityNumberFATAL3    = pdata.SeverityNumberFATAL3
	SeverityNumberFATAL4    = pdata.SeverityNumberFATAL4
)

// Deprecated: [v0.48.0] Use ScopeLogsSlice instead.
type InstrumentationLibraryLogsSlice = pdata.ScopeLogsSlice

// Deprecated: [v0.48.0] Use NewScopeLogsSlice instead.
var NewInstrumentationLibraryLogsSlice = pdata.NewScopeLogsSlice

// Deprecated: [v0.48.0] Use ScopeLogs instead.
type InstrumentationLibraryLogs = pdata.ScopeLogs

// Deprecated: [v0.48.0] Use NewScopeLogs instead.
var NewInstrumentationLibraryLogs = pdata.NewScopeLogs
