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
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"math/rand"
	"net"
	"net/url"
	"slices"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	btpb "cloud.google.com/go/bigtable/apiv2/bigtablepb"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	gtransport "google.golang.org/api/transport/grpc"
	"google.golang.org/grpc/credentials/alts"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"

	btopt "cloud.google.com/go/bigtable/internal/option"

	"google.golang.org/grpc"
)

// A safety net to prevent a connection from draining indefinitely if a stream hangs.
// We cap the max draining timeout to 30mins as there might be a long running stream (such as full table scan).
var maxDrainingTimeout = 30 * time.Minute

const requestParamsHeader = "x-goog-request-params"

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

// WithAppProfile provides the appProfile
func WithAppProfile(appProfile string) BigtableChannelPoolOption {
	return func(p *BigtableChannelPool) {
		p.appProfile = appProfile
	}
}

// WithMeterProvider provides the meter provider for writing metrics
func WithMeterProvider(mp metric.MeterProvider) BigtableChannelPoolOption {
	return func(p *BigtableChannelPool) {
		p.meterProvider = mp
	}
}

// WithLogger provides the logger for logging events
func WithLogger(logger *log.Logger) BigtableChannelPoolOption {
	return func(p *BigtableChannelPool) {
		p.logger = logger
	}
}

// WithInstanceName provides the full instance Name
func WithInstanceName(instanceName string) BigtableChannelPoolOption {
	return func(p *BigtableChannelPool) {
		p.instanceName = instanceName
	}
}

// WithFeatureFlagsMetadata provides the feature flags metadata
func WithFeatureFlagsMetadata(featureFlagsMd metadata.MD) BigtableChannelPoolOption {
	return func(p *BigtableChannelPool) {
		p.featureFlagsMD = featureFlagsMd
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
	return unary + streaming
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

	logger         *log.Logger // logging events
	appProfile     string
	instanceName   string
	featureFlagsMD metadata.MD
	meterProvider  metric.MeterProvider
	// configs
	metricsConfig btopt.MetricsReporterConfig

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

	// Set the selection function based on the strategy
	switch strategy {
	case btopt.LeastInFlight:
		pool.selectFunc = pool.selectLeastLoaded
	case btopt.PowerOfTwoLeastInFlight:
		pool.selectFunc = pool.selectLeastLoadedRandomOfTwo
	default: // RoundRobin is the default
		pool.selectFunc = pool.selectRoundRobin
	}

	var exitSignal error

	initialConns := make([]*connEntry, connPoolSize)
	for i := 0; i < connPoolSize; i++ {
		select {
		case <-pool.poolCtx.Done():
			exitSignal = errors.New("bigtable_connpool: pool context canceled")
		default:
		}

		if exitSignal != nil {
			break
		}

		conn, err := dial()
		if err != nil {
			exitSignal = err
			break
		}

		entry := &connEntry{conn: conn}
		initialConns[i] = entry // Note, we keep non primed conns in conns
		// Prime the new connection in a non-blocking goroutine to warm it up.
		go func(e *connEntry) {
			err := e.conn.Prime(ctx, pool.instanceName, pool.appProfile, pool.featureFlagsMD)
			if err != nil {
				btopt.Debugf(pool.logger, "bigtable_connpool: failed to prime initial connection: %v\n", err)
			}
		}(entry)
	}
	if exitSignal != nil {
		btopt.Debugf(pool.logger, "bigtable_connpool: error during initial connection creation: %v\n", exitSignal)
		// Close populated conns
		for _, entry := range initialConns {
			if entry != nil && entry.conn != nil {
				entry.conn.Close()
			}
		}
		return nil, exitSignal
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
	pool.startMonitors()

	// record the client startup time
	// TODO: currently Prime() is non-blocking, we will make Prime() blocking and infer the transport type here.
	transportType := "unknown"
	pool.recordClientStartUp(clientCreationTimestamp, transportType)

	return pool, nil
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
	newConn, err := p.dial()
	if err != nil {
		btopt.Debugf(p.logger, "bigtable_connpool: Failed to redial connection at index %d: %v\n", idx, err)
		return
	}

	err = newConn.Prime(p.poolCtx, p.instanceName, p.appProfile, p.featureFlagsMD)

	if err != nil {
		btopt.Debugf(p.logger, "bigtable_connpool: Failed to prime replacement connection at index %d: %v. Closing new conn. Old connection remains (draining).\n", idx, err)
		newConn.Close() //
		return          // Abort
	}

	btopt.Debugf(p.logger, "bigtable_connpool: Successfully primed new connection. Replacing connection at index %d\n", idx)
	newEntry := &connEntry{
		conn: newConn,
	}

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

// NewStream selects the least loaded connection and calls NewStream on it.
// This method provides automatic load tracking via a wrapped stream.
func (p *BigtableChannelPool) NewStream(ctx context.Context, desc *grpc.StreamDesc, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	entry, err := p.selectFunc()
	if err != nil {
		return nil, err
	}

	entry.streamingLoad.Add(1)
	stream, err := entry.conn.NewStream(ctx, desc, method, opts...)
	if err != nil {
		entry.errorCount.Add(1)
		entry.streamingLoad.Add(-1) // Decrement immediately on creation failure
		return nil, err
	}

	return &refCountedStream{
		ClientStream: stream,
		entry:        entry, // Store the entry itself
		once:         sync.Once{},
	}, nil
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
		idx1 := rand.Intn(numConns)
		idx2 := rand.Intn(numConns)

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

// refCountedStream wraps a grpc.ClientStream to decrement the load count when the stream is done.
// refCountedStream in this BigtableConnectionPool is to hook into the stream's lifecycle
// to decrement the load counter (s.pool.load[s.connIndex]) when the stream is no longer usable.
// This is primarily detected by errors occurring during SendMsg or RecvMsg (including io.EOF on RecvMsg).

// Another option would have been to use grpc.OnFinish for streams is about the timing of when the load should be considered "finished".
// The grpc.OnFinish callback is executed only when the entire stream is fully closed and the final status is determined.
type refCountedStream struct {
	grpc.ClientStream
	entry *connEntry // Reference to the connection entry
	once  sync.Once
}

// SendMsg calls the embedded stream's SendMsg method.
func (s *refCountedStream) SendMsg(m interface{}) error {
	err := s.ClientStream.SendMsg(m)
	if err != nil {
		s.entry.errorCount.Add(1)
		s.decrementLoad()
	}
	return err
}

// RecvMsg calls the embedded stream's RecvMsg method and decrements load on error.
func (s *refCountedStream) RecvMsg(m interface{}) error {
	err := s.ClientStream.RecvMsg(m)
	if err != nil { // io.EOF is also an error, indicating stream end.
		// io.EOF is a normal stream termination, not an error to be counted.
		if !errors.Is(err, io.EOF) {
			s.entry.errorCount.Add(1)
		}
		s.decrementLoad()
	}
	return err
}

// decrementLoad ensures the load count is decremented exactly once.
func (s *refCountedStream) decrementLoad() {
	s.once.Do(func() {
		s.entry.streamingLoad.Add(-1)
	})
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

			conn, err := p.dial()
			if err != nil {
				btopt.Debugf(p.logger, "bigtable_connpool: Failed to dial new connection for scale up: %v\n", err)
				return
			}

			err = conn.Prime(p.poolCtx, p.instanceName, p.appProfile, p.featureFlagsMD)
			if err != nil {
				btopt.Debugf(p.logger, "bigtable_connpool: Failed to prime new connection: %v. Connection will not be added.\n", err)
				conn.Close()
				return
			}

			results <- &connEntry{conn: conn}
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
