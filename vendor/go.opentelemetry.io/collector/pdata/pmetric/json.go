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

package pmetric // import "go.opentelemetry.io/collector/pdata/pmetric"

import (
	"bytes"

	"github.com/gogo/protobuf/jsonpb"

	"go.opentelemetry.io/collector/pdata/internal"
	otlpmetrics "go.opentelemetry.io/collector/pdata/internal/data/protogen/metrics/v1"
	"go.opentelemetry.io/collector/pdata/internal/otlp"
)

// NewJSONMarshaler returns a model.Marshaler. Marshals to OTLP json bytes.
func NewJSONMarshaler() Marshaler {
	return newJSONMarshaler()
}

type jsonMarshaler struct {
	delegate jsonpb.Marshaler
}

func newJSONMarshaler() *jsonMarshaler {
	return &jsonMarshaler{delegate: jsonpb.Marshaler{}}
}

func (e *jsonMarshaler) MarshalMetrics(md Metrics) ([]byte, error) {
	buf := bytes.Buffer{}
	err := e.delegate.Marshal(&buf, internal.MetricsToOtlp(md))
	return buf.Bytes(), err
}

type jsonUnmarshaler struct {
	delegate jsonpb.Unmarshaler
}

// NewJSONUnmarshaler returns a model.Unmarshaler. Unmarshals from OTLP json bytes.
func NewJSONUnmarshaler() Unmarshaler {
	return newJSONUnmarshaler()
}

func newJSONUnmarshaler() *jsonUnmarshaler {
	return &jsonUnmarshaler{delegate: jsonpb.Unmarshaler{}}
}

func (d *jsonUnmarshaler) UnmarshalMetrics(buf []byte) (Metrics, error) {
	md := otlpmetrics.MetricsData{}
	if err := d.delegate.Unmarshal(bytes.NewReader(buf), &md); err != nil {
		return Metrics{}, err
	}
	otlp.InstrumentationLibraryMetricsToScope(md.ResourceMetrics)
	return internal.MetricsFromProto(md), nil
}
