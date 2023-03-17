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
	"go.opentelemetry.io/collector/model/pdata"
)

// NewJSONTracesMarshaler returns a model.TracesMarshaler. Marshals to OTLP json bytes.
func NewJSONTracesMarshaler() pdata.TracesMarshaler {
	return newJSONMarshaler()
}

// NewJSONMetricsMarshaler returns a model.MetricsMarshaler. Marshals to OTLP json bytes.
func NewJSONMetricsMarshaler() pdata.MetricsMarshaler {
	return newJSONMarshaler()
}

// NewJSONLogsMarshaler returns a model.LogsMarshaler. Marshals to OTLP json bytes.
func NewJSONLogsMarshaler() pdata.LogsMarshaler {
	return newJSONMarshaler()
}

type jsonMarshaler struct {
	delegate jsonpb.Marshaler
}

func newJSONMarshaler() *jsonMarshaler {
	return &jsonMarshaler{delegate: jsonpb.Marshaler{}}
}

func (e *jsonMarshaler) MarshalLogs(ld pdata.Logs) ([]byte, error) {
	buf := bytes.Buffer{}
	err := e.delegate.Marshal(&buf, internal.LogsToOtlp(ld.InternalRep()))
	return buf.Bytes(), err
}

func (e *jsonMarshaler) MarshalMetrics(md pdata.Metrics) ([]byte, error) {
	buf := bytes.Buffer{}
	err := e.delegate.Marshal(&buf, internal.MetricsToOtlp(md.InternalRep()))
	return buf.Bytes(), err
}

func (e *jsonMarshaler) MarshalTraces(td pdata.Traces) ([]byte, error) {
	buf := bytes.Buffer{}
	err := e.delegate.Marshal(&buf, internal.TracesToOtlp(td.InternalRep()))
	return buf.Bytes(), err
}
