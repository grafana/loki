// Copyright 2025 Google LLC
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

package internal

import (
	"context"
	"fmt"
	"log"
	"math"
	"math/rand/v2"
	"net"
	"net/url"
	"slices"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	btpb "cloud.google.com/go/bigtable/apiv2/bigtablepb"
	"github.com/googleapis/gax-go/v2"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"golang.org/x/sync/errgroup"
	gtransport "google.golang.org/api/transport/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/alts"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	btopt "cloud.google.com/go/bigtable/internal/option"

	"google.golang.org/grpc"
)

// A safety net to prevent a connection from draining indefinitely if a stream hangs.
// We cap the max draining timeout to 30mins as there might be a long running stream (such as full table scan).
var maxDrainingTimeout = 30 * time.Minute

const (
	artificialLoadIfError        = 10
	artificialLoadPenalizedTimer = 5 * time.Second
	requestParamsHeader          = "x-goog-request-params"
	// maxPrimeWorkers caps the goroutines used to prime initial pool
	// connections in parallel. Pools smaller than this naturally fan out to
	// connPoolSize workers; larger pools cap here so we don't spawn one
	// dial+Prime goroutine per connection.
	maxPrimeWorkers = 10
)

// ipProtocol represents the type of IP protocol used.
type ipProtocol int32

const (
	// unknown represents an unknown or undetermined IP protocol.
	unknown ipProtocol = iota - 1
	// ipv6 represents the IPv4 protocol.
	ipv4
	// ipv6 represents the IPv6 protocol.
	ipv6
)

// AddressType returns the string representation of the IPProtocol.
func (ip ipProtocol) addressType() string {
	switch ip {
	case ipv4:
		return "ipv4"
	case ipv6:
		return "ipv6"
	default:
		return "unknown"
	}
}

// BigtableChannelPoolOption options for configurable
type BigtableChannelPoolOption func(*BigtableChannelPool)

// connPoolStatsSupplier callback  that returns a snapshot of connection pool statistics.
type connPoolStatsSupplier func() []connPoolStats

// connPoolStats holds a snapshot of statistics for a single connection.
type connPoolStats struct {
	OutstandingUnaryLoad     int32
	OutstandingStreamingLoad int32
	ErrorCount               int64
	IsALTSUsed               bool
	LBPolicy                 string
}

var _ Monitor = (*MetricsReporter)(nil)

// WithMeterProvider provides the meter provider for writing metrics
func WithMeterProvider(mp metric.MeterProvider) BigtableChannelPoolOption {
	return func(p *BigtableChannelPool) {
		p.meterProvider = mp
	}
}

// WithDirectAccessChecker plugs in the strategy used to decide whether Direct
// Access (DirectPath / DirectPathXds) is compatible at startup. Today the
// classic channel pool factory wires up a PingAndWarm-based checker; a future
// session-pool factory will wire up a GetClientConfiguration-based checker.
// Required: NewBigtableChannelPool refuses construction if no checker is
// supplied. Callers that want Direct Access off should pass the disabled
// stub (newDisabledDirectAccessChecker) so the direct_access/compatible
// metric still surfaces the off state.
func WithDirectAccessChecker(checker DirectAccessChecker) BigtableChannelPoolOption {
	return func(p *BigtableChannelPool) {
		p.directAccessChecker = checker
	}
}

// WithChannelPrimer plugs in the strategy used to warm freshly-dialed
// channels before they enter rotation. Optional: when no primer is supplied,
// the pool's connection factory dials the channel and returns it without
// issuing any prime RPC. The classic channel pool factory wires up a
// PingAndWarm-based primer; alternative pool factories can swap in a
// different strategy (e.g. session-based) or pass nothing at all.
func WithChannelPrimer(primer ChannelPrimer) BigtableChannelPoolOption {
	return func(p *BigtableChannelPool) {
		p.channelPrimer = primer
	}
}

// WithLogger provides the logger for logging events
func WithLogger(logger *log.Logger) BigtableChannelPoolOption {
	return func(p *BigtableChannelPool) {
		p.logger = logger
	}
}

const (
	primeRPCTimeout = 10 * time.Second
)

var errNoConnections = fmt.Errorf("bigtable_connpool: no connections available in the pool")
var _ gtransport.ConnPool = &BigtableChannelPool{}

