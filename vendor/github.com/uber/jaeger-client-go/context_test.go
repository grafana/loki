// Copyright (c) 2017 Uber Technologies, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package jaeger

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContextFromString(t *testing.T) {
	var err error
	_, err = ContextFromString("")
	assert.Error(t, err)
	_, err = ContextFromString("abcd")
	assert.Error(t, err)
	_, err = ContextFromString("x:1:1:1")
	assert.Error(t, err)
	_, err = ContextFromString("1:x:1:1")
	assert.Error(t, err)
	_, err = ContextFromString("1:1:x:1")
	assert.Error(t, err)
	_, err = ContextFromString("1:1:1:x")
	assert.Error(t, err)
	_, err = ContextFromString("1:1:1:x")
	assert.Error(t, err)
	_, err = ContextFromString("01234567890123456789012345678901234:1:1:1")
	assert.Error(t, err)
	_, err = ContextFromString("01234567890123456789012345678901:1:1:1")
	assert.NoError(t, err)
	_, err = ContextFromString("01234_67890123456789012345678901:1:1:1")
	assert.Error(t, err)
	_, err = ContextFromString("0123456789012345678901_345678901:1:1:1")
	assert.Error(t, err)
	_, err = ContextFromString("1:0123456789012345:1:1")
	assert.NoError(t, err)
	_, err = ContextFromString("1:01234567890123456:1:1")
	assert.Error(t, err)
	ctx, err := ContextFromString("10000000000000001:1:1:1")
	assert.NoError(t, err)
	assert.EqualValues(t, TraceID{High: 1, Low: 1}, ctx.traceID)
	ctx, err = ContextFromString("1:1:1:1")
	assert.NoError(t, err)
	assert.EqualValues(t, TraceID{Low: 1}, ctx.traceID)
	assert.EqualValues(t, 1, ctx.spanID)
	assert.EqualValues(t, 1, ctx.parentID)
	assert.EqualValues(t, 1, ctx.flags)
	ctx = NewSpanContext(TraceID{Low: 1}, 1, 1, true, nil)
	assert.EqualValues(t, TraceID{Low: 1}, ctx.traceID)
	assert.EqualValues(t, 1, ctx.spanID)
	assert.EqualValues(t, 1, ctx.parentID)
	assert.EqualValues(t, 1, ctx.flags)
	assert.Equal(t, "ff", SpanID(255).String())
	assert.Equal(t, "ff", TraceID{Low: 255}.String())
	assert.Equal(t, "ff00000000000000ff", TraceID{High: 255, Low: 255}.String())
	ctx = NewSpanContext(TraceID{High: 255, Low: 255}, SpanID(1), SpanID(1), false, nil)
	assert.Equal(t, "ff00000000000000ff:1:1:0", ctx.String())
}

func TestSpanContext_WithBaggageItem(t *testing.T) {
	var ctx SpanContext
	ctx = ctx.WithBaggageItem("some-KEY", "Some-Value")
	assert.Equal(t, map[string]string{"some-KEY": "Some-Value"}, ctx.baggage)
	ctx = ctx.WithBaggageItem("some-KEY", "Some-Other-Value")
	assert.Equal(t, map[string]string{"some-KEY": "Some-Other-Value"}, ctx.baggage)
}

func TestSpanContext_SampledDebug(t *testing.T) {
	ctx, err := ContextFromString("1:1:1:1")
	require.NoError(t, err)
	assert.True(t, ctx.IsSampled())
	assert.False(t, ctx.IsDebug())

	ctx, err = ContextFromString("1:1:1:3")
	require.NoError(t, err)
	assert.True(t, ctx.IsSampled())
	assert.True(t, ctx.IsDebug())

	ctx, err = ContextFromString("1:1:1:0")
	require.NoError(t, err)
	assert.False(t, ctx.IsSampled())
	assert.False(t, ctx.IsDebug())
}

func TestSpanContext_CopyFrom(t *testing.T) {
	ctx, err := ContextFromString("1:1:1:1")
	require.NoError(t, err)
	ctx2 := SpanContext{}
	ctx2.CopyFrom(&ctx)
	assert.Equal(t, ctx, ctx2)
	// with baggage
	ctx = ctx.WithBaggageItem("x", "y")
	ctx2 = SpanContext{}
	ctx2.CopyFrom(&ctx)
	assert.Equal(t, ctx, ctx2)
	assert.Equal(t, "y", ctx2.baggage["x"])
}
