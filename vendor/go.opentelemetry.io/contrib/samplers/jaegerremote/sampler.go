// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Copyright (c) 2021 The Jaeger Authors.
// Copyright (c) 2017 Uber Technologies, Inc.
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

package jaegerremote // import "go.opentelemetry.io/contrib/samplers/jaegerremote"

import (
	"encoding/binary"
	"fmt"
	"math"
	"sync"

	jaeger_api_v2 "go.opentelemetry.io/contrib/samplers/jaegerremote/internal/proto-gen/jaeger-idl/proto/api_v2"
	"go.opentelemetry.io/contrib/samplers/jaegerremote/internal/utils"
	"go.opentelemetry.io/otel/sdk/trace"
	oteltrace "go.opentelemetry.io/otel/trace"
)

const (
	defaultMaxOperations = 2000
)

// -----------------------

// probabilisticSampler is a sampler that randomly samples a certain percentage
// of traces.
type probabilisticSampler struct {
	samplingRate     float64
	samplingBoundary uint64
}

const maxRandomNumber = ^(uint64(1) << 63) // i.e. 0x7fffffffffffffff

// newProbabilisticSampler creates a sampler that randomly samples a certain percentage of traces specified by the
// samplingRate, in the range between 0.0 and 1.0.
//
// It relies on the fact that new trace IDs are 63bit random numbers themselves, thus making the sampling decision
// without generating a new random number, but simply calculating if traceID < (samplingRate * 2^63).
func newProbabilisticSampler(samplingRate float64) *probabilisticSampler {
	s := new(probabilisticSampler)
	return s.init(samplingRate)
}

func (s *probabilisticSampler) init(samplingRate float64) *probabilisticSampler {
	s.samplingRate = math.Max(0.0, math.Min(samplingRate, 1.0))
	s.samplingBoundary = uint64(float64(maxRandomNumber) * s.samplingRate)
	return s
}

// SamplingRate returns the sampling probability this sampled was constructed with.
func (s *probabilisticSampler) SamplingRate() float64 {
	return s.samplingRate
}

func (s *probabilisticSampler) ShouldSample(p trace.SamplingParameters) trace.SamplingResult {
	psc := oteltrace.SpanContextFromContext(p.ParentContext)
	traceID := binary.BigEndian.Uint64(p.TraceID[0:8])
	if s.samplingBoundary >= traceID&maxRandomNumber {
		return trace.SamplingResult{
			Decision:   trace.RecordAndSample,
			Tracestate: psc.TraceState(),
		}
	}
	return trace.SamplingResult{
		Decision:   trace.Drop,
		Tracestate: psc.TraceState(),
	}
}

// Equal compares with another sampler.
func (s *probabilisticSampler) Equal(other trace.Sampler) bool {
	if o, ok := other.(*probabilisticSampler); ok {
		return s.samplingBoundary == o.samplingBoundary
	}
	return false
}

// Update modifies in-place the sampling rate. Locking must be done externally.
func (s *probabilisticSampler) Update(samplingRate float64) error {
	if samplingRate < 0.0 || samplingRate > 1.0 {
		return fmt.Errorf("sampling rate must be between 0.0 and 1.0, received %f", samplingRate)
	}
	s.init(samplingRate)
	return nil
}

func (s *probabilisticSampler) Description() string {
	return "probabilisticSampler{}"
}

// -----------------------

// rateLimitingSampler samples at most maxTracesPerSecond. The distribution of sampled traces follows
// burstiness of the service, i.e. a service with uniformly distributed requests will have those
// requests sampled uniformly as well, but if requests are bursty, especially sub-second, then a
// number of sequential requests can be sampled each second.
type rateLimitingSampler struct {
	maxTracesPerSecond float64
	rateLimiter        *utils.RateLimiter
}

// newRateLimitingSampler creates new rateLimitingSampler.
func newRateLimitingSampler(maxTracesPerSecond float64) *rateLimitingSampler {
	s := new(rateLimitingSampler)
	return s.init(maxTracesPerSecond)
}