// BigtableConn wraps grpc.ClientConn to add Bigtable specific methods.
type BigtableConn struct {
	*grpc.ClientConn
	isALTSConn atomic.Bool
	createdAt  atomic.Int64
	// remoteAddrType stores the  type: -1 (unknown/nil), 0 (ipv4), 1 (ipv6)
	remoteAddrType atomic.Int32
}

// ipProtocol returns the IP protocol as a string: "ipv4", "ipv6", or "unknown".
func (bc *BigtableConn) ipProtocol() string {
	return ipProtocol(bc.remoteAddrType.Load()).addressType()
}

// Prime sends a PingAndWarm request to warm up the connection.
func (bc *BigtableConn) Prime(ctx context.Context, fullInstanceName, appProfileID string, featureFlagsMd metadata.MD) error {
	client := btpb.NewBigtableClient(bc.ClientConn)
	req := &btpb.PingAndWarmRequest{
		Name:         fullInstanceName,
		AppProfileId: appProfileID,
	}

	requestParamsMD := metadata.Pairs(requestParamsHeader,
		fmt.Sprintf("name=%s&app_profile_id=%s", url.QueryEscape(fullInstanceName), url.QueryEscape(appProfileID)))

	originalContextMd, _ := metadata.FromOutgoingContext(ctx)
	ctx = metadata.NewOutgoingContext(ctx, metadata.Join(originalContextMd, requestParamsMD, featureFlagsMd))

	// Use a timeout for the prime operation
	primeCtx, cancel := context.WithTimeout(ctx, primeRPCTimeout)
	defer cancel()

	var p peer.Peer
	_, err := client.PingAndWarm(primeCtx, req, grpc.Peer(&p))
	if err != nil {
		return err
	}

	// ip protocol will be -1 if it addr is nil/default, 0 is ipv4 and 1 if ipv6.
	if p.Addr != nil {
		if tcpAddr, ok := p.Addr.(*net.TCPAddr); ok {
			if tcpAddr.IP != nil {
				if tcpAddr.IP.To4() != nil {
					bc.remoteAddrType.Store(int32(ipv4))
				} else {
					bc.remoteAddrType.Store(int32(ipv6))
				}
			}
		}
	}

	if p.AuthInfo != nil {
		if _, ok := p.AuthInfo.(alts.AuthInfo); ok {
			bc.isALTSConn.Store(true)
		}
	}
	return nil
}

// connPoolStatsSupplier returns a snapshot of the current connection pool statistics.
func (p *BigtableChannelPool) connPoolStatsSupplier() []connPoolStats {
	conns := p.getConns()
	if len(conns) == 0 {
		return nil
	}

	stats := make([]connPoolStats, len(conns))
	lbPolicy := p.strategy.String()

	for i, entry := range conns {
		stats[i] = connPoolStats{
			OutstandingUnaryLoad:     entry.unaryLoad.Load(),
			OutstandingStreamingLoad: entry.streamingLoad.Load(),
			ErrorCount:               entry.errorCount.Swap(0),
			IsALTSUsed:               entry.isALTSUsed(),
			LBPolicy:                 lbPolicy,
		}
	}
	return stats
}

// NewBigtableConn creates a wrapped grpc Client Conn
func NewBigtableConn(conn *grpc.ClientConn) *BigtableConn {
	bc := &BigtableConn{
		ClientConn: conn,
	}
	bc.createdAt.Store(time.Now().UnixMilli())
	bc.remoteAddrType.Store(int32(unknown))
	return bc
}

// createdAt returns the creation time of the connection in int64. milliseconds since epoch
func (bc *BigtableConn) creationTime() int64 {
	return bc.createdAt.Load()
}

// connEntry represents a single connection in the pool.
type connEntry struct {
	conn          *BigtableConn
	unaryLoad     atomic.Int32 // In-flight unary requests
	streamingLoad atomic.Int32 // In-flight streaming requests
	errorCount    atomic.Int64 // Errors since the last metric report
	drainingState atomic.Bool  // True if the connection is being gracefully drained.
	penaltyExpiry atomic.Int64 // penaltyExpiry stores the UnixNano timestamp of when the penalty ends

}

// isALTSUsed reports whether the connection is using ALTS aka Direct Access.
// best effort basis
func (e *connEntry) isALTSUsed() bool {
	if e.conn == nil {
		return false
	}
	return e.conn.isALTSConn.Load()
}

// createdAt returns the creation time of the connection in the entry.
// It returns the zero if conn is nil.
func (e *connEntry) createdAt() int64 {
	if e.conn == nil {
		return 0
	}
	return e.conn.creationTime()
}

