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

package otlp

import (
	"bytes"

	"github.com/gogo/protobuf/jsonpb"

	"go.opentelemetry.io/collector/model/internal"
	otlpcollectorlog "go.opentelemetry.io/collector/model/internal/data/protogen/collector/logs/v1"
	otlpcollectormetrics "go.opentelemetry.io/collector/model/internal/data/protogen/collector/metrics/v1"
	otlpcollectortrace "go.opentelemetry.io/collector/model/internal/data/protogen/collector/trace/v1"
	"go.opentelemetry.io/collector/model/pdata"
)

type jsonUnmarshaler struct {
	delegate jsonpb.Unmarshaler
}

// NewJSONTracesUnmarshaler returns a model.TracesUnmarshaler. Unmarshals from OTLP json bytes.
func NewJSONTracesUnmarshaler() pdata.TracesUnmarshaler {
	return newJSONUnmarshaler()
}

// NewJSONMetricsUnmarshaler returns a model.MetricsUnmarshaler. Unmarshals from OTLP json bytes.
func NewJSONMetricsUnmarshaler() pdata.MetricsUnmarshaler {
	return newJSONUnmarshaler()
}

// NewJSONLogsUnmarshaler returns a model.LogsUnmarshaler. Unmarshals from OTLP json bytes.
func NewJSONLogsUnmarshaler() pdata.LogsUnmarshaler {
	return newJSONUnmarshaler()
}

func newJSONUnmarshaler() *jsonUnmarshaler {
	return &jsonUnmarshaler{delegate: jsonpb.Unmarshaler{}}
}

func (d *jsonUnmarshaler) UnmarshalLogs(buf []byte) (pdata.Logs, error) {
	ld := &otlpcollectorlog.ExportLogsServiceRequest{}
	err := d.delegate.Unmarshal(bytes.NewReader(buf), ld)
	return pdata.LogsFromInternalRep(internal.LogsFromOtlp(ld)), err
}

func (d *jsonUnmarshaler) UnmarshalMetrics(buf []byte) (pdata.Metrics, error) {
	md := &otlpcollectormetrics.ExportMetricsServiceRequest{}
	err := d.delegate.Unmarshal(bytes.NewReader(buf), md)
	return pdata.MetricsFromInternalRep(internal.MetricsFromOtlp(md)), err
}

func (d *jsonUnmarshaler) UnmarshalTraces(buf []byte) (pdata.Traces, error) {
	td := &otlpcollectortrace.ExportTraceServiceRequest{}
	err := d.delegate.Unmarshal(bytes.NewReader(buf), td)
	return pdata.TracesFromInternalRep(internal.TracesFromOtlp(td)), err
}
