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
	"sync"
	"testing"

	"github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/ext"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uber/jaeger-client-go/internal/throttler"
)

func TestBaggageIterator(t *testing.T) {
	service := "DOOP"
	tracer, closer := NewTracer(service, NewConstSampler(true), NewNullReporter())
	defer closer.Close()

	sp1 := tracer.StartSpan("s1").(*Span)
	sp1.SetBaggageItem("Some_Key", "12345")
	sp1.SetBaggageItem("Some-other-key", "42")
	expectedBaggage := map[string]string{"Some_Key": "12345", "Some-other-key": "42"}
	assertBaggage(t, sp1, expectedBaggage)
	assertBaggageRecords(t, sp1, expectedBaggage)

	b := extractBaggage(sp1, false) // break out early
	assert.Equal(t, 1, len(b), "only one baggage item should be extracted")

	sp2 := tracer.StartSpan("s2", opentracing.ChildOf(sp1.Context())).(*Span)
	assertBaggage(t, sp2, expectedBaggage) // child inherits the same baggage
	require.Len(t, sp2.logs, 0)            // child doesn't inherit the baggage logs
}

func assertBaggageRecords(t *testing.T, sp *Span, expected map[string]string) {
	require.Len(t, sp.logs, len(expected))
	for _, logRecord := range sp.logs {
		require.Len(t, logRecord.Fields, 3)
		require.Equal(t, "event:baggage", logRecord.Fields[0].String())
		key := logRecord.Fields[1].Value().(string)
		value := logRecord.Fields[2].Value().(string)

		require.Contains(t, expected, key)
		assert.Equal(t, expected[key], value)
	}
}

func assertBaggage(t *testing.T, sp opentracing.Span, expected map[string]string) {
	b := extractBaggage(sp, true)
	assert.Equal(t, expected, b)
}

func extractBaggage(sp opentracing.Span, allItems bool) map[string]string {
	b := make(map[string]string)
	sp.Context().ForeachBaggageItem(func(k, v string) bool {
		b[k] = v
		return allItems
	})
	return b
}

func TestSpanProperties(t *testing.T) {
	tracer, closer := NewTracer("DOOP", NewConstSampler(true), NewNullReporter())
	defer closer.Close()

	sp1 := tracer.StartSpan("s1").(*Span)
	assert.Equal(t, tracer, sp1.Tracer())
	assert.NotNil(t, sp1.Context())
}

func TestSpanOperationName(t *testing.T) {
	tracer, closer := NewTracer("DOOP", NewConstSampler(true), NewNullReporter())
	defer closer.Close()

	sp1 := tracer.StartSpan("s1").(*Span)
	sp1.SetOperationName("s2")
	sp1.Finish()

	assert.Equal(t, "s2", sp1.OperationName())
}

func TestSetTag_SamplingPriority(t *testing.T) {
	tracer, closer := NewTracer("DOOP", NewConstSampler(true), NewNullReporter(),
		TracerOptions.DebugThrottler(throttler.DefaultThrottler{}))

	sp1 := tracer.StartSpan("s1").(*Span)
	ext.SamplingPriority.Set(sp1, 0)
	assert.False(t, sp1.context.IsDebug())

	ext.SamplingPriority.Set(sp1, 1)
	assert.True(t, sp1.context.IsDebug())
	assert.NotNil(t, findDomainTag(sp1, "sampling.priority"), "sampling.priority tag should be added")
	closer.Close()

	tracer, closer = NewTracer("DOOP", NewConstSampler(true), NewNullReporter(),
		TracerOptions.DebugThrottler(testThrottler{allowAll: false}))
	defer closer.Close()

	sp1 = tracer.StartSpan("s1").(*Span)
	ext.SamplingPriority.Set(sp1, 1)
	assert.False(t, sp1.context.IsDebug(), "debug should not be allowed by the throttler")
}

type testThrottler struct {
	allowAll bool
}

func (t testThrottler) IsAllowed(operation string) bool {
	return t.allowAll
}

func TestBaggageContextRace(t *testing.T) {
	tracer, closer := NewTracer("DOOP", NewConstSampler(true), NewNullReporter())
	defer closer.Close()

	sp1 := tracer.StartSpan("s1").(*Span)

	var startWg, endWg sync.WaitGroup
	startWg.Add(1)
	endWg.Add(2)

	f := func() {
		startWg.Wait()
		sp1.SetBaggageItem("x", "y")
		sp1.Context().ForeachBaggageItem(func(k, v string) bool { return false })
		endWg.Done()
	}

	go f()
	go f()

	startWg.Done()
	endWg.Wait()
}
