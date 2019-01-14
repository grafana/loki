// Copyright 2018 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package bigtable

import (
	"context"
	"fmt"

	"go.opencensus.io/trace"
)

func traceStartSpan(ctx context.Context, name string) context.Context {
	ctx, _ = trace.StartSpan(ctx, name)
	return ctx
}

func traceEndSpan(ctx context.Context, err error) {
	span := trace.FromContext(ctx)
	if err != nil {
		span.SetStatus(trace.Status{Message: err.Error()})
	}

	span.End()
}

func tracePrintf(ctx context.Context, attrMap map[string]interface{}, format string, args ...interface{}) {
	var attrs []trace.Attribute
	for k, v := range attrMap {
		var a trace.Attribute
		switch v := v.(type) {
		case string:
			a = trace.StringAttribute(k, v)
		case bool:
			a = trace.BoolAttribute(k, v)
		case int:
			a = trace.Int64Attribute(k, int64(v))
		case int64:
			a = trace.Int64Attribute(k, v)
		default:
			a = trace.StringAttribute(k, fmt.Sprintf("%#v", v))
		}
		attrs = append(attrs, a)
	}
	trace.FromContext(ctx).Annotatef(attrs, format, args...)
}
