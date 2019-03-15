// Copyright 2018, OpenCensus Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package ocagent

import (
	"time"

	"go.opencensus.io/trace"
	"go.opencensus.io/trace/tracestate"

	tracepb "github.com/census-instrumentation/opencensus-proto/gen-go/trace/v1"
	"github.com/golang/protobuf/ptypes/timestamp"
)

func ocSpanToProtoSpan(sd *trace.SpanData) *tracepb.Span {
	if sd == nil {
		return nil
	}
	var namePtr *tracepb.TruncatableString
	if sd.Name != "" {
		namePtr = &tracepb.TruncatableString{Value: sd.Name}
	}
	return &tracepb.Span{
		TraceId:      sd.TraceID[:],
		SpanId:       sd.SpanID[:],
		ParentSpanId: sd.ParentSpanID[:],
		Status:       ocStatusToProtoStatus(sd.Status),
		StartTime:    timeToTimestamp(sd.StartTime),
		EndTime:      timeToTimestamp(sd.EndTime),
		Links:        ocLinksToProtoLinks(sd.Links),
		Kind:         ocSpanKindToProtoSpanKind(sd.SpanKind),
		Name:         namePtr,
		Attributes:   ocAttributesToProtoAttributes(sd.Attributes),
		Tracestate:   ocTracestateToProtoTracestate(sd.Tracestate),
	}
}

var blankStatus trace.Status

func ocStatusToProtoStatus(status trace.Status) *tracepb.Status {
	if status == blankStatus {
		return nil
	}
	return &tracepb.Status{
		Code:    status.Code,
		Message: status.Message,
	}
}

func ocLinksToProtoLinks(links []trace.Link) *tracepb.Span_Links {
	if len(links) == 0 {
		return nil
	}

	sl := make([]*tracepb.Span_Link, 0, len(links))
	for _, ocLink := range links {
		// This redefinition is necessary to prevent ocLink.*ID[:] copies
		// being reused -- in short we need a new ocLink per iteration.
		ocLink := ocLink

		sl = append(sl, &tracepb.Span_Link{
			TraceId: ocLink.TraceID[:],
			SpanId:  ocLink.SpanID[:],
			Type:    ocLinkTypeToProtoLinkType(ocLink.Type),
		})
	}

	return &tracepb.Span_Links{
		Link: sl,
	}
}

func ocLinkTypeToProtoLinkType(oct trace.LinkType) tracepb.Span_Link_Type {
	switch oct {
	case trace.LinkTypeChild:
		return tracepb.Span_Link_CHILD_LINKED_SPAN
	case trace.LinkTypeParent:
		return tracepb.Span_Link_PARENT_LINKED_SPAN
	default:
		return tracepb.Span_Link_TYPE_UNSPECIFIED
	}
}

func ocAttributesToProtoAttributes(attrs map[string]interface{}) *tracepb.Span_Attributes {
	if len(attrs) == 0 {
		return nil
	}
	outMap := make(map[string]*tracepb.AttributeValue)
	for k, v := range attrs {
		switch v := v.(type) {
		case bool:
			outMap[k] = &tracepb.AttributeValue{Value: &tracepb.AttributeValue_BoolValue{BoolValue: v}}

		case int:
			outMap[k] = &tracepb.AttributeValue{Value: &tracepb.AttributeValue_IntValue{IntValue: int64(v)}}

		case int64:
			outMap[k] = &tracepb.AttributeValue{Value: &tracepb.AttributeValue_IntValue{IntValue: v}}

		case string:
			outMap[k] = &tracepb.AttributeValue{
				Value: &tracepb.AttributeValue_StringValue{
					StringValue: &tracepb.TruncatableString{Value: v},
				},
			}
		}
	}
	return &tracepb.Span_Attributes{
		AttributeMap: outMap,
	}
}

func timeToTimestamp(t time.Time) *timestamp.Timestamp {
	nanoTime := t.UnixNano()
	return &timestamp.Timestamp{
		Seconds: nanoTime / 1e9,
		Nanos:   int32(nanoTime % 1e9),
	}
}

func ocSpanKindToProtoSpanKind(kind int) tracepb.Span_SpanKind {
	switch kind {
	case trace.SpanKindClient:
		return tracepb.Span_CLIENT
	case trace.SpanKindServer:
		return tracepb.Span_SERVER
	default:
		return tracepb.Span_SPAN_KIND_UNSPECIFIED
	}
}

func ocTracestateToProtoTracestate(ts *tracestate.Tracestate) *tracepb.Span_Tracestate {
	if ts == nil {
		return nil
	}
	return &tracepb.Span_Tracestate{
		Entries: ocTracestateEntriesToProtoTracestateEntries(ts.Entries()),
	}
}

func ocTracestateEntriesToProtoTracestateEntries(entries []tracestate.Entry) []*tracepb.Span_Tracestate_Entry {
	protoEntries := make([]*tracepb.Span_Tracestate_Entry, 0, len(entries))
	for _, entry := range entries {
		protoEntries = append(protoEntries, &tracepb.Span_Tracestate_Entry{
			Key:   entry.Key,
			Value: entry.Value,
		})
	}
	return protoEntries
}
