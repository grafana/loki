package handler

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"

	"github.com/chaudum/go-kaff/codec"
	"github.com/chaudum/go-kaff/store"
)

// Metrics is the observability interface the Router calls into.
// It matches broker.Metrics; a no-op implementation is passed when none is
// configured so handlers never need to nil-check.
type Metrics interface {
	RecordProduce(topic string, partition int32, records int, bytes int64)
	RecordFetch(topic string, partition int32, records int, bytes int64)
	RecordGroupRebalance(groupID string, generation int32)
	SetPartitionHighWaterMark(topic string, partition int32, hwm int64)
	SetActiveConnections(n int)
}

// BrokerInfo holds the network identity and policy settings of this broker.
type BrokerInfo struct {
	// NodeID is the Kafka broker node ID (always 0 for go-kaff).
	NodeID int32
	// Host is the hostname or IP address that clients connect to.
	Host string
	// Port is the TCP port this broker listens on.
	Port int32
	// AutoCreateTopics controls whether unknown topics are created on first
	// Metadata/Produce request.
	AutoCreateTopics bool
	// DefaultPartitions is the partition count for auto-created topics.
	DefaultPartitions int32
	// MaxBytesPerPartition is the per-partition soft retention cap (0 = unlimited).
	MaxBytesPerPartition int64
}

// HandlerFunc processes the body of a decoded Kafka request and writes the
// response body into w.
//
//   - r is positioned immediately after the request header (body only).
//   - w should contain only the response-specific payload; the response header
//     (CorrelationId + optional tagged fields) is prepended by the Router.
type HandlerFunc func(hdr RequestHeader, r *codec.Reader, w *codec.Writer)

// Router maps Kafka API keys to HandlerFuncs and drives the per-connection
// read/write loop.
type Router struct {
	routes  map[int16]HandlerFunc
	store   *store.Store
	broker  BrokerInfo
	log     *slog.Logger
	metrics Metrics
	// ctx is cancelled when the broker is shutting down.  Long-poll handlers
	// (e.g. Fetch) select on ctx.Done() to return promptly on shutdown.
	ctx context.Context
}

// NewRouter wires up all handlers and returns a ready Router.
// ctx should be the broker's lifecycle context; it is cancelled on Stop().
func NewRouter(ctx context.Context, s *store.Store, b BrokerInfo, log *slog.Logger, m Metrics) *Router {
	r := &Router{
		routes:  make(map[int16]HandlerFunc),
		store:   s,
		broker:  b,
		log:     log,
		metrics: m,
		ctx:     ctx,
	}
	// Phase 1
	r.routes[apiKeyApiVersions] = r.handleApiVersions
	r.routes[apiKeyMetadata] = r.handleMetadata
	// Phase 2
	r.routes[apiKeyProduce] = r.handleProduce
	r.routes[apiKeyFetch] = r.handleFetch
	r.routes[apiKeyListOffsets] = r.handleListOffsets
	// Phase 3
	r.routes[apiKeyFindCoordinator] = r.handleFindCoordinator
	r.routes[apiKeyJoinGroup] = r.handleJoinGroup
	r.routes[apiKeySyncGroup] = r.handleSyncGroup
	r.routes[apiKeyHeartbeat] = r.handleHeartbeat
	r.routes[apiKeyLeaveGroup] = r.handleLeaveGroup
	r.routes[apiKeyOffsetCommit] = r.handleOffsetCommit
	r.routes[apiKeyOffsetFetch] = r.handleOffsetFetch
	// Phase 4
	r.routes[apiKeyDescribeGroups] = r.handleDescribeGroups
	r.routes[apiKeyListGroups] = r.handleListGroups
	r.routes[apiKeyCreateTopics] = r.handleCreateTopics
	r.routes[apiKeyDeleteTopics] = r.handleDeleteTopics
	return r
}

// ServeConn handles a single TCP connection until it is closed or a
// non-recoverable error occurs.  It is safe to call from multiple goroutines
// concurrently (each call owns its own connection).
func (rt *Router) ServeConn(conn net.Conn) {
	defer conn.Close()

	addr := conn.RemoteAddr()
	rt.log.Debug("connection opened", "remote", addr)
	defer rt.log.Debug("connection closed", "remote", addr)

	for {
		// ── Read one frame ────────────────────────────────────────────────────
		body, err := codec.ReadFrame(conn)
		if err != nil {
			if !isConnClosedErr(err) {
				rt.log.Debug("read error", "remote", addr, "err", err)
			}
			return
		}

		// ── Decode request header ─────────────────────────────────────────────
		rd := codec.NewReader(body)
		hdr := decodeRequestHeader(rd)
		if err := rd.Err(); err != nil {
			rt.log.Error("header decode error", "remote", addr, "err", err)
			return
		}

		flexible := isFlexible(hdr.ApiKey, hdr.ApiVersion)

		rt.log.Debug("request",
			"remote", addr,
			"apiKey", hdr.ApiKey,
			"apiVersion", hdr.ApiVersion,
			"correlationId", hdr.CorrelationID,
			"clientId", hdr.ClientID,
		)

		// ── Dispatch ──────────────────────────────────────────────────────────
		handler, ok := rt.routes[hdr.ApiKey]
		if !ok {
			rt.log.Warn("unsupported API key", "apiKey", hdr.ApiKey)
			// Close the connection; we cannot form a meaningful error response
			// without knowing the expected response schema for this key.
			return
		}

		// ── Call handler ──────────────────────────────────────────────────────
		bodyWriter := codec.NewWriter()
		handler(hdr, rd, bodyWriter)

		if err := rd.Err(); err != nil {
			rt.log.Error("handler read error",
				"apiKey", hdr.ApiKey, "apiVersion", hdr.ApiVersion, "err", err)
			return
		}

		// ── Build response frame ──────────────────────────────────────────────
		// Response layout:
		//   [frame-length (4)] [correlationId (4)] [?tagged-fields (1)] [body...]
		//
		// The tagged-fields byte is included in the response header for all
		// flexible requests EXCEPT ApiVersions (key 18), which omits it even
		// at v3+ to allow backward-compatible version negotiation.
		resp := codec.NewWriter()
		resp.WriteInt32(hdr.CorrelationID)
		if flexible && hdr.ApiKey != apiKeyApiVersions {
			resp.WriteTaggedFields() // response header v1: empty tagged fields
		}
		resp.WriteRaw(bodyWriter.Bytes())

		if err := codec.WriteFrame(conn, resp.Bytes()); err != nil {
			if !isConnClosedErr(err) {
				rt.log.Error("write error", "remote", addr, "err", err)
			}
			return
		}
	}
}

// isConnClosedErr returns true for errors that indicate a connection was
// closed normally (EOF, "use of closed network connection").
func isConnClosedErr(err error) bool {
	if err == nil {
		return false
	}
	var netErr *net.OpError
	if errors.As(err, &netErr) {
		return errors.Is(netErr.Err, net.ErrClosed)
	}
	// io.EOF arrives when the remote side closes the connection cleanly.
	return errors.Is(err, io.EOF)
}
