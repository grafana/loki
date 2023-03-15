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
	"go.opentelemetry.io/collector/pdata/internal"
	otlplogs "go.opentelemetry.io/collector/pdata/internal/data/protogen/logs/v1"
)

// NewProtoMarshaler returns a Marshaler. Marshals to OTLP binary protobuf bytes.
func NewProtoMarshaler() Marshaler {
	return newPbMarshaler()
}

// TODO(#3842): Figure out how we want to represent/return *Sizers.
type pbMarshaler struct{}

func newPbMarshaler() *pbMarshaler {
	return &pbMarshaler{}
}

var _ Sizer = (*pbMarshaler)(nil)

func (e *pbMarshaler) MarshalLogs(ld Logs) ([]byte, error) {
	pb := internal.LogsToProto(ld)
	return pb.Marshal()
}

func (e *pbMarshaler) LogsSize(ld Logs) int {
	pb := internal.LogsToProto(ld)
	return pb.Size()
}

type pbUnmarshaler struct{}

// NewProtoUnmarshaler returns a model.Unmarshaler. Unmarshals from OTLP binary protobuf bytes.
func NewProtoUnmarshaler() Unmarshaler {
	return newPbUnmarshaler()
}

func newPbUnmarshaler() *pbUnmarshaler {
	return &pbUnmarshaler{}
}

func (d *pbUnmarshaler) UnmarshalLogs(buf []byte) (Logs, error) {
	pb := otlplogs.LogsData{}
	err := pb.Unmarshal(buf)
	return internal.LogsFromProto(pb), err
}
