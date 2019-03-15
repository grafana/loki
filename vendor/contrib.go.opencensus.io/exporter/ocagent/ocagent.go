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
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"google.golang.org/api/support/bundler"
	"google.golang.org/grpc"

	"go.opencensus.io/trace"

	agentcommonpb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/common/v1"
	agenttracepb "github.com/census-instrumentation/opencensus-proto/gen-go/agent/trace/v1"
	tracepb "github.com/census-instrumentation/opencensus-proto/gen-go/trace/v1"
)

var startupMu sync.Mutex
var startTime time.Time

func init() {
	startupMu.Lock()
	startTime = time.Now()
	startupMu.Unlock()
}

var _ trace.Exporter = (*Exporter)(nil)

type Exporter struct {
	// mu protects the non-atomic and non-channel variables
	mu              sync.RWMutex
	started         bool
	stopped         bool
	agentPort       uint16
	agentAddress    string
	serviceName     string
	canDialInsecure bool
	traceSvcClient  agenttracepb.TraceServiceClient
	traceExporter   agenttracepb.TraceService_ExportClient
	nodeInfo        *agentcommonpb.Node
	grpcClientConn  *grpc.ClientConn

	traceBundler *bundler.Bundler
}

func NewExporter(opts ...ExporterOption) (*Exporter, error) {
	exp, err := NewUnstartedExporter(opts...)
	if err != nil {
		return nil, err
	}
	if err := exp.Start(); err != nil {
		return nil, err
	}
	return exp, nil
}

const spanDataBufferSize = 300

func NewUnstartedExporter(opts ...ExporterOption) (*Exporter, error) {
	e := new(Exporter)
	for _, opt := range opts {
		opt.withExporter(e)
	}
	if e.agentPort <= 0 {
		e.agentPort = DefaultAgentPort
	}
	traceBundler := bundler.NewBundler((*trace.SpanData)(nil), func(bundle interface{}) {
		e.uploadTraces(bundle.([]*trace.SpanData))
	})
	traceBundler.DelayThreshold = 2 * time.Second
	traceBundler.BundleCountThreshold = spanDataBufferSize
	e.traceBundler = traceBundler
	e.nodeInfo = createNodeInfo(e.serviceName)
	return e, nil
}

const (
	maxInitialConfigRetries = 10
	maxInitialTracesRetries = 10
)

// Start dials to the agent, establishing a connection to it. It also
// initiates the Config and Trace services by sending over the initial
// messages that consist of the node identifier. Start performs a best case
// attempt to try to send the initial messages, by applying exponential
// backoff at most 10 times.
func (ae *Exporter) Start() error {
	ae.mu.Lock()
	defer ae.mu.Unlock()

	err := ae.doStartLocked()
	if err == nil {
		ae.started = true
		return nil
	}

	// Otherwise we have an error and should clean up to avoid leaking resources.
	ae.started = false
	if ae.grpcClientConn != nil {
		ae.grpcClientConn.Close()
	}

	return err
}

func (ae *Exporter) prepareAgentAddress() string {
	if ae.agentAddress != "" {
		return ae.agentAddress
	}
	port := DefaultAgentPort
	if ae.agentPort > 0 {
		port = ae.agentPort
	}
	return fmt.Sprintf("%s:%d", DefaultAgentHost, port)
}

func (ae *Exporter) doStartLocked() error {
	if ae.started {
		return nil
	}

	// Now start it
	cc, err := ae.dialToAgent()
	if err != nil {
		return err
	}
	ae.grpcClientConn = cc

	// Initiate the trace service by sending over node identifier info.
	traceSvcClient := agenttracepb.NewTraceServiceClient(cc)
	traceExporter, err := traceSvcClient.Export(context.Background())
	if err != nil {
		return fmt.Errorf("Exporter.Start:: TraceServiceClient: %v", err)
	}

	firstTraceMessage := &agenttracepb.ExportTraceServiceRequest{Node: ae.nodeInfo}
	err = nTriesWithExponentialBackoff(maxInitialTracesRetries, 200*time.Microsecond, func() error {
		return traceExporter.Send(firstTraceMessage)
	})
	if err != nil {
		return fmt.Errorf("Exporter.Start:: Failed to initiate the Config service: %v", err)
	}
	ae.traceExporter = traceExporter

	// Initiate the config service by sending over node identifier info.
	configStream, err := traceSvcClient.Config(context.Background())
	if err != nil {
		return fmt.Errorf("Exporter.Start:: ConfigStream: %v", err)
	}
	firstCfgMessage := &agenttracepb.CurrentLibraryConfig{Node: ae.nodeInfo}
	err = nTriesWithExponentialBackoff(maxInitialConfigRetries, 200*time.Microsecond, func() error {
		return configStream.Send(firstCfgMessage)
	})
	if err != nil {
		return fmt.Errorf("Exporter.Start:: Failed to initiate the Config service: %v", err)
	}

	// In the background, handle trace configurations that are beamed down
	// by the agent, but also reply to it with the applied configuration.
	go ae.handleConfigStreaming(configStream)

	return nil
}