// applyErrorPenalty checks if the error warrants a load balancing penalty,
// and if so, sets an expiration time for the artificial load.
func (e *connEntry) applyErrorPenalty(err error) {
	if err == nil {
		return
	}

	code := status.Code(err)

	// Penalize errors that typically indicate target-specific health or capacity issues.
	if code == codes.Unavailable ||
		code == codes.ResourceExhausted ||
		code == codes.Internal {
		// A simple Store is safe here; concurrent updates is fine here.
		newExpiry := time.Now().Add(artificialLoadPenalizedTimer).UnixNano()
		e.penaltyExpiry.Store(newExpiry)
	}
}

// isDraining atomically checks if the connection is in the draining state.
func (e *connEntry) isDraining() bool {
	return e.drainingState.Load()
}

// markAsDraining atomically sets the connection's state to draining.
// It returns true if it successfully marked it, false if it was already marked.
func (e *connEntry) markAsDraining() bool {
	return e.drainingState.CompareAndSwap(false, true)
}

// waitForDrainAndClose waits for a connection's in-flight request count to drop to zero
// before closing it. It runs in a separate goroutine.
func (p *BigtableChannelPool) waitForDrainAndClose(entry *connEntry) {
	// Create a context with a drain timeout
	ctx, cancel := context.WithTimeout(p.poolCtx, maxDrainingTimeout)
	defer cancel()

	ticker := time.NewTicker(250 * time.Millisecond) // 250ms tick
	defer ticker.Stop()

	btopt.Debugf(p.logger, "bigtable_connpool: Connection is draining, waiting for load to become 0.")

	for {
		select {
		case <-ticker.C:
			if entry.calculateConnLoad() == 0 {
				btopt.Debugf(p.logger, "bigtable_connpool: Draining connection is idle, closing now.")
				entry.conn.Close()
				return
			}
		case <-ctx.Done():
			btopt.Debugf(p.logger, "bigtable_connpool: Draining connection timed out after %v with load %d. Force closing.", maxDrainingTimeout, entry.calculateConnLoad())
			entry.conn.Close()
			return
		}
	}
}

func (e *connEntry) calculateConnLoad() int32 {
	unary := e.unaryLoad.Load()
	streaming := e.streamingLoad.Load()
	load := unary + streaming

	expiry := e.penaltyExpiry.Load()
	if expiry > 0 {
		if time.Now().UnixNano() < expiry {
			load += artificialLoadIfError // Apply the artificial penalty weight
		} else {
			// restore to zero
			e.penaltyExpiry.CompareAndSwap(expiry, 0)
		}
	}
	return load
}

// BigtableChannelPool implements ConnPool and routes requests to the connection
// pool according to load balancing strategy.
type BigtableChannelPool struct {
	conns atomic.Pointer[[]*connEntry] // Stores []*connEntry

	dial       func() (*BigtableConn, error)
	strategy   btopt.LoadBalancingStrategy
	rrIndex    uint64                     // For round-robin selection
	selectFunc func() (*connEntry, error) // returns *connEntry

	dialMu sync.Mutex // Serializes dial/replace/resize operations

	poolCtx    context.Context    // Context for the pool's background tasks
	poolCancel context.CancelFunc // Function to cancel the poolCtx

	logger *log.Logger // logging events

	factory *connectionFactory // Use the factory for connection creation

	meterProvider metric.MeterProvider
	// configs
	metricsConfig btopt.MetricsReporterConfig

	// directAccessChecker is the pluggable Direct Access compatibility
	// strategy. Required (NewBigtableChannelPool refuses construction
	// without one): CheckCompatibility runs at startup and may switch the
	// connection factory to the direct-access dialer. Callers that want
	// Direct Access off pass the disabled stub so the
	// direct_access/compatible metric still surfaces the off state.
	directAccessChecker DirectAccessChecker

	// channelPrimer is the pluggable strategy used to warm freshly-dialed
	// channels. Optional: when nil, the connection factory skips priming
	// entirely and hands the raw connection straight to the pool. The
	// classic channel pool factory wires up a PingAndWarm-based primer; the
	// future session-pool factory may skip it.
	channelPrimer ChannelPrimer

	// background monitors
	monitors []Monitor
}

// WithMetricsReporterConfig attaches the relevant config for exporting the metrics
func WithMetricsReporterConfig(config btopt.MetricsReporterConfig) BigtableChannelPoolOption {
	return func(p *BigtableChannelPool) { p.metricsConfig = config }
}

