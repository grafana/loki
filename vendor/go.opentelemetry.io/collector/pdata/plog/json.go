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

package plog // import "go.opentelemetry.io/collector/pdata/plog"

import (
	"bytes"

	"github.com/gogo/protobuf/jsonpb"

	"go.opentelemetry.io/collector/pdata/internal"
	otlplogs "go.opentelemetry.io/collector/pdata/internal/data/protogen/logs/v1"
	"go.opentelemetry.io/collector/pdata/plog/internal/plogjson"
)

// NewJSONMarshaler returns a Marshaler. Marshals to OTLP json bytes.
func NewJSONMarshaler() Marshaler {
	return &jsonMarshaler{delegate: jsonpb.Marshaler{}}
}

type jsonMarshaler struct {
	delegate jsonpb.Marshaler
}

func (e *jsonMarshaler) MarshalLogs(ld Logs) ([]byte, error) {
	buf := bytes.Buffer{}
	pb := internal.LogsToProto(internal.Logs(ld))
	err := e.delegate.Marshal(&buf, &pb)
	return buf.Bytes(), err
}

type jsonUnmarshaler struct{}

// NewJSONUnmarshaler returns a model.Unmarshaler. Unmarshals from OTLP json bytes.
func NewJSONUnmarshaler() Unmarshaler {
	return &jsonUnmarshaler{}
}

func (jsonUnmarshaler) UnmarshalLogs(buf []byte) (Logs, error) {
	var ld otlplogs.LogsData
	if err := plogjson.UnmarshalLogsData(buf, &ld); err != nil {
		return Logs{}, err
	}
	return Logs(internal.LogsFromProto(ld)), nil
}
