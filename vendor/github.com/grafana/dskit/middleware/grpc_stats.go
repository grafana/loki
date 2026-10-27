// Provenance-includes-location: https://github.com/weaveworks/common/blob/main/middleware/grpc_stats.go
// Provenance-includes-license: Apache-2.0
// Provenance-includes-copyright: Weaveworks Ltd.

package middleware

import (
	"context"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/atomic"
	"google.golang.org/grpc/stats"
)

// NewStatsHandler creates handler that can be added to gRPC server options to track received and sent message sizes.
func NewStatsHandler(reg prometheus.Registerer, receivedPayloadSize, sentPayloadSize *prometheus.HistogramVec, inflightRequests *prometheus.GaugeVec, collectMaxStreamsByConn bool) stats.Handler {
	var streamTracker *StreamTracker
	if collectMaxStreamsByConn {
		grpcConcurrentStreamsByConnMax := prometheus.NewDesc(
			"grpc_concurrent_streams_by_conn_max",
			"The current number of concurrent streams in the connection with the most concurrent streams.",
			[]string{},
			prometheus.Labels{},
		)
		streamTracker = NewStreamTracker(grpcConcurrentStreamsByConnMax)
		reg.MustRegister(streamTracker)
	}

	return &grpcStatsHandler{
		receivedPayloadSize: receivedPayloadSize,
		sentPayloadSize:     sentPayloadSize,
		inflightRequests:    inflightRequests,

		grpcConcurrentStreamsTracker: streamTracker,
	}
}

type grpcStatsHandler struct {
	receivedPayloadSize *prometheus.HistogramVec
	sentPayloadSize     *prometheus.HistogramVec
	inflightRequests    *prometheus.GaugeVec

	grpcConcurrentStreamsTracker *StreamTracker
}

// Custom type to hide it from other packages.
type contextKey int

const (
	contextKeyRouteName    contextKey = 2
	contextKeyConnID       contextKey = 3
	contextKeyRPCObservers contextKey = 4
)

// rpcObservers holds pre-resolved Prometheus metric handles for a single RPC.
// They are resolved once in TagRPC and reused for every HandleRPC call,
// eliminating per-message label hashing and mutex contention.
type rpcObservers struct {
	receivedPayloadSize prometheus.Observer
	sentPayloadSize     prometheus.Observer
	inflightRequests    prometheus.Gauge
}

func (g *grpcStatsHandler) TagRPC(ctx context.Context, info *stats.RPCTagInfo) context.Context {
	// Pre-resolve all labeled metric handles once per stream so that HandleRPC
	// (called on every message) can call Observe/Inc/Dec without any label
	// hashing, map lookups, or mutex acquisitions inside the HistogramVec/GaugeVec.
	obs := &rpcObservers{
		receivedPayloadSize: g.receivedPayloadSize.WithLabelValues(gRPC, info.FullMethodName),
		sentPayloadSize:     g.sentPayloadSize.WithLabelValues(gRPC, info.FullMethodName),
		inflightRequests:    g.inflightRequests.WithLabelValues(gRPC, info.FullMethodName),
	}
	return context.WithValue(ctx, contextKeyRPCObservers, obs)
}

func (g *grpcStatsHandler) HandleRPC(ctx context.Context, rpcStats stats.RPCStats) {
	obs, ok := ctx.Value(contextKeyRPCObservers).(*rpcObservers)
	if !ok {
		return
	}

	switch s := rpcStats.(type) {
	case *stats.Begin:
		obs.inflightRequests.Inc()
		if g.grpcConcurrentStreamsTracker != nil {
			if connID, hasConnID := ctx.Value(contextKeyConnID).(string); hasConnID {
				g.grpcConcurrentStreamsTracker.OpenStream(connID)
			}
		}
	case *stats.End:
		obs.inflightRequests.Dec()
		if g.grpcConcurrentStreamsTracker != nil {
			if connID, hasConnID := ctx.Value(contextKeyConnID).(string); hasConnID {
				g.grpcConcurrentStreamsTracker.CloseStream(connID)
			}
		}
	case *stats.InHeader:
		// Ignore incoming headers.
	case *stats.InPayload:
		obs.receivedPayloadSize.Observe(float64(s.WireLength))
	case *stats.InTrailer:
		// Ignore incoming trailers.
	case *stats.OutHeader:
		// Ignore outgoing headers.
	case *stats.OutPayload:
		obs.sentPayloadSize.Observe(float64(s.WireLength))
	case *stats.OutTrailer:
		// Ignore outgoing trailers. OutTrailer doesn't have valid WireLength (there is a deprecated field, always set to 0).
	}
}

func (g *grpcStatsHandler) TagConn(ctx context.Context, conn *stats.ConnTagInfo) context.Context {
	if g.grpcConcurrentStreamsTracker != nil {
		return context.WithValue(ctx, contextKeyConnID, conn.LocalAddr.String()+":"+conn.RemoteAddr.String())
	}
	return ctx
}

func (g *grpcStatsHandler) HandleConn(_ context.Context, _ stats.ConnStats) {
	// Not interested.
}

// StreamTracker tracks the number of streams per connection and the max.
type StreamTracker struct {
	grpcConcurrentStreamsByConnMax *prometheus.Desc

	mu      sync.RWMutex
	connMap map[string]*atomic.Int32
}

func NewStreamTracker(grpcConcurrentStreamsByConnMax *prometheus.Desc) *StreamTracker {
	return &StreamTracker{
		grpcConcurrentStreamsByConnMax: grpcConcurrentStreamsByConnMax,
		connMap:                        make(map[string]*atomic.Int32),
	}
}

func (st *StreamTracker) createOrGetConnEntry(connID string) *atomic.Int32 {
	st.mu.Lock()
	defer st.mu.Unlock()
	got, ok := st.connMap[connID] // Do not overwrite new entries.
	if !ok {
		st.connMap[connID] = atomic.NewInt32(0)
		return st.connMap[connID]
	}
	return got
}

func (st *StreamTracker) OpenStream(connID string) {
	st.mu.RLock()
	conn, ok := st.connMap[connID]
	st.mu.RUnlock()
	if !ok {
		conn = st.createOrGetConnEntry(connID)
	}
	conn.Inc()
}

func (st *StreamTracker) CloseStream(connID string) {
	st.mu.RLock()
	conn := st.connMap[connID]
	st.mu.RUnlock()
	if conn == nil {
		return
	}
	if res := conn.Dec(); res == 0 {
		// Delete the entry if it's empty.
		st.mu.Lock()

		// Get the entry again to avoid race condition.
		conn = st.connMap[connID]
		if conn == nil || conn.Load() == 0 {
			delete(st.connMap, connID)
		}

		st.mu.Unlock()
	}
}

// MaxStreams returns the number of streams in the connection with the most streams.
func (st *StreamTracker) MaxStreams() int {
	var max int32 = 0
	st.mu.RLock()
	for _, conn := range st.connMap {
		if conn.Load() > max {
			max = conn.Load()
		}
	}
	st.mu.RUnlock()
	return int(max)
}

func (st *StreamTracker) Describe(ch chan<- *prometheus.Desc) {
	ch <- st.grpcConcurrentStreamsByConnMax
}

func (st *StreamTracker) Collect(ch chan<- prometheus.Metric) {
	ch <- prometheus.MustNewConstMetric(st.grpcConcurrentStreamsByConnMax, prometheus.GaugeValue, float64(st.MaxStreams()))
}