// getConns safely loads the current slice of connections.
func (p *BigtableChannelPool) getConns() []*connEntry {
	connsPtr := p.conns.Load()
	if connsPtr == nil {
		return nil
	}
	return *connsPtr
}

// NewBigtableChannelPool creates a pool of connPoolSize and takes the dial func()
// NewBigtableChannelPool primes the new connection in a non-blocking goroutine to warm it up.
// We keep it consistent with the current channelpool behavior which is lazily initialized.
func NewBigtableChannelPool(ctx context.Context, connPoolSize int, strategy btopt.LoadBalancingStrategy, dial func() (*BigtableConn, error), clientCreationTimestamp time.Time, opts ...BigtableChannelPoolOption) (*BigtableChannelPool, error) {
	if connPoolSize <= 0 {
		return nil, fmt.Errorf("bigtable_connpool: connPoolSize must be positive")
	}

	if dial == nil {
		return nil, fmt.Errorf("bigtable_connpool: dial function cannot be nil")
	}
	poolCtx, poolCancel := context.WithCancel(ctx)

	pool := &BigtableChannelPool{
		dial:       dial,
		strategy:   strategy,
		rrIndex:    0,
		poolCtx:    poolCtx,
		poolCancel: poolCancel,
	}

	for _, opt := range opts {
		opt(pool)
	}

	if pool.directAccessChecker == nil {
		poolCancel()
		return nil, fmt.Errorf("bigtable_connpool: DirectAccessChecker is required (use WithDirectAccessChecker)")
	}

	// Default to the standard dialer. The Direct Access checker may swap the
	// dialer for the direct-access equivalent after a successful compatibility
	// probe. The ChannelPrimer (if any) is the single source of priming
	// behavior — both the direct-access and standard-path factories run
	// fresh connections through it before they enter rotation.
	factoryDial := dial

	var firstConn *BigtableConn

	directAccessConn, isDirectAccess := pool.directAccessChecker.CheckCompatibility(pool.poolCtx)
	if isDirectAccess {
		btopt.Debugf(pool.logger, "bigtable_connpool: Direct Access is available. Using Direct Access now.")
		factoryDial = pool.directAccessChecker.Dialer()
		firstConn = directAccessConn
	} else {
		if directAccessConn != nil {
			btopt.Debugf(pool.logger, "bigtable_connpool: Closing probe connection (Direct Access unavailable).")
			directAccessConn.Close()
		}
		btopt.Debugf(pool.logger, "bigtable_connpool: Direct Access is not available. Using standard path.")
	}

	// Initialize the connectionFactory
	pool.factory = &connectionFactory{
		dial:   factoryDial,
		primer: pool.channelPrimer,
		logger: pool.logger,
	}

	// Set the selection function based on the strategy
	switch strategy {
	case btopt.LeastInFlight:
		pool.selectFunc = pool.selectLeastLoaded
	case btopt.PowerOfTwoLeastInFlight:
		pool.selectFunc = pool.selectLeastLoadedRandomOfTwo
	default: // RoundRobin is the default
		pool.selectFunc = pool.selectRoundRobin
	}

	btopt.Debugf(pool.logger, "bigtable_connpool: Creating conn pool with %d connections", connPoolSize)
	// TODO: Replace this logic with addConnections(...).
	initialConns := make([]*connEntry, connPoolSize)
	primeStart := 0
	if firstConn != nil {
		initialConns[0] = &connEntry{conn: firstConn}
		primeStart = 1
	}

	if err := pool.primeInitialConns(pool.poolCtx, initialConns, primeStart); err != nil {
		btopt.Debugf(pool.logger, "bigtable_connpool: error during initial connection creation: %v\n", err)
		for _, entry := range initialConns {
			if entry != nil && entry.conn != nil {
				entry.conn.Close()
			}
		}
		return nil, err
	}

	pool.conns.Store(&initialConns)

	btopt.Debugf(pool.logger, "bigtable_connpool: using load balancing strategy: %s\n", strategy)

	metricsReporter, err := NewMetricsReporter(pool.metricsConfig, pool.connPoolStatsSupplier, pool.logger, pool.meterProvider)
	if err == nil {
		// ignore
		pool.monitors = append(pool.monitors, metricsReporter)
	} else {
		btopt.Debugf(pool.logger, "bigtable_connpool: failed to create metrics reporter: %v\n", err)
	}

	// Initialize and register the Pacemaker
	pacemaker := NewPacemaker(pool.meterProvider, pool.logger)
	pool.monitors = append(pool.monitors, pacemaker)

	pool.startMonitors()

	// record the client startup time
	// TODO: currently Prime() is non-blocking, we will make Prime() blocking and infer the transport type here.
	transportType := "unknown"
	pool.recordClientStartUp(clientCreationTimestamp, transportType)

	return pool, nil
}

