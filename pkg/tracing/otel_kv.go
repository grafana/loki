package tracing

import (
	"fmt"

	"github.com/grafana/dskit/tracing"
	"go.opentelemetry.io/otel/attribute"
)

func KeyValuesToOTelAttributes(kvps ...any) []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, len(kvps)/2)
	for i := 0; i < len(kvps); i += 2 {
		if i+1 < len(kvps) {
			key, ok := kvps[i].(string)
			if !ok {
				key = fmt.Sprintf("not_string_key:%v", kvps[i])
			}
			attrs = append(attrs, tracing.KeyValueToOTelAttribute(key, kvps[i+1]))
		}
	}
	return attrs
}