// dialToAgent performs a best case attempt to dial to the agent.
// It retries failed dials with:
//  * gRPC dialTimeout of 1s
//  * exponential backoff, 5 times with a period of 50ms
// hence in the worst case of (no agent actually available), it
// will take at least:
//      (5 * 1s) + ((1<<5)-1) * 0.01 s = 5s + 1.55s = 6.55s
func (ae *Exporter) dialToAgent() (*grpc.ClientConn, error) {
	addr := ae.prepareAgentAddress()
	dialOpts := []grpc.DialOption{grpc.WithBlock()}
	if ae.canDialInsecure {
		dialOpts = append(dialOpts, grpc.WithInsecure())
	}

	var cc *grpc.ClientConn
	dialOpts = append(dialOpts, grpc.WithTimeout(1*time.Second))
	dialBackoffWaitPeriod := 50 * time.Millisecond
	err := nTriesWithExponentialBackoff(5, dialBackoffWaitPeriod, func() error {
		var err error
		cc, err = grpc.Dial(addr, dialOpts...)
		return err
	})
	return cc, err
}

func (ae *Exporter) handleConfigStreaming(configStream agenttracepb.TraceService_ConfigClient) error {
	for {
		recv, err := configStream.Recv()
		if err != nil {
			// TODO: Check if this is a transient error or exponential backoff-able.
			return err
		}
		cfg := recv.Config
		if cfg == nil {
			continue
		}

		// Otherwise now apply the trace configuration sent down from the agent
		if psamp := cfg.GetProbabilitySampler(); psamp != nil {
			trace.ApplyConfig(trace.Config{DefaultSampler: trace.ProbabilitySampler(psamp.SamplingProbability)})
		} else if csamp := cfg.GetConstantSampler(); csamp != nil {
			alwaysSample := csamp.Decision == true
			if alwaysSample {
				trace.ApplyConfig(trace.Config{DefaultSampler: trace.AlwaysSample()})
			} else {
				trace.ApplyConfig(trace.Config{DefaultSampler: trace.NeverSample()})
			}
		} else { // TODO: Add the rate limiting sampler here
		}

		// Then finally send back to upstream the newly applied configuration
		err = configStream.Send(&agenttracepb.CurrentLibraryConfig{Config: &tracepb.TraceConfig{Sampler: cfg.Sampler}})
		if err != nil {
			return err
		}
	}
}

var (
	errNotStarted = errors.New("not started")
)

// Stop shuts down all the connections and resources
// related to the exporter.
func (ae *Exporter) Stop() error {
	ae.mu.Lock()
	defer ae.mu.Unlock()

	if !ae.started {
		return errNotStarted
	}
	if ae.stopped {
		// TODO: tell the user that we've already stopped, so perhaps a sentinel error?
		return nil
	}

	ae.Flush()

	// Now close the underlying gRPC connection.
	var err error
	if ae.grpcClientConn != nil {
		err = ae.grpcClientConn.Close()
	}

	// At this point we can change the state variables: started and stopped
	ae.started = false
	ae.stopped = true

	return err
}

func (ae *Exporter) ExportSpan(sd *trace.SpanData) {
	if sd == nil {
		return
	}
	_ = ae.traceBundler.Add(sd, -1)
}

func (ae *Exporter) uploadTraces(sdl []*trace.SpanData) {
	if len(sdl) == 0 {
		return
	}
	protoSpans := make([]*tracepb.Span, 0, len(sdl))
	for _, sd := range sdl {
		if sd != nil {
			protoSpans = append(protoSpans, ocSpanToProtoSpan(sd))
		}
	}

	if len(protoSpans) > 0 {
		_ = ae.traceExporter.Send(&agenttracepb.ExportTraceServiceRequest{
			Spans: protoSpans,
		})
	}
}

func (ae *Exporter) Flush() {
	ae.traceBundler.Flush()
}