// primeInitialConns dials and primes the connections at indices [primeStart, len(out))
// in parallel, capped at maxPrimeWorkers. Successful entries are written into out at
// their target index; if any prime fails, the first error is returned and the caller is
// responsible for closing any populated entries.
//
// ctx scopes the prime operations: the first worker error (or ctx cancellation) tears
// down the rest via errgroup's derived context.
func (p *BigtableChannelPool) primeInitialConns(ctx context.Context, out []*connEntry, primeStart int) error {
	jobs := len(out) - primeStart
	if jobs <= 0 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("bigtable_connpool: pool context canceled: %w", err)
	}

	workers := jobs
	if workers > maxPrimeWorkers {
		workers = maxPrimeWorkers
	}

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(workers)
	for i := primeStart; i < len(out); i++ {
		idx := i
		g.Go(func() error {
			entry, err := p.factory.newEntry(gctx)
			if err != nil {
				return err
			}
			out[idx] = entry
			return nil
		})
	}
	return g.Wait()
}

func (p *BigtableChannelPool) recordClientStartUp(clientCreationTimestamp time.Time, transportType string) {
	if p.meterProvider == nil {
		return
	}

	meter := p.meterProvider.Meter(clientMeterName)
	// Define buckets for startup latency (in milliseconds)
	bucketBounds := []float64{0, 10, 50, 100, 300, 500, 1000, 2000, 5000, 10000, 20000}
	clientStartupTime, err := meter.Float64Histogram(
		"startup_time",
		metric.WithDescription("Total time for completion of logic of NewClientWithConfig"),
		metric.WithUnit("ms"),
		metric.WithExplicitBucketBoundaries(bucketBounds...),
	)

	if err == nil {
		elapsedTime := float64(time.Since(clientCreationTimestamp).Milliseconds())
		clientStartupTime.Record(p.poolCtx, elapsedTime, metric.WithAttributes(
			attribute.String("transport_type", transportType),
			attribute.String("status", "OK"),
		))
	}
}

func (p *BigtableChannelPool) startMonitors() {
	for _, m := range p.monitors {
		btopt.Debugf(p.logger, "bigtable_connpool: Starting monitor %T\n", m)
		m.Start(p.poolCtx)
	}
}

// Num returns the number of connections in the pool.
func (p *BigtableChannelPool) Num() int {
	return len(p.getConns())
}