func (s *rateLimitingSampler) init(maxTracesPerSecond float64) *rateLimitingSampler {
	if s.rateLimiter == nil {
		s.rateLimiter = utils.NewRateLimiter(maxTracesPerSecond, math.Max(maxTracesPerSecond, 1.0))
	} else {
		s.rateLimiter.Update(maxTracesPerSecond, math.Max(maxTracesPerSecond, 1.0))
	}
	s.maxTracesPerSecond = maxTracesPerSecond
	return s
}

func (s *rateLimitingSampler) ShouldSample(p trace.SamplingParameters) trace.SamplingResult {
	psc := oteltrace.SpanContextFromContext(p.ParentContext)
	if s.rateLimiter.CheckCredit(1.0) {
		return trace.SamplingResult{
			Decision:   trace.RecordAndSample,
			Tracestate: psc.TraceState(),
		}
	}
	return trace.SamplingResult{
		Decision:   trace.Drop,
		Tracestate: psc.TraceState(),
	}
}

// Update reconfigures the rate limiter, while preserving its accumulated balance.
// Locking must be done externally.
func (s *rateLimitingSampler) Update(maxTracesPerSecond float64) {
	if s.maxTracesPerSecond != maxTracesPerSecond {
		s.init(maxTracesPerSecond)
	}
}

// Equal compares with another sampler.
func (s *rateLimitingSampler) Equal(other trace.Sampler) bool {
	if o, ok := other.(*rateLimitingSampler); ok {
		return s.maxTracesPerSecond == o.maxTracesPerSecond
	}
	return false
}

func (s *rateLimitingSampler) Description() string {
	return "rateLimitingSampler{}"
}

// -----------------------

// guaranteedThroughputProbabilisticSampler is a sampler that leverages both probabilisticSampler and
// rateLimitingSampler. The rateLimitingSampler is used as a guaranteed lower bound sampler such that
// every operation is sampled at least once in a time interval defined by the lowerBound. ie a lowerBound
// of 1.0 / (60 * 10) will sample an operation at least once every 10 minutes.
//
// The probabilisticSampler is given higher priority when tags are emitted, ie. if IsSampled() for both
// samplers return true, the tags for probabilisticSampler will be used.
type guaranteedThroughputProbabilisticSampler struct {
	probabilisticSampler *probabilisticSampler
	lowerBoundSampler    *rateLimitingSampler
	samplingRate         float64
	lowerBound           float64
}

func newGuaranteedThroughputProbabilisticSampler(lowerBound, samplingRate float64) *guaranteedThroughputProbabilisticSampler {
	s := &guaranteedThroughputProbabilisticSampler{
		lowerBoundSampler: newRateLimitingSampler(lowerBound),
		lowerBound:        lowerBound,
	}
	s.setProbabilisticSampler(samplingRate)
	return s
}

func (s *guaranteedThroughputProbabilisticSampler) setProbabilisticSampler(samplingRate float64) {
	if s.probabilisticSampler == nil {
		s.probabilisticSampler = newProbabilisticSampler(samplingRate)
	} else if s.samplingRate != samplingRate {
		s.probabilisticSampler.init(samplingRate)
	}
	// since we don't validate samplingRate, sampler may have clamped it to [0, 1] interval
	s.samplingRate = s.probabilisticSampler.SamplingRate()
}

func (s *guaranteedThroughputProbabilisticSampler) ShouldSample(p trace.SamplingParameters) trace.SamplingResult {
	if result := s.probabilisticSampler.ShouldSample(p); result.Decision == trace.RecordAndSample {
		s.lowerBoundSampler.ShouldSample(p)
		return result
	}
	result := s.lowerBoundSampler.ShouldSample(p)
	return result
}

// this function should only be called while holding a Write lock.
func (s *guaranteedThroughputProbabilisticSampler) update(lowerBound, samplingRate float64) {
	s.setProbabilisticSampler(samplingRate)
	if s.lowerBound != lowerBound {
		s.lowerBoundSampler.Update(lowerBound)
		s.lowerBound = lowerBound
	}
}

func (s *guaranteedThroughputProbabilisticSampler) Description() string {
	return "guaranteedThroughputProbabilisticSampler{}"
}

// -----------------------

