package grpc_opentracing

import (
	"strings"

	grpc_ctxtags "github.com/grpc-ecosystem/go-grpc-middleware/tags"
	opentracing "github.com/opentracing/opentracing-go"
	"google.golang.org/grpc/grpclog"
)

const (
	TagTraceId           = "trace.traceid"
	TagSpanId            = "trace.spanid"
	TagSampled           = "trace.sampled"
	jaegerNotSampledFlag = "0"
)

// injectOpentracingIdsToTags writes trace data to ctxtags.
// This is done in an incredibly hacky way, because the public-facing interface of opentracing doesn't give access to
// the TraceId and SpanId of the SpanContext. Only the Tracer's Inject/Extract methods know what these are.
// Most tracers have them encoded as keys with 'traceid' and 'spanid':
// https://github.com/openzipkin/zipkin-go-opentracing/blob/594640b9ef7e5c994e8d9499359d693c032d738c/propagation_ot.go#L29
// https://github.com/opentracing/basictracer-go/blob/1b32af207119a14b1b231d451df3ed04a72efebf/propagation_ot.go#L26
// Jaeger from Uber use one-key schema with next format '{trace-id}:{span-id}:{parent-span-id}:{flags}'
// https://www.jaegertracing.io/docs/client-libraries/#trace-span-identity
func injectOpentracingIdsToTags(span opentracing.Span, tags grpc_ctxtags.Tags) {
	if err := span.Tracer().Inject(span.Context(), opentracing.HTTPHeaders, &tagsCarrier{tags}); err != nil {
		grpclog.Infof("grpc_opentracing: failed extracting trace info into ctx %v", err)
	}
}

// tagsCarrier is a really hacky way of
type tagsCarrier struct {
	grpc_ctxtags.Tags
}

func (t *tagsCarrier) Set(key, val string) {
	key = strings.ToLower(key)
	if strings.Contains(key, "traceid") {
		t.Tags.Set(TagTraceId, val) // this will most likely be base-16 (hex) encoded
	}

	if strings.Contains(key, "spanid") && !strings.Contains(strings.ToLower(key), "parent") {
		t.Tags.Set(TagSpanId, val) // this will most likely be base-16 (hex) encoded
	}

	if strings.Contains(key, "sampled") {
		switch val {
		case "true", "false":
			t.Tags.Set(TagSampled, val)
		}
	}

	if key == "uber-trace-id" {
		parts := strings.Split(val, ":")
		if len(parts) == 4 {
			t.Tags.Set(TagTraceId, parts[0])
			t.Tags.Set(TagSpanId, parts[1])

			if parts[3] != jaegerNotSampledFlag {
				t.Tags.Set(TagSampled, "true")
			} else {
				t.Tags.Set(TagSampled, "false")
			}
		}
	}
}