// Close closes all connections in the pool.
func (p *BigtableChannelPool) Close() error {
	p.poolCancel() // Cancel the context for background tasks
	// Stop all monitors.
	for _, m := range p.monitors {
		m.Stop()
	}
	conns := p.getConns()
	var errs multiError

	// immediately store zero-length slice
	p.conns.Store((&[]*connEntry{}))

	for _, entry := range conns {
		if err := entry.conn.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return errs
}

// replaceConnection closes the connection for the oldEntry
func (p *BigtableChannelPool) replaceConnection(oldEntry *connEntry) {
	p.dialMu.Lock() // Serialize replacements
	defer p.dialMu.Unlock()

	// Mark the connection
	// if it is marked,
	// it means another routine (health eviction or dynamic scale down) took over it.
	if !oldEntry.markAsDraining() {
		return
	}

	currentConns := p.getConns()
	idx := slices.Index(currentConns, oldEntry)

	// If the connection isn't in the slice, it was already removed.
	// The drain process should still be kicked off.
	if idx == -1 {
		btopt.Debugf(p.logger, "bigtable_connpool: Connection to replace was already removed. Draining it.")
		// thread safe to call waitForDrainAndClose as conn.Close() can be called multiple times.
		go p.waitForDrainAndClose(oldEntry)
		return
	}
	// Simple eviction logic.
	btopt.Debugf(p.logger, "bigtable_connpool: Evicting connection at index %d\n", idx)
	select {
	case <-p.poolCtx.Done():
		btopt.Debugf(p.logger, "bigtable_connpool: Pool context done, skipping redial: %v\n", p.poolCtx.Err())
		return
	default:
	}
	newEntry, err := p.factory.newEntry(p.poolCtx)
	if err != nil {
		btopt.Debugf(p.logger, "bigtable_connpool: Failed to replace connection at index %d: %v. Closing new conn. Old connection remains (draining).\n", idx, err)
		return
	}

	btopt.Debugf(p.logger, "bigtable_connpool: Successfully primed new connection. Replacing connection at index %d\n", idx)
	// Copy-on-write
	newConns := make([]*connEntry, len(currentConns))
	copy(newConns, currentConns)
	newConns[idx] = newEntry
	p.conns.Store(&newConns)
	// Start the graceful shutdown process for the old connection
	go p.waitForDrainAndClose(oldEntry)
}

// Invoke selects the least loaded connection and calls Invoke on it.
// This method provides automatic load tracking.
// Load is tracked as a unary call.
func (p *BigtableChannelPool) Invoke(ctx context.Context, method string, args interface{}, reply interface{}, opts ...grpc.CallOption) error {
	entry, err := p.selectFunc()
	if err != nil {
		return err
	}
	entry.unaryLoad.Add(1)
	defer entry.unaryLoad.Add(-1)

	err = entry.conn.Invoke(ctx, method, args, reply, opts...)
	if err != nil {
		entry.errorCount.Add(1)
		entry.applyErrorPenalty(err) // Apply penalty on error
	}
	return err

}

// Conn provides connbased on selectfunc()
func (p *BigtableChannelPool) Conn() *grpc.ClientConn {
	bigtableConn := p.getBigtableConn()
	if bigtableConn == nil {
		return nil
	}
	return bigtableConn.ClientConn
}

func (p *BigtableChannelPool) getBigtableConn() *BigtableConn {
	entry, err := p.selectFunc()
	if err != nil {
		return nil
	}
	return entry.conn
}

// NewStream selects a connection by the configured load-balancing strategy
// and opens a stream on it. grpc.OnFinish fires exactly once for any stream
// that was successfully created (normal completion, context cancellation,
// transport teardown), so it is the single source of truth for both load
// accounting and per-stream error attribution — no need to wrap the
// returned ClientStream.
func (p *BigtableChannelPool) NewStream(ctx context.Context, desc *grpc.StreamDesc, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	entry, err := p.selectFunc()
	if err != nil {
		return nil, err
	}

	entry.streamingLoad.Add(1)

	onFinish := grpc.OnFinish(func(err error) {
		if err != nil {
			entry.errorCount.Add(1)
			entry.applyErrorPenalty(err)
		}
		entry.streamingLoad.Add(-1)
	})
	// Prepend onto a fresh slice so we never write into spare capacity of
	// the caller's opts (which would race with concurrent NewStream calls
	// that share the same backing array).
	opts = append([]grpc.CallOption{onFinish}, opts...)

	stream, err := entry.conn.NewStream(ctx, desc, method, opts...)
	if err != nil {
		entry.errorCount.Add(1)
		entry.applyErrorPenalty(err)
		entry.streamingLoad.Add(-1) // Decrement immediately on creation failure
		return nil, err
	}

	return stream, nil
}

// selectLeastLoadedRandomOfTwo() returns the index of the connection via random of two
func (p *BigtableChannelPool) selectLeastLoadedRandomOfTwo() (*connEntry, error) {
	conns := p.getConns()
	numConns := len(conns)
	if numConns == 0 {
		return nil, errNoConnections
	}
	if numConns == 1 {
		if conns[0].isDraining() {
			return nil, errNoConnections
		}
		return conns[0], nil
	}

	// Retry numConns * 2 times in worst case.
	for i := 0; i < numConns*2 && numConns > 1; i++ {
		idx1 := rand.IntN(numConns)
		idx2 := rand.IntN(numConns)

		entry1 := conns[idx1]
		entry2 := conns[idx2]

		if entry1.isDraining() || entry2.isDraining() {
			continue // Find another pair
		}

		if idx1 == idx2 {
			return entry1, nil // Both random choices were the same and it's not draining
		}

		load1 := entry1.calculateConnLoad()
		load2 := entry2.calculateConnLoad()
		if load1 <= load2 {
			return entry1, nil
		}
		return entry2, nil
	}
	//  Fallback to finding any active connection if the random strategy fails.,
	return p.selectLeastLoaded()
}

func (p *BigtableChannelPool) selectRoundRobin() (*connEntry, error) {
	conns := p.getConns()
	numConns := len(conns)
	if numConns == 0 {
		return nil, errNoConnections
	}
	// Add a retry loop to handle draining connections.
	// We iterate at most numConns times to prevent an infinite loop if all connections are draining.
	for i := 0; i < numConns; i++ {
		nextIndex := atomic.AddUint64(&p.rrIndex, 1) - 1
		entry := conns[int(nextIndex%uint64(numConns))]
		if !entry.isDraining() {
			return entry, nil
		}
	}

	return nil, errNoConnections // All connections we checked are draining
}

// selectLeastLoaded returns the index of the connection with the minimum load.
func (p *BigtableChannelPool) selectLeastLoaded() (*connEntry, error) {
	conns := p.getConns()
	numConns := len(conns)
	if numConns == 0 {
		return nil, errNoConnections
	}

	minIndex := -1
	minLoad := int32(math.MaxInt32)

	for i, entry := range conns {
		if entry.isDraining() {
			continue
		}
		currentLoad := entry.calculateConnLoad()
		if currentLoad < minLoad {
			minLoad = currentLoad
			minIndex = i
		}
	}
	if minIndex == -1 {
		return nil, errNoConnections // All connections are draining
	}
	return conns[minIndex], nil
}

// addConnections returns true if the pool size changed.
// TODO: addConnections has a long section where we dial and prime the connections.
// Currently, we are taking dialMu() throughout the section and dialMu() is also required for
// replaceConnection().
//
//	Note that DynamicScaleMonitor allows only one evaluateAndScale as it takes a mutex
//	during evaluateAndScale so don't expect any size changes in conns
func (p *BigtableChannelPool) addConnections(increaseDelta, maxConns int) bool {
	// dialMu access
	p.dialMu.Lock()
	defer p.dialMu.Unlock()
	numCurrent := p.Num()
	currentConns := p.getConns()
	maxDelta := maxConns - numCurrent
	cappedIncrease := min(increaseDelta, maxDelta)

	if cappedIncrease <= 0 {
		return false
	}

	// LONG SECTION<START>
	// This section can take time as it involves creating conn and Prime()
	// TODO(): Avoid taking dialMu here.
	results := make(chan *connEntry, cappedIncrease)
	var wg sync.WaitGroup

	for i := 0; i < cappedIncrease; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			select {
			case <-p.poolCtx.Done():
				btopt.Debugf(p.logger, "bigtable_connpool: Context done, skipping connection creation: %v\n", p.poolCtx.Err())
				return
			default:
			}

			entry, err := p.factory.newEntry(p.poolCtx)
			if err != nil {
				btopt.Debugf(p.logger, "bigtable_connpool: Failed to add new connection: %v. Connection will not be added.\n", err)
				return
			}

			results <- entry
		}()
	}
	// Goroutine to close the results channel once all workers are done.
	go func() {
		wg.Wait()
		close(results)
	}()

	newEntries := make([]*connEntry, 0, cappedIncrease)
	for entry := range results {
		newEntries = append(newEntries, entry)
	}

	if len(newEntries) == 0 {
		btopt.Debugf(p.logger, "bigtable_connpool: No new connections were successfully created and primed.\n")
		return false
	}

	// LONG SECTION<END>

	// add now
	combinedConns := make([]*connEntry, numCurrent+len(newEntries))
	copy(combinedConns, currentConns)
	copy(combinedConns[numCurrent:], newEntries)
	p.conns.Store(&combinedConns)

	btopt.Debugf(p.logger, "bigtable_connpool: Added %d connections, new size: %d\n", numCurrent+len(newEntries), len(combinedConns))
	return true
}

