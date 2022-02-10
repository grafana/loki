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

package otlpreceiver // import "go.opentelemetry.io/collector/receiver/otlpreceiver"

import (
	"bytes"

	"github.com/gogo/protobuf/jsonpb"
	"github.com/gogo/protobuf/proto"
	spb "google.golang.org/genproto/googleapis/rpc/status"

	"go.opentelemetry.io/collector/model/otlpgrpc"
)

const (
	pbContentType   = "application/x-protobuf"
	jsonContentType = "application/json"
)

var (
	pbEncoder     = &protoEncoder{}
	jsEncoder     = &jsonEncoder{}
	jsonMarshaler = &jsonpb.Marshaler{}
)

type encoder interface {
	unmarshalTracesRequest(buf []byte) (otlpgrpc.TracesRequest, error)
	unmarshalMetricsRequest(buf []byte) (otlpgrpc.MetricsRequest, error)
	unmarshalLogsRequest(buf []byte) (otlpgrpc.LogsRequest, error)

	marshalTracesResponse(otlpgrpc.TracesResponse) ([]byte, error)
	marshalMetricsResponse(otlpgrpc.MetricsResponse) ([]byte, error)
	marshalLogsResponse(otlpgrpc.LogsResponse) ([]byte, error)

	marshalStatus(rsp *spb.Status) ([]byte, error)

	contentType() string
}

type protoEncoder struct{}

func (protoEncoder) unmarshalTracesRequest(buf []byte) (otlpgrpc.TracesRequest, error) {
	return otlpgrpc.UnmarshalTracesRequest(buf)
}

func (protoEncoder) unmarshalMetricsRequest(buf []byte) (otlpgrpc.MetricsRequest, error) {
	return otlpgrpc.UnmarshalMetricsRequest(buf)
}

func (protoEncoder) unmarshalLogsRequest(buf []byte) (otlpgrpc.LogsRequest, error) {
	return otlpgrpc.UnmarshalLogsRequest(buf)
}

func (protoEncoder) marshalTracesResponse(resp otlpgrpc.TracesResponse) ([]byte, error) {
	return resp.Marshal()
}

func (protoEncoder) marshalMetricsResponse(resp otlpgrpc.MetricsResponse) ([]byte, error) {
	return resp.Marshal()
}

func (protoEncoder) marshalLogsResponse(resp otlpgrpc.LogsResponse) ([]byte, error) {
	return resp.Marshal()
}

func (protoEncoder) marshalStatus(resp *spb.Status) ([]byte, error) {
	return proto.Marshal(resp)
}

func (protoEncoder) contentType() string {
	return pbContentType
}

type jsonEncoder struct{}

func (jsonEncoder) unmarshalTracesRequest(buf []byte) (otlpgrpc.TracesRequest, error) {
	return otlpgrpc.UnmarshalJSONTracesRequest(buf)
}

func (jsonEncoder) unmarshalMetricsRequest(buf []byte) (otlpgrpc.MetricsRequest, error) {
	return otlpgrpc.UnmarshalJSONMetricsRequest(buf)
}

func (jsonEncoder) unmarshalLogsRequest(buf []byte) (otlpgrpc.LogsRequest, error) {
	return otlpgrpc.UnmarshalJSONLogsRequest(buf)
}

func (jsonEncoder) marshalTracesResponse(resp otlpgrpc.TracesResponse) ([]byte, error) {
	return resp.MarshalJSON()
}

func (jsonEncoder) marshalMetricsResponse(resp otlpgrpc.MetricsResponse) ([]byte, error) {
	return resp.MarshalJSON()
}

func (jsonEncoder) marshalLogsResponse(resp otlpgrpc.LogsResponse) ([]byte, error) {
	return resp.MarshalJSON()
}

func (jsonEncoder) marshalStatus(resp *spb.Status) ([]byte, error) {
	buf := new(bytes.Buffer)
	err := jsonMarshaler.Marshal(buf, resp)
	return buf.Bytes(), err
}

func (jsonEncoder) contentType() string {
	return jsonContentType
}