// perOperationSampler is a delegating sampler that applies guaranteedThroughputProbabilisticSampler
// on a per-operation basis.
type perOperationSampler struct {
	sync.RWMutex

	samplers       map[string]*guaranteedThroughputProbabilisticSampler
	defaultSampler *probabilisticSampler
	lowerBound     float64
	maxOperations  int

	// see description in perOperationSamplerParams
	operationNameLateBinding bool
}

// perOperationSamplerParams defines parameters when creating perOperationSampler.
type perOperationSamplerParams struct {
	// Max number of operations that will be tracked. Other operations will be given default strategy.
	MaxOperations int

	// Opt-in feature for applications that require late binding of span name via explicit call to SetOperationName.
	// When this feature is enabled, the sampler will return retryable=true from OnCreateSpan(), thus leaving
	// the sampling decision as non-final (and the span as writeable). This may lead to degraded performance
	// in applications that always provide the correct span name on oteltrace creation.
	//
	// For backwards compatibility this option is off by default.
	OperationNameLateBinding bool

	// Initial configuration of the sampling strategies (usually retrieved from the backend by Remote Sampler).
	Strategies *jaeger_api_v2.PerOperationSamplingStrategies
}

// newPerOperationSampler returns a new perOperationSampler.
func newPerOperationSampler(params perOperationSamplerParams) *perOperationSampler {
	if params.MaxOperations <= 0 {
		params.MaxOperations = defaultMaxOperations
	}
	samplers := make(map[string]*guaranteedThroughputProbabilisticSampler)
	for _, strategy := range params.Strategies.PerOperationStrategies {
		sampler := newGuaranteedThroughputProbabilisticSampler(
			params.Strategies.DefaultLowerBoundTracesPerSecond,
			strategy.ProbabilisticSampling.SamplingRate,
		)
		samplers[strategy.Operation] = sampler
	}
	return &perOperationSampler{
		samplers:                 samplers,
		defaultSampler:           newProbabilisticSampler(params.Strategies.DefaultSamplingProbability),
		lowerBound:               params.Strategies.DefaultLowerBoundTracesPerSecond,
		maxOperations:            params.MaxOperations,
		operationNameLateBinding: params.OperationNameLateBinding,
	}
}

func (s *perOperationSampler) ShouldSample(p trace.SamplingParameters) trace.SamplingResult {
	sampler := s.getSamplerForOperation(p.Name)
	return sampler.ShouldSample(p)
}

func (s *perOperationSampler) getSamplerForOperation(operation string) trace.Sampler {
	s.RLock()
	sampler, ok := s.samplers[operation]
	if ok {
		defer s.RUnlock()
		return sampler
	}
	s.RUnlock()
	s.Lock()
	defer s.Unlock()

	// Check if sampler has already been created
	sampler, ok = s.samplers[operation]
	if ok {
		return sampler
	}
	// Store only up to maxOperations of unique ops.
	if len(s.samplers) >= s.maxOperations {
		return s.defaultSampler
	}
	newSampler := newGuaranteedThroughputProbabilisticSampler(s.lowerBound, s.defaultSampler.SamplingRate())
	s.samplers[operation] = newSampler
	return newSampler
}

func (s *perOperationSampler) Description() string {
	return "perOperationSampler{}"
}

func (s *perOperationSampler) update(strategies *jaeger_api_v2.PerOperationSamplingStrategies) {
	s.Lock()
	defer s.Unlock()
	newSamplers := map[string]*guaranteedThroughputProbabilisticSampler{}
	for _, strategy := range strategies.PerOperationStrategies {
		operation := strategy.Operation
		samplingRate := strategy.ProbabilisticSampling.SamplingRate
		lowerBound := strategies.DefaultLowerBoundTracesPerSecond
		if sampler, ok := s.samplers[operation]; ok {
			sampler.update(lowerBound, samplingRate)
			newSamplers[operation] = sampler
		} else {
			sampler := newGuaranteedThroughputProbabilisticSampler(
				lowerBound,
				samplingRate,
			)
			newSamplers[operation] = sampler
		}
	}
	s.lowerBound = strategies.DefaultLowerBoundTracesPerSecond
	if s.defaultSampler.SamplingRate() != strategies.DefaultSamplingProbability {
		s.defaultSampler = newProbabilisticSampler(strategies.DefaultSamplingProbability)
	}
	s.samplers = newSamplers
}