type entryWithAge struct {
	entry     *connEntry
	createdAt int64
}

// removeConnections returns true if the pool size changed. It removes the oldest connections available in the conns.
func (p *BigtableChannelPool) removeConnections(decreaseDelta, minConns, maxRemoveConns int) bool {
	// the critical section is very short
	// as we just need to sort the conns and get rid of n old connections.
	p.dialMu.Lock()

	if decreaseDelta <= 0 {
		p.dialMu.Unlock()
		return false
	}
	snapshotConns := p.getConns()
	numSnapshot := len(snapshotConns)

	if numSnapshot <= minConns {
		p.dialMu.Unlock()
		btopt.Debugf(p.logger, "bigtable_connpool: Removal skippped, current size %d <= minConns %d\n", numSnapshot, minConns)
		return false
	}

	// the max we can decrease is min(maxRemoveConns, min(decreaseDelta, numSnapshot - minConns))
	cappedDecrease := min(maxRemoveConns, min(decreaseDelta, numSnapshot-minConns))

	if cappedDecrease <= 0 {
		p.dialMu.Unlock()
		return false
	}

	entries := make([]entryWithAge, 0, numSnapshot)
	for _, entry := range snapshotConns {
		// Only consider connections not *already* draining for removal via this logic.
		if !entry.isDraining() {
			entries = append(entries, entryWithAge{entry: entry, createdAt: entry.conn.creationTime()})
		}
	}

	// Sort by creation time, oldest first
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].entry.createdAt() < entries[j].entry.createdAt()
	})

	// Select the oldest non-draining connections to mark for draining.
	connsToDrain := make([]*connEntry, 0, cappedDecrease)
	for i := 0; i < cappedDecrease; i++ {
		connsToDrain = append(connsToDrain, entries[i].entry)
		entries[i].entry.markAsDraining()
	}

	// Build the slice of connections to keep
	// maintains all connections from the snapshot EXCEPT the ones we just
	// explicitly marked for removal/draining in this method.
	connsToKeep := make([]*connEntry, 0, numSnapshot-cappedDecrease)
	for _, entry := range snapshotConns {
		if !entry.isDraining() {
			connsToKeep = append(connsToKeep, entry)
		}
	}

	p.conns.Store(&connsToKeep) // new slice
	// Release the lock
	p.dialMu.Unlock()

	btopt.Debugf(p.logger, "bigtable_connpool: Marked %d oldest connections for draining, new pool size: %d\n", len(connsToDrain), len(connsToKeep))
	// Initiate graceful shutdown for the connections in connsToDrain.
	for _, entry := range connsToDrain {
		go p.waitForDrainAndClose(entry)
	}
	return len(connsToDrain) > 0

}

