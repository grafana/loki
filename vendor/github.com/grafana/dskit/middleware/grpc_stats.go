// Provenance-includes-location: https://github.com/weaveworks/common/blob/main/middleware/grpc_stats.go
// Provenance-includes-license: Apache-2.0
// Provenance-includes-copyright: Weaveworks Ltd.

package middleware

import (
	"context"
	"sync"

	"github.com/google/btree"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc/stats"
)

// NewStatsHandler creates handler that can be added to gRPC server options to track received and sent message sizes.
func NewStatsHandler(receivedPayloadSize, sentPayloadSize *prometheus.HistogramVec, inflightRequests *prometheus.GaugeVec, grpcConcurrentStreamsByConnMax *prometheus.GaugeVec) stats.Handler {
	return &grpcStatsHandler{
		receivedPayloadSize: receivedPayloadSize,
		sentPayloadSize:     sentPayloadSize,
		inflightRequests:    inflightRequests,

		grpcConcurrentStreamsTracker:        NewStreamTracker(),
		grpcConcurrentStreamsByConnMaxGauge: grpcConcurrentStreamsByConnMax,
	}
}

type grpcStatsHandler struct {
	receivedPayloadSize *prometheus.HistogramVec
	sentPayloadSize     *prometheus.HistogramVec
	inflightRequests    *prometheus.GaugeVec

	grpcConcurrentStreamsTracker        *StreamTracker
	grpcConcurrentStreamsByConnMaxGauge *prometheus.GaugeVec
}

// Custom type to hide it from other packages.
type contextKey int

const (
	contextKeyMethodName contextKey = 1
	contextKeyRouteName  contextKey = 2
	contextKeyConnID     contextKey = 3
)

func (g *grpcStatsHandler) TagRPC(ctx context.Context, info *stats.RPCTagInfo) context.Context {
	return context.WithValue(ctx, contextKeyMethodName, info.FullMethodName)
}

func (g *grpcStatsHandler) HandleRPC(ctx context.Context, rpcStats stats.RPCStats) {
	// We use full method name from context, because not all RPCStats structs have it.
	fullMethodName, ok := ctx.Value(contextKeyMethodName).(string)
	if !ok {
		return
	}

	connID, hasConnID := ctx.Value(contextKeyConnID).(string)

	switch s := rpcStats.(type) {
	case *stats.Begin:
		g.inflightRequests.WithLabelValues(gRPC, fullMethodName).Inc()
		if hasConnID {
			g.grpcConcurrentStreamsTracker.OpenStream(connID)
			g.grpcConcurrentStreamsByConnMaxGauge.WithLabelValues().Set(float64(g.grpcConcurrentStreamsTracker.MaxStreams()))
		}
	case *stats.End:
		g.inflightRequests.WithLabelValues(gRPC, fullMethodName).Dec()
		if hasConnID {
			g.grpcConcurrentStreamsTracker.CloseStream(connID)
			g.grpcConcurrentStreamsByConnMaxGauge.WithLabelValues().Set(float64(g.grpcConcurrentStreamsTracker.MaxStreams()))
		}
	case *stats.InHeader:
		// Ignore incoming headers.
	case *stats.InPayload:
		g.receivedPayloadSize.WithLabelValues(gRPC, fullMethodName).Observe(float64(s.WireLength))
	case *stats.InTrailer:
		// Ignore incoming trailers.
	case *stats.OutHeader:
		// Ignore outgoing headers.
	case *stats.OutPayload:
		g.sentPayloadSize.WithLabelValues(gRPC, fullMethodName).Observe(float64(s.WireLength))
	case *stats.OutTrailer:
		// Ignore outgoing trailers. OutTrailer doesn't have valid WireLength (there is a deprecated field, always set to 0).
	}
}

func (g *grpcStatsHandler) TagConn(ctx context.Context, conn *stats.ConnTagInfo) context.Context {
	return context.WithValue(ctx, contextKeyConnID, conn.LocalAddr.String()+":"+conn.RemoteAddr.String())
}

func (g *grpcStatsHandler) HandleConn(_ context.Context, _ stats.ConnStats) {
	// Not interested.
}

// streamEntry represents a connection and its stream count.
type streamEntry struct {
	streamCount int
	connID      string
}

// Less defines the sort order: descending by streamCount, then ascending by connID.
func (a streamEntry) Less(b btree.Item) bool {
	bb := b.(streamEntry)
	if a.streamCount != bb.streamCount {
		return a.streamCount > bb.streamCount // descending order
	}
	return a.connID < bb.connID
}

// StreamTracker tracks the number of streams per connection and the max.
type StreamTracker struct {
	mu      sync.RWMutex
	tree    *btree.BTree           // sorted set of stream counts
	connMap map[string]streamEntry // connID -> entry
}

func NewStreamTracker() *StreamTracker {
	return &StreamTracker{
		tree:    btree.New(2), // degree 2 is fine for small datasets
		connMap: make(map[string]streamEntry),
	}
}

func (st *StreamTracker) OpenStream(connID string) {
	st.mu.Lock()
	defer st.mu.Unlock()

	entry, exists := st.connMap[connID]
	if exists {
		st.tree.Delete(entry)
		entry.streamCount++
	} else {
		entry = streamEntry{streamCount: 1, connID: connID}
	}
	st.connMap[connID] = entry
	st.tree.ReplaceOrInsert(entry)
}

func (st *StreamTracker) CloseStream(connID string) {
	st.mu.Lock()
	defer st.mu.Unlock()

	entry, exists := st.connMap[connID]
	if !exists {
		return // no-op
	}

	st.tree.Delete(entry)
	entry.streamCount--
	if entry.streamCount == 0 {
		delete(st.connMap, connID)
	} else {
		st.connMap[connID] = entry
		st.tree.ReplaceOrInsert(entry)
	}
}

// MaxStreams returns the number of streams in the connection with the most streams.
func (st *StreamTracker) MaxStreams() int {
	st.mu.RLock()
	defer st.mu.RUnlock()

	if st.tree.Len() == 0 {
		return 0
	}
	entry := st.tree.Min().(streamEntry) // Min returns the first item in the tree which is the one with the most streams here (descending order)
	return entry.streamCount
}
