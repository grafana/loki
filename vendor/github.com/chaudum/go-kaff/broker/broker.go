package broker

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"sync"

	"github.com/chaudum/go-kaff/handler"
	"github.com/chaudum/go-kaff/store"
)

// Broker is an in-memory Kafka broker that speaks the Kafka TCP wire protocol.
//
// Typical usage:
//
//	b, err := broker.New(broker.Config{ListenAddr: "127.0.0.1:0"})
//	b.CreateTopic("events", broker.TopicConfig{NumPartitions: 4})
//	b.Start()
//	defer b.Stop()
//	addr := b.Addr() // hand to any Kafka client library
type Broker struct {
	cfg     Config
	store   *store.Store
	log     *slog.Logger
	metrics Metrics
	router  *handler.Router

	mu       sync.Mutex
	listener net.Listener

	cancel  context.CancelFunc
	wg      sync.WaitGroup

	// Active TCP connections; closed on Stop() to unblock blocking reads.
	connsMu sync.Mutex
	conns   map[net.Conn]struct{}
}

// New creates a new Broker with the given configuration.
//
// AutoCreateTopics defaults to true if not explicitly set via the config
// (callers who want to disable it must set AutoCreateTopics: false explicitly
// after calling New and before calling Start, or set it via the Config).
func New(cfg Config) (*Broker, error) {
	// Apply defaults.
	cfg.defaults()

	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}

	metrics := cfg.Metrics
	if metrics == nil {
		metrics = NoopMetrics{}
	}

	s := store.New()

	return &Broker{
		cfg:     cfg,
		store:   s,
		log:     log,
		metrics: metrics,
		conns:   make(map[net.Conn]struct{}),
	}, nil
}

// CreateTopic pre-creates a topic before the broker is started.
// It may also be called after Start to create topics at runtime.
// Returns an error if the topic already exists.
// CreateTopic pre-creates a topic before the broker is started.
// It may also be called after Start to create topics at runtime.
// Returns an error if the topic already exists.
func (b *Broker) CreateTopic(name string, cfg TopicConfig) error {
	n := cfg.NumPartitions
	if n <= 0 {
		n = b.cfg.DefaultNumPartitions
	}
	maxBytes := cfg.RetentionBytes
	if maxBytes == 0 {
		maxBytes = b.cfg.MaxBytesPerPartition
	}
	return b.store.CreateTopic(name, n, maxBytes)
}

// DeleteTopic removes a topic from the broker.  Any long-poll Fetch requests
// watching the topic's partitions are woken and will return
// UNKNOWN_TOPIC_OR_PARTITION.
func (b *Broker) DeleteTopic(name string) error {
	return b.store.DeleteTopic(name)
}

// Start binds the TCP listener and begins accepting connections.
// It returns an error if the address is already in use.
func (b *Broker) Start() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.listener != nil {
		return fmt.Errorf("broker: already started")
	}

	ln, err := net.Listen("tcp", b.cfg.ListenAddr)
	if err != nil {
		return fmt.Errorf("broker: listen %s: %w", b.cfg.ListenAddr, err)
	}
	b.listener = ln

	host, portStr, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		ln.Close()
		return fmt.Errorf("broker: parse addr: %w", err)
	}
	port, _ := strconv.Atoi(portStr)

	info := handler.BrokerInfo{
		NodeID:               0,
		Host:                 host,
		Port:                 int32(port),
		AutoCreateTopics:     b.cfg.AutoCreateTopics,
		DefaultPartitions:    b.cfg.DefaultNumPartitions,
		MaxBytesPerPartition: b.cfg.MaxBytesPerPartition,
	}
	ctx, cancel := context.WithCancel(context.Background())
	b.cancel = cancel

	b.router = handler.NewRouter(ctx, b.store, info, b.log, b.metrics)

	b.wg.Add(1)
	go b.acceptLoop(ctx, ln)

	b.log.Info("broker started", "addr", ln.Addr())
	return nil
}

// Stop cancels the broker context, closes the listener, closes all active TCP
// connections, and waits for all goroutines to exit.
func (b *Broker) Stop() {
	b.mu.Lock()
	ln := b.listener
	cancel := b.cancel
	b.mu.Unlock()

	if ln == nil {
		return
	}

	// 1. Cancel the context: wakes long-poll Fetch/JoinGroup/SyncGroup handlers.
	if cancel != nil {
		cancel()
	}

	// 2. Close the listener: stops accepting new connections.
	ln.Close()

	// 3. Close all active connections: unblocks blocking codec.ReadFrame calls.
	b.connsMu.Lock()
	for c := range b.conns {
		c.Close()
	}
	b.connsMu.Unlock()

	// 4. Wait for all goroutines to finish.
	b.wg.Wait()
	b.log.Info("broker stopped")
}

// Addr returns the actual TCP address the broker is listening on.
// This is useful when ListenAddr uses port 0 (OS-assigned port).
// Addr panics if called before Start.
func (b *Broker) Addr() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.listener == nil {
		panic("broker: Addr called before Start")
	}
	return b.listener.Addr().String()
}

// acceptLoop accepts incoming TCP connections and spawns a goroutine for each.
func (b *Broker) acceptLoop(ctx context.Context, ln net.Listener) {
	defer b.wg.Done()

	for {
		conn, err := ln.Accept()
		if err != nil {
			// Accept fails when the listener is closed (Stop was called).
			select {
			case <-ctx.Done():
				return
			default:
			}
			b.log.Error("accept error", "err", err)
			return
		}

		b.trackConn(conn, true)

		b.wg.Add(1)
		go func(c net.Conn) {
			defer b.wg.Done()
			defer b.trackConn(c, false)
			b.router.ServeConn(c)
		}(conn)
	}
}

// trackConn registers or deregisters an active connection and updates the
// active-connections metric.
func (b *Broker) trackConn(c net.Conn, add bool) {
	b.connsMu.Lock()
	if add {
		b.conns[c] = struct{}{}
	} else {
		delete(b.conns, c)
	}
	n := len(b.conns)
	b.connsMu.Unlock()
	b.metrics.SetActiveConnections(n)
}