// connectionFactory is responsible for creating and (optionally) priming
// new Bigtable connections. When primer is nil the factory dials and
// returns the connection without warming it.
type connectionFactory struct {
	dial   func() (*BigtableConn, error)
	primer ChannelPrimer
	logger *log.Logger
}

// newEntry creates a new connection, primes it (if a primer is configured),
// and returns it as a connEntry. Blocks until the connection is ready, or
// returns an error.
func (cf *connectionFactory) newEntry(ctx context.Context) (*connEntry, error) {
	conn, err := cf.dial()
	if err != nil {
		return nil, fmt.Errorf("factory dial failed: %w", err)
	}

	if err := cf.primeWithRetry(ctx, conn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("bigtable_connpool:  connection factory prime failed: %w", err)
	}

	return &connEntry{conn: conn}, nil
}

// primeWithRetry runs the configured ChannelPrimer with exponential backoff.
// Returns nil immediately when no primer is configured, so the pool can be
// used without priming.
func (cf *connectionFactory) primeWithRetry(ctx context.Context, conn *BigtableConn) error {
	if cf.primer == nil {
		return nil
	}
	backoffPolicy := gax.Backoff{
		Initial:    100 * time.Millisecond,
		Max:        2 * time.Second,
		Multiplier: 1.2,
	}
	maxAttempts := 3
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {

		// ctx.Done() returns a error
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("bigtable_connpool:  error before prime attempt %d: %w", attempt, err)
		}

		lastErr = cf.primer.Prime(ctx, conn)
		if lastErr == nil {
			return nil
		}

		if attempt == maxAttempts-1 {
			// no need to pause(), short circuit
			break
		}

		pause := backoffPolicy.Pause()
		btopt.Debugf(cf.logger, "bigtable_connpool: Prime failed with  error on attempt %d, retrying in %v: %v", attempt+1, pause, lastErr)

		select {
		case <-ctx.Done():
			return fmt.Errorf("context done while backing off for prime: %w", ctx.Err())
		case <-time.After(pause):
		}
	}

	return fmt.Errorf("factory prime failed after %d attempts: %w", maxAttempts, lastErr)

}

type multiError []error

func (m multiError) Error() string {
	s, n := "", 0
	for _, e := range m {
		if e != nil {
			if n == 0 {
				s = e.Error()
			}
			n++
		}
	}
	switch n {
	case 0:
		return "(0 errors)"
	case 1:
		return s
	case 2:
		return s + " (and 1 other error)"
	}
	return fmt.Sprintf("%s (and %d other errors)", s, n-1)
}
