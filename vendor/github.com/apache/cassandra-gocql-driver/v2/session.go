/*
 * Licensed to the Apache Software Foundation (ASF) under one
 * or more contributor license agreements.  See the NOTICE file
 * distributed with this work for additional information
 * regarding copyright ownership.  The ASF licenses this file
 * to you under the Apache License, Version 2.0 (the
 * "License"); you may not use this file except in compliance
 * with the License.  You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
/*
 * Content before git sha 34fdeebefcbf183ed7f916f931aa0586fdaa1b40
 * Copyright (c) 2012, The Gocql authors,
 * provided under the BSD-3-Clause License.
 * See the NOTICE file distributed with this work for additional information.
 */

package gocql

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/apache/cassandra-gocql-driver/v2/internal/lru"
)

// Session is the interface used by users to interact with the database.
//
// It's safe for concurrent use by multiple goroutines and a typical usage
// scenario is to have one global session object to interact with the
// whole Cassandra cluster.
//
// This type extends the Node interface by adding a convenient query builder
// and automatically sets a default consistency level on all operations
// that do not have a consistency level set.
type Session struct {
	cons                Consistency
	pageSize            int
	prefetch            float64
	routingKeyInfoCache routingKeyInfoLRU
	schemaDescriber     *schemaDescriber
	trace               Tracer
	queryObserver       QueryObserver
	batchObserver       BatchObserver
	connectObserver     ConnectObserver
	frameObserver       FrameHeaderObserver
	streamObserver      StreamObserver
	hostSource          *ringDescriber
	ringRefresher       *refreshDebouncer
	stmtsLRU            *preparedLRU
	types               *RegisteredTypes

	connCfg *ConnConfig

	executor *queryExecutor
	pool     *policyConnPool
	policy   HostSelectionPolicy

	ring     ring
	metadata clusterMetadata

	control *controlConn

	// event handlers
	nodeEvents   *eventDebouncer
	schemaEvents *eventDebouncer

	// ring metadata
	useSystemSchema           bool
	hasAggregatesAndFunctions bool

	cfg ClusterConfig

	ctx    context.Context
	cancel context.CancelFunc

	// sessionStateMu protects isClosed and isInitialized.
	sessionStateMu sync.RWMutex
	// isClosed is true once Session.Close is finished.
	isClosed bool
	// isClosing bool is true once Session.Close is started.
	isClosing bool
	// isInitialized is true once Session.init succeeds.
	// you can use initialized() to read the value.
	isInitialized bool

	logger StructuredLogger
}

func addrsToHosts(addrs []string, defaultPort int, logger StructuredLogger) ([]*HostInfo, error) {
	var hosts []*HostInfo
	for _, hostaddr := range addrs {
		resolvedHosts, err := hostInfo(hostaddr, defaultPort)
		if err != nil {
			// Try other hosts if unable to resolve DNS name
			if _, ok := err.(*net.DNSError); ok {
				logger.Error("DNS error.", newLogFieldError("err", err))
				continue
			}
			return nil, err
		}

		hosts = append(hosts, resolvedHosts...)
	}
	if len(hosts) == 0 {
		return nil, errors.New("failed to resolve any of the provided hostnames")
	}
	return hosts, nil
}

// NewSession wraps an existing Node.
func NewSession(cfg ClusterConfig) (*Session, error) {
	// Check that hosts in the ClusterConfig is not empty
	if len(cfg.Hosts) < 1 {
		return nil, ErrNoHosts
	}

	// Check that either Authenticator is set or AuthProvider, not both
	if cfg.Authenticator != nil && cfg.AuthProvider != nil {
		return nil, errors.New("Can't use both Authenticator and AuthProvider in cluster config.")
	}

	if cfg.SerialConsistency > 0 && !cfg.SerialConsistency.isSerial() {
		return nil, fmt.Errorf("the default SerialConsistency level is not allowed to be anything else but SERIAL or LOCAL_SERIAL. Recived value: %v", cfg.SerialConsistency)
	}

	// TODO: we should take a context in here at some point
	ctx, cancel := context.WithCancel(context.TODO())

	s := &Session{
		cons:            cfg.Consistency,
		prefetch:        cfg.NextPagePrefetch,
		cfg:             cfg,
		pageSize:        cfg.PageSize,
		stmtsLRU:        &preparedLRU{lru: lru.New(cfg.MaxPreparedStmts)},
		connectObserver: cfg.ConnectObserver,
		ctx:             ctx,
		cancel:          cancel,
		logger:          cfg.newLogger(),
		trace:           cfg.Tracer,
	}
	if cfg.RegisteredTypes == nil {
		s.types = GlobalTypes.Copy()
	} else {
		s.types = cfg.RegisteredTypes.Copy()
	}

	s.schemaDescriber = newSchemaDescriber(s)

	s.nodeEvents = newEventDebouncer("NodeEvents", s.handleNodeEvent, s.logger)
	s.schemaEvents = newEventDebouncer("SchemaEvents", s.handleSchemaEvent, s.logger)

	s.routingKeyInfoCache.lru = lru.New(cfg.MaxRoutingKeyInfo)

	s.hostSource = &ringDescriber{session: s}
	s.ringRefresher = newRefreshDebouncer(ringRefreshDebounceTime, func() error { return refreshRing(s.hostSource) })

	if cfg.PoolConfig.HostSelectionPolicy == nil {
		cfg.PoolConfig.HostSelectionPolicy = RoundRobinHostPolicy()
	}
	s.pool = cfg.PoolConfig.buildPool(s)

	s.policy = cfg.PoolConfig.HostSelectionPolicy
	s.policy.Init(s)

	s.executor = &queryExecutor{
		pool:   s.pool,
		policy: cfg.PoolConfig.HostSelectionPolicy,
	}

	s.queryObserver = cfg.QueryObserver
	s.batchObserver = cfg.BatchObserver
	s.connectObserver = cfg.ConnectObserver
	s.frameObserver = cfg.FrameHeaderObserver
	s.streamObserver = cfg.StreamObserver

	//Check the TLS Config before trying to connect to anything external
	connCfg, err := connConfig(&s.cfg)
	if err != nil {
		//TODO: Return a typed error
		return nil, fmt.Errorf("gocql: unable to create session: %v", err)
	}
	s.connCfg = connCfg

	if err := s.init(); err != nil {
		s.Close()
		if err == ErrNoConnectionsStarted {
			//This error used to be generated inside NewSession & returned directly
			//Forward it on up to be backwards compatible
			return nil, ErrNoConnectionsStarted
		} else {
			// TODO(zariel): dont wrap this error in fmt.Errorf, return a typed error
			return nil, fmt.Errorf("gocql: unable to create session: %v", err)
		}
	}

	return s, nil
}

func (s *Session) init() error {
	hosts, err := addrsToHosts(s.cfg.Hosts, s.cfg.Port, s.logger)
	if err != nil {
		return err
	}
	s.ring.endpoints = hosts

	if !s.cfg.disableControlConn {
		s.control = createControlConn(s)
		if s.cfg.ProtoVersion == 0 {
			proto, err := s.control.discoverProtocol(hosts)
			if err != nil {
				return fmt.Errorf("unable to discover protocol version: %v", err)
			} else if proto == 0 {
				return errors.New("unable to discovery protocol version")
			}

			// TODO(zariel): we really only need this in 1 place
			s.cfg.ProtoVersion = proto
			s.connCfg.ProtoVersion = proto
			s.logger.Info("Discovered protocol version.", newLogFieldInt("protocol_version", proto))
		}

		if err := s.control.connect(hosts, true); err != nil {
			return err
		}

		if !s.cfg.DisableInitialHostLookup {
			var partitioner string
			newHosts, partitioner, err := s.hostSource.GetHosts()
			if err != nil {
				return err
			}
			s.policy.SetPartitioner(partitioner)
			filteredHosts := make([]*HostInfo, 0, len(newHosts))
			for _, host := range newHosts {
				if !s.cfg.filterHost(host) {
					filteredHosts = append(filteredHosts, host)
				}
			}

			hosts = filteredHosts
			s.logger.Info("Refreshed ring.", newLogFieldString("ring", ringString(hosts)))
		} else {
			s.logger.Info("Not performing a ring refresh because DisableInitialHostLookup is true.")
		}
	}

	for _, host := range hosts {
		// In case when host lookup is disabled and when we are in unit tests,
		// host are not discovered, and we are missing host ID information used
		// by internal logic.
		// Associate random UUIDs here with all hosts missing this information.
		if len(host.HostID()) == 0 {
			host.setHostID(MustRandomUUID().String())
		}
	}

	hostMap := make(map[string]*HostInfo, len(hosts))
	for _, host := range hosts {
		hostMap[host.HostID()] = host
	}

	hosts = hosts[:0]
	// each host will increment left and decrement it after connecting and once
	// there's none left, we'll close hostCh
	var left int64
	// we will receive up to len(hostMap) of messages so create a buffer so we
	// don't end up stuck in a goroutine if we stopped listening
	connectedCh := make(chan struct{}, len(hostMap))
	// we add one here because we don't want to end up closing hostCh until we're
	// done looping and the decerement code might be reached before we've looped
	// again
	atomic.AddInt64(&left, 1)
	for _, host := range hostMap {
		host, exists := s.ring.addOrUpdate(host)
		if s.cfg.filterHost(host) {
			continue
		}
		if !exists {
			s.logger.Info("Adding host (session initialization).",
				newLogFieldIp("host_addr", host.ConnectAddress()), newLogFieldString("host_id", host.HostID()))
		}

		atomic.AddInt64(&left, 1)
		go func() {
			s.pool.addHost(host)
			connectedCh <- struct{}{}

			// if there are no hosts left, then close the hostCh to unblock the loop
			// below if its still waiting
			if atomic.AddInt64(&left, -1) == 0 {
				close(connectedCh)
			}
		}()

		hosts = append(hosts, host)
	}
	// once we're done looping we subtract the one we initially added and check
	// to see if we should close
	if atomic.AddInt64(&left, -1) == 0 {
		close(connectedCh)
	}

	// before waiting for them to connect, add them all to the policy so we can
	// utilize efficiencies by calling AddHosts if the policy supports it
	type bulkAddHosts interface {
		AddHosts([]*HostInfo)
	}
	if v, ok := s.policy.(bulkAddHosts); ok {
		v.AddHosts(hosts)
	} else {
		for _, host := range hosts {
			s.policy.AddHost(host)
		}
	}

	readyPolicy, _ := s.policy.(ReadyPolicy)
	// now loop over connectedCh until it's closed (meaning we've connected to all)
	// or until the policy says we're ready
	for range connectedCh {
		if readyPolicy != nil && readyPolicy.Ready() {
			break
		}
	}

	// TODO(zariel): we probably dont need this any more as we verify that we
	// can connect to one of the endpoints supplied by using the control conn.
	// See if there are any connections in the pool
	if s.cfg.ReconnectInterval > 0 {
		go s.reconnectDownedHosts(s.cfg.ReconnectInterval)
	}

	// If we disable the initial host lookup, we need to still check if the
	// cluster is using the newer system schema or not... however, if control
	// connection is disable, we really have no choice, so we just make our
	// best guess...
	if !s.cfg.disableControlConn && s.cfg.DisableInitialHostLookup {
		newer, _ := checkSystemSchema(s.control)
		s.useSystemSchema = newer
	} else {
		version := s.ring.rrHost().Version()
		s.useSystemSchema = version.AtLeast(3, 0, 0)
		s.hasAggregatesAndFunctions = version.AtLeast(2, 2, 0)
	}

	if s.pool.Size() == 0 {
		return ErrNoConnectionsStarted
	}

	// Invoke KeyspaceChanged to let the policy cache the session keyspace
	// parameters. This is used by tokenAwareHostPolicy to discover replicas.
	if !s.cfg.disableControlConn && s.cfg.Keyspace != "" {
		s.policy.KeyspaceChanged(KeyspaceUpdateEvent{Keyspace: s.cfg.Keyspace})
	}

	s.sessionStateMu.Lock()
	s.isInitialized = true
	s.sessionStateMu.Unlock()

	s.logger.Info("Session initialized successfully.")
	return nil
}

// AwaitSchemaAgreement will wait until schema versions across all nodes in the
// cluster are the same (as seen from the point of view of the control connection).
// The maximum amount of time this takes is governed
// by the MaxWaitSchemaAgreement setting in the configuration (default: 60s).
// AwaitSchemaAgreement returns an error in case schema versions are not the same
// after the timeout specified in MaxWaitSchemaAgreement elapses.
func (s *Session) AwaitSchemaAgreement(ctx context.Context) error {
	if s.cfg.disableControlConn {
		return errNoControl
	}
	return s.control.withConn(func(conn *Conn) *Iter {
		return newErrIter(conn.awaitSchemaAgreement(ctx), &queryMetrics{}, "", nil, nil)
	}).err
}

func (s *Session) reconnectDownedHosts(intv time.Duration) {
	reconnectTicker := time.NewTicker(intv)
	defer reconnectTicker.Stop()

	for {
		select {
		case <-reconnectTicker.C:
			s.logger.Debug("Connecting to downed hosts if there is any.")
			hosts := s.ring.allHosts()

			// Print session.ring for debug.
			s.logger.Debug("Logging current ring state.", newLogFieldString("ring", ringString(hosts)))

			for _, h := range hosts {
				if h.IsUp() {
					continue
				}
				s.logger.Debug("Reconnecting to downed host.",
					newLogFieldIp("host_addr", h.ConnectAddress()),
					newLogFieldInt("host_port", h.Port()),
					newLogFieldString("host_id", h.HostID()))
				// we let the pool call handleNodeConnected to change the host state
				s.pool.addHost(h)
			}
		case <-s.ctx.Done():
			return
		}
	}
}

// Query generates a new query object for interacting with the database.
// Further details of the query may be tweaked using the resulting query
// value before the query is executed. Query is automatically prepared
// if it has not previously been executed.
//
// Supported Go to CQL type conversions for query parameters are as follows:
//
//	Go type (value)             | CQL type                    | Note
//	string, []byte              | varchar, ascii, blob, text  |
//	bool                        | boolean                     |
//	integer types               | tinyint, smallint, int      |
//	string                      | tinyint, smallint, int      | formatted as base 10 number
//	integer types               | bigint, counter             |
//	big.Int                     | bigint, counter             | according to cassandra bigint specification the big.Int value limited to int64 size(an eight-byte two's complement integer.)
//	string                      | bigint, counter             | formatted as base 10 number
//	float32                     | float                       |
//	float64                     | double                      |
//	inf.Dec                     | decimal                     |
//	int64                       | time                        | nanoseconds since start of day
//	time.Duration               | time                        | duration since start of day
//	int64                       | timestamp                   | milliseconds since Unix epoch
//	time.Time                   | timestamp                   |
//	slice, array                | list, set                   |
//	map[X]struct{}              | list, set                   |
//	map[X]Y                     | map                         |
//	gocql.UUID                  | uuid, timeuuid              |
//	[16]byte                    | uuid, timeuuid              | raw UUID bytes
//	[]byte                      | uuid, timeuuid              | raw UUID bytes, length must be 16 bytes
//	string                      | uuid, timeuuid              | hex representation, see ParseUUID
//	integer types               | varint                      |
//	big.Int                     | varint                      |
//	string                      | varint                      | value of number in decimal notation
//	net.IP                      | inet                        |
//	string                      | inet                        | IPv4 or IPv6 address string
//	slice, array                | tuple                       |
//	struct                      | tuple                       | fields are marshaled in order of declaration
//	gocql.UDTMarshaler          | user-defined type           | MarshalUDT is called
//	map[string]interface{}      | user-defined type           |
//	struct                      | user-defined type           | struct fields' cql tags are used for column names
//	int64                       | date                        | milliseconds since Unix epoch to start of day (in UTC)
//	time.Time                   | date                        | start of day (in UTC)
//	string                      | date                        | parsed using "2006-01-02" format
//	int64                       | duration                    | duration in nanoseconds
//	time.Duration               | duration                    |
//	gocql.Duration              | duration                    |
//	string                      | duration                    | parsed with time.ParseDuration
func (s *Session) Query(stmt string, values ...interface{}) *Query {
	qry := &Query{}
	qry.session = s
	qry.stmt = stmt
	qry.values = values
	qry.hostID = ""
	qry.defaultsFromSession()
	return qry
}

// QueryInfo represents metadata information about a prepared query.
// It contains the query ID, argument information, result information, and primary key columns.
type QueryInfo struct {
	Id          []byte
	Args        []ColumnInfo
	Rval        []ColumnInfo
	PKeyColumns []int
}

// Bind generates a new query object based on the query statement passed in.
// The query is automatically prepared if it has not previously been executed.
// The binding callback allows the application to define which query argument
// values will be marshalled as part of the query execution.
// During execution, the meta data of the prepared query will be routed to the
// binding callback, which is responsible for producing the query argument values.
//
// For supported Go to CQL type conversions for query parameters, see Session.Query documentation.
func (s *Session) Bind(stmt string, b func(q *QueryInfo) ([]interface{}, error)) *Query {
	qry := &Query{}
	qry.session = s
	qry.stmt = stmt
	qry.binding = b
	qry.defaultsFromSession()
	return qry
}

// Close closes all connections. The session is unusable after this
// operation.
func (s *Session) Close() {

	s.sessionStateMu.Lock()
	if s.isClosing {
		s.sessionStateMu.Unlock()
		return
	}
	s.isClosing = true
	s.sessionStateMu.Unlock()

	if s.pool != nil {
		s.pool.Close()
	}

	if s.control != nil {
		s.control.close()
	}

	if s.nodeEvents != nil {
		s.nodeEvents.stop()
	}

	if s.schemaEvents != nil {
		s.schemaEvents.stop()
	}

	if s.ringRefresher != nil {
		s.ringRefresher.stop()
	}

	if s.cancel != nil {
		s.cancel()
	}

	s.sessionStateMu.Lock()
	s.isClosed = true
	s.sessionStateMu.Unlock()
}

func (s *Session) Closed() bool {
	s.sessionStateMu.RLock()
	closed := s.isClosed
	s.sessionStateMu.RUnlock()
	return closed
}

func (s *Session) initialized() bool {
	s.sessionStateMu.RLock()
	initialized := s.isInitialized
	s.sessionStateMu.RUnlock()
	return initialized
}

func (s *Session) executeQuery(qry *internalQuery) (it *Iter) {
	// fail fast
	if s.Closed() {
		return newErrIter(ErrSessionClosed, qry.metrics, qry.Keyspace(), qry.getRoutingInfo(), qry.getKeyspaceFunc())
	}

	iter, err := s.executor.executeQuery(qry)
	if err != nil {
		return newErrIter(err, qry.metrics, qry.Keyspace(), qry.getRoutingInfo(), qry.getKeyspaceFunc())
	}
	if iter == nil {
		panic("nil iter")
	}

	return iter
}

func (s *Session) removeHost(h *HostInfo) {
	s.logger.Warning("Removing host.", newLogFieldIp("host_addr", h.ConnectAddress()), newLogFieldString("host_id", h.HostID()))
	s.policy.RemoveHost(h)
	hostID := h.HostID()
	s.pool.removeHost(hostID)
	s.ring.removeHost(hostID)
}

// KeyspaceMetadata returns the schema metadata for the keyspace specified. Returns an error if the keyspace does not exist.
func (s *Session) KeyspaceMetadata(keyspace string) (*KeyspaceMetadata, error) {
	// fail fast
	if s.Closed() {
		return nil, ErrSessionClosed
	} else if keyspace == "" {
		return nil, ErrNoKeyspace
	}

	return s.schemaDescriber.getSchema(keyspace)
}

func (s *Session) getConn() *Conn {
	hosts := s.ring.allHosts()
	for _, host := range hosts {
		if !host.IsUp() {
			continue
		}

		pool, ok := s.pool.getPool(host)
		if !ok {
			continue
		} else if conn := pool.Pick(); conn != nil {
			return conn
		}
	}

	return nil
}

// Returns routing key indexes and type info.
// If keyspace == "" it uses the keyspace which is specified in Cluster.Keyspace
func (s *Session) routingKeyInfo(ctx context.Context, stmt string, keyspace string) (*routingKeyInfo, error) {
	if keyspace == "" {
		keyspace = s.cfg.Keyspace
	}

	routingKeyInfoCacheKey := keyspace + stmt

	s.routingKeyInfoCache.mu.Lock()

	// Using here keyspace + stmt as a cache key because
	// the query keyspace could be overridden via SetKeyspace
	entry, cached := s.routingKeyInfoCache.lru.Get(routingKeyInfoCacheKey)
	if cached {
		// done accessing the cache
		s.routingKeyInfoCache.mu.Unlock()
		// the entry is an inflight struct similar to that used by
		// Conn to prepare statements
		inflight := entry.(*inflightCachedEntry)

		// wait for any inflight work
		inflight.wg.Wait()

		if inflight.err != nil {
			return nil, inflight.err
		}

		key, _ := inflight.value.(*routingKeyInfo)

		return key, nil
	}

	// create a new inflight entry while the data is created
	inflight := new(inflightCachedEntry)
	inflight.wg.Add(1)
	defer inflight.wg.Done()
	s.routingKeyInfoCache.lru.Add(routingKeyInfoCacheKey, inflight)
	s.routingKeyInfoCache.mu.Unlock()

	var (
		info         *preparedStatment
		partitionKey []*ColumnMetadata
	)

	conn := s.getConn()
	if conn == nil {
		// TODO: better error?
		inflight.err = errors.New("gocql: unable to fetch prepared info: no connection available")
		return nil, inflight.err
	}

	// get the query info for the statement
	info, inflight.err = conn.prepareStatement(ctx, stmt, nil, keyspace)
	if inflight.err != nil {
		// don't cache this error
		s.routingKeyInfoCache.Remove(stmt)
		return nil, inflight.err
	}

	// TODO: it would be nice to mark hosts here but as we are not using the policies
	// to fetch hosts we cant

	if info.request.colCount == 0 {
		// no arguments, no routing key, and no error
		return nil, nil
	}

	table := info.request.table
	if info.request.keyspace != "" {
		keyspace = info.request.keyspace
	}

	if len(info.request.pkeyColumns) > 0 {
		// proto v4 dont need to calculate primary key columns
		types := make([]TypeInfo, len(info.request.pkeyColumns))
		for i, col := range info.request.pkeyColumns {
			types[i] = info.request.columns[col].TypeInfo
		}

		routingKeyInfo := &routingKeyInfo{
			indexes:  info.request.pkeyColumns,
			types:    types,
			keyspace: keyspace,
			table:    table,
		}

		inflight.value = routingKeyInfo
		return routingKeyInfo, nil
	}

	var keyspaceMetadata *KeyspaceMetadata
	keyspaceMetadata, inflight.err = s.KeyspaceMetadata(info.request.columns[0].Keyspace)
	if inflight.err != nil {
		// don't cache this error
		s.routingKeyInfoCache.Remove(stmt)
		return nil, inflight.err
	}

	tableMetadata, found := keyspaceMetadata.Tables[table]
	if !found {
		// unlikely that the statement could be prepared and the metadata for
		// the table couldn't be found, but this may indicate either a bug
		// in the metadata code, or that the table was just dropped.
		inflight.err = ErrNoMetadata
		// don't cache this error
		s.routingKeyInfoCache.Remove(stmt)
		return nil, inflight.err
	}

	partitionKey = tableMetadata.PartitionKey

	size := len(partitionKey)
	routingKeyInfo := &routingKeyInfo{
		indexes:  make([]int, size),
		types:    make([]TypeInfo, size),
		keyspace: keyspace,
		table:    table,
	}

	for keyIndex, keyColumn := range partitionKey {
		// set an indicator for checking if the mapping is missing
		routingKeyInfo.indexes[keyIndex] = -1

		// find the column in the query info
		for argIndex, boundColumn := range info.request.columns {
			if keyColumn.Name == boundColumn.Name {
				// there may be many such bound columns, pick the first
				routingKeyInfo.indexes[keyIndex] = argIndex
				routingKeyInfo.types[keyIndex] = boundColumn.TypeInfo
				break
			}
		}

		if routingKeyInfo.indexes[keyIndex] == -1 {
			// missing a routing key column mapping
			// no routing key, and no error
			return nil, nil
		}
	}

	// cache this result
	inflight.value = routingKeyInfo

	return routingKeyInfo, nil
}

// Exec executes a batch operation and returns nil if successful
// otherwise an error is returned describing the failure.
func (b *Batch) Exec() error {
	iter := b.session.executeBatch(b, b.context)
	return iter.Close()
}

// ExecContext executes a batch operation with the provided context and returns nil if successful
// otherwise an error is returned describing the failure.
func (b *Batch) ExecContext(ctx context.Context) error {
	iter := b.session.executeBatch(b, ctx)
	return iter.Close()
}

// Iter executes a batch operation and returns an Iter object
// that can be used to access properties related to the execution like Iter.Attempts and Iter.Latency
func (b *Batch) Iter() *Iter { return b.IterContext(b.context) }

// IterContext executes a batch operation with the provided context and returns an Iter object
// that can be used to access properties related to the execution like Iter.Attempts and Iter.Latency
func (b *Batch) IterContext(ctx context.Context) *Iter {
	return b.session.executeBatch(b, ctx)
}

func (s *Session) executeBatch(batch *Batch, ctx context.Context) *Iter {
	b := newInternalBatch(batch, ctx)
	// fail fast
	if s.Closed() {
		return newErrIter(ErrSessionClosed, b.metrics, b.Keyspace(), b.getRoutingInfo(), b.getKeyspaceFunc())
	}

	// Prevent the execution of the batch if greater than the limit
	// Currently batches have a limit of 65536 queries.
	// https://datastax-oss.atlassian.net/browse/JAVA-229
	if batch.Size() > BatchSizeMaximum {
		return newErrIter(ErrTooManyStmts, b.metrics, b.Keyspace(), b.getRoutingInfo(), b.getKeyspaceFunc())
	}

	iter, err := s.executor.executeQuery(b)
	if err != nil {
		return newErrIter(err, b.metrics, b.Keyspace(), b.getRoutingInfo(), b.getKeyspaceFunc())
	}

	return iter
}

// Deprecated: use Batch.Exec instead.
// ExecuteBatch executes a batch operation and returns nil if successful
// otherwise an error is returned describing the failure.
func (s *Session) ExecuteBatch(batch *Batch) error {
	iter := s.executeBatch(batch, batch.context)
	return iter.Close()
}

// Deprecated: use Batch.ExecCAS instead
// ExecuteBatchCAS executes a batch operation and returns true if successful and
// an iterator (to scan additional rows if more than one conditional statement)
// was sent.
// Further scans on the interator must also remember to include
// the applied boolean as the first argument to *Iter.Scan
func (s *Session) ExecuteBatchCAS(batch *Batch, dest ...interface{}) (applied bool, iter *Iter, err error) {
	return batch.ExecCAS(dest...)
}

// ExecCAS executes a batch operation and returns true if successful and
// an iterator (to scan additional rows if more than one conditional statement)
// was sent.
// Further scans on the interator must also remember to include
// the applied boolean as the first argument to *Iter.Scan
func (b *Batch) ExecCAS(dest ...interface{}) (applied bool, iter *Iter, err error) {
	return b.ExecCASContext(b.context, dest...)
}

// ExecCASContext executes a batch operation with the provided context and returns true if successful and
// an iterator (to scan additional rows if more than one conditional statement)
// was sent.
// Further scans on the interator must also remember to include
// the applied boolean as the first argument to *Iter.Scan
func (b *Batch) ExecCASContext(ctx context.Context, dest ...interface{}) (applied bool, iter *Iter, err error) {
	iter = b.session.executeBatch(b, ctx)
	if err := iter.checkErrAndNotFound(); err != nil {
		iter.Close()
		return false, nil, err
	}

	if len(iter.Columns()) > 1 {
		dest = append([]interface{}{&applied}, dest...)
		iter.Scan(dest...)
	} else {
		iter.Scan(&applied)
	}

	return applied, iter, iter.err
}

// Deprecated: use Batch.MapExecCAS instead
// MapExecuteBatchCAS executes a batch operation much like ExecuteBatchCAS,
// however it accepts a map rather than a list of arguments for the initial
// scan.
func (s *Session) MapExecuteBatchCAS(batch *Batch, dest map[string]interface{}) (applied bool, iter *Iter, err error) {
	return batch.MapExecCAS(dest)
}

// MapExecCAS executes a batch operation much like ExecuteBatchCAS,
// however it accepts a map rather than a list of arguments for the initial
// scan.
func (b *Batch) MapExecCAS(dest map[string]interface{}) (applied bool, iter *Iter, err error) {
	return b.MapExecCASContext(b.context, dest)
}

// MapExecCASContext executes a batch operation with the provided context much like ExecuteBatchCAS,
// however it accepts a map rather than a list of arguments for the initial
// scan.
func (b *Batch) MapExecCASContext(ctx context.Context, dest map[string]interface{}) (applied bool, iter *Iter, err error) {
	iter = b.session.executeBatch(b, ctx)
	if err := iter.checkErrAndNotFound(); err != nil {
		iter.Close()
		return false, nil, err
	}
	iter.MapScan(dest)
	if iter.err != nil {
		return false, iter, iter.err
	}
	// check if [applied] was returned, otherwise it might not be CAS
	if _, ok := dest["[applied]"]; ok {
		applied = dest["[applied]"].(bool)
		delete(dest, "[applied]")
	}

	// we usually close here, but instead of closing, just returin an error
	// if MapScan failed. Although Close just returns err, using Close
	// here might be confusing as we are not actually closing the iter
	return applied, iter, iter.err
}

type hostMetrics struct {
	// Attempts is count of how many times this query has been attempted for this host.
	// An attempt is either a retry or fetching next page of results.
	Attempts int

	// TotalLatency is the sum of attempt latencies for this host in nanoseconds.
	TotalLatency int64
}

type queryMetrics struct {
	totalAttempts int64
	totalLatency  int64
}

func (qm *queryMetrics) attempt(addLatency time.Duration) int {
	atomic.AddInt64(&qm.totalLatency, addLatency.Nanoseconds())
	return int(atomic.AddInt64(&qm.totalAttempts, 1) - 1)
}

func (qm *queryMetrics) attempts() int {
	return int(atomic.LoadInt64(&qm.totalAttempts))
}

func (qm *queryMetrics) latency() int64 {
	attempts := atomic.LoadInt64(&qm.totalAttempts)
	if attempts == 0 {
		return atomic.LoadInt64(&qm.totalLatency)
	}
	return atomic.LoadInt64(&qm.totalLatency) / attempts
}

type hostMetricsManager interface {
	attempt(addLatency time.Duration, host *HostInfo) *hostMetrics
}

type hostMetricsManagerImpl struct {
	l sync.RWMutex
	m map[string]*hostMetrics
}

func newHostMetricsManager() *hostMetricsManagerImpl {
	return &hostMetricsManagerImpl{m: make(map[string]*hostMetrics)}
}

// preFilledHostMetricsMetricsManager initializes new hostMetrics based on per-host supplied data.
func preFilledHostMetricsMetricsManager(m map[string]*hostMetrics) *hostMetricsManagerImpl {
	return &hostMetricsManagerImpl{m: m}
}

// hostMetricsLocked gets or creates host metrics for given host.
// It must be called only while holding qm.l lock.
func (qm *hostMetricsManagerImpl) hostMetricsLocked(host *HostInfo) *hostMetrics {
	metrics, exists := qm.m[host.ConnectAddress().String()]
	if !exists {
		// if the host is not in the map, it means it's been accessed for the first time
		metrics = &hostMetrics{}
		qm.m[host.ConnectAddress().String()] = metrics
	}

	return metrics
}

func (qm *hostMetricsManagerImpl) attempt(addLatency time.Duration, host *HostInfo) *hostMetrics {
	qm.l.Lock()
	updateHostMetrics := qm.hostMetricsLocked(host)
	updateHostMetrics.Attempts += 1
	updateHostMetrics.TotalLatency += addLatency.Nanoseconds()
	qm.l.Unlock()
	return updateHostMetrics
}

var emptyHostMetricsManager = &emptyHostMetricsManagerImpl{}

type emptyHostMetricsManagerImpl struct {
}

func (qm *emptyHostMetricsManagerImpl) attempt(_ time.Duration, _ *HostInfo) *hostMetrics {
	return nil
}

// Query represents a CQL statement that can be executed.
type Query struct {
	stmt                  string
	values                []interface{}
	initialConsistency    Consistency
	pageSize              int
	routingKey            []byte
	initialPageState      []byte
	prefetch              float64
	trace                 Tracer
	observer              QueryObserver
	session               *Session
	rt                    RetryPolicy
	spec                  SpeculativeExecutionPolicy
	binding               func(q *QueryInfo) ([]interface{}, error)
	serialCons            Consistency
	defaultTimestamp      bool
	defaultTimestampValue int64
	disableSkipMetadata   bool
	context               context.Context
	idempotent            bool
	customPayload         map[string][]byte

	disableAutoPage bool

	// getKeyspace is field so that it can be overriden in tests
	getKeyspace func() string

	// used by control conn queries to prevent triggering a write to systems
	// tables in AWS MCS see
	skipPrepare bool

	// hostID specifies the host on which the query should be executed.
	// If it is empty, then the host is picked by HostSelectionPolicy
	hostID string

	keyspace          string
	nowInSecondsValue *int
}

type queryRoutingInfo struct {
	// mu protects contents of queryRoutingInfo.
	mu sync.RWMutex

	keyspace string

	table string
}

func (qr *queryRoutingInfo) getKeyspace() string {
	qr.mu.RLock()
	defer qr.mu.RUnlock()
	return qr.keyspace
}

func (qr *queryRoutingInfo) getTable() string {
	qr.mu.RLock()
	defer qr.mu.RUnlock()
	return qr.table
}

func (q *Query) defaultsFromSession() {
	s := q.session

	q.initialConsistency = s.cons
	q.pageSize = s.pageSize
	q.trace = s.trace
	q.observer = s.queryObserver
	q.prefetch = s.prefetch
	q.rt = s.cfg.RetryPolicy
	q.serialCons = s.cfg.SerialConsistency
	q.defaultTimestamp = s.cfg.DefaultTimestamp
	q.idempotent = s.cfg.DefaultIdempotence

	q.spec = &NonSpeculativeExecution{}
}

// Statement returns the statement that was used to generate this query.
func (q Query) Statement() string {
	return q.stmt
}

// Values returns the values passed in via Bind.
// This can be used by a wrapper type that needs to access the bound values.
func (q Query) Values() []interface{} {
	return q.values
}

// String implements the stringer interface.
func (q Query) String() string {
	return fmt.Sprintf("[query statement=%q values=%+v consistency=%s]", q.stmt, q.values, q.initialConsistency)
}

// Consistency sets the consistency level for this query. If no consistency
// level have been set, the default consistency level of the cluster
// is used.
func (q *Query) Consistency(c Consistency) *Query {
	q.initialConsistency = c
	return q
}

// GetConsistency returns the currently configured consistency level for
// the query.
func (q *Query) GetConsistency() Consistency {
	return q.initialConsistency
}

// Deprecated: use Query.Consistency instead
func (q *Query) SetConsistency(c Consistency) {
	q.initialConsistency = c
}

// CustomPayload sets the custom payload level for this query. The map is not copied internally
// so it shouldn't be modified after the query is scheduled for execution.
func (q *Query) CustomPayload(customPayload map[string][]byte) *Query {
	q.customPayload = customPayload
	return q
}

// Deprecated: Context retrieval is deprecated. Pass context directly to execution methods
// like ExecContext or IterContext instead.
func (q *Query) Context() context.Context {
	if q.context == nil {
		return context.Background()
	}
	return q.context
}

// Trace enables tracing of this query. Look at the documentation of the
// Tracer interface to learn more about tracing.
func (q *Query) Trace(trace Tracer) *Query {
	q.trace = trace
	return q
}

// Observer enables query-level observer on this query.
// The provided observer will be called every time this query is executed.
func (q *Query) Observer(observer QueryObserver) *Query {
	q.observer = observer
	return q
}

// PageSize will tell the iterator to fetch the result in pages of size n.
// This is useful for iterating over large result sets, but setting the
// page size too low might decrease the performance. This feature is only
// available in Cassandra 2 and onwards.
func (q *Query) PageSize(n int) *Query {
	q.pageSize = n
	return q
}

// DefaultTimestamp will enable the with default timestamp flag on the query.
// If enable, this will replace the server side assigned
// timestamp as default timestamp. Note that a timestamp in the query itself
// will still override this timestamp. This is entirely optional.
//
// Only available on protocol >= 3
func (q *Query) DefaultTimestamp(enable bool) *Query {
	q.defaultTimestamp = enable
	return q
}

// WithTimestamp will enable the with default timestamp flag on the query
// like DefaultTimestamp does. But also allows to define value for timestamp.
// It works the same way as USING TIMESTAMP in the query itself, but
// should not break prepared query optimization.
//
// Only available on protocol >= 3
func (q *Query) WithTimestamp(timestamp int64) *Query {
	q.DefaultTimestamp(true)
	q.defaultTimestampValue = timestamp
	return q
}

// RoutingKey sets the routing key to use when a token aware connection
// pool is used to optimize the routing of this query.
func (q *Query) RoutingKey(routingKey []byte) *Query {
	q.routingKey = routingKey
	return q
}

// Deprecated: Use Query.ExecContext or Query.IterContext instead. This will be removed in a future major version.
//
// WithContext returns a shallow copy of q with its context
// set to ctx.
//
// The provided context controls the entire lifetime of executing a
// query, queries will be canceled and return once the context is
// canceled.
func (q *Query) WithContext(ctx context.Context) *Query {
	q2 := *q
	q2.context = ctx
	return &q2
}

// Keyspace returns the keyspace the query will be executed against.
func (q *Query) Keyspace() string {
	if q.getKeyspace != nil {
		return q.getKeyspace()
	}
	if q.keyspace != "" {
		return q.keyspace
	}

	if q.session == nil {
		return ""
	}
	// TODO(chbannis): this should be parsed from the query or we should let
	// this be set by users.
	return q.session.cfg.Keyspace
}

func (q *Query) shouldPrepare() bool {
	return shouldPrepare(q.stmt)
}

func shouldPrepare(s string) bool {
	stmt := strings.TrimLeftFunc(strings.TrimRightFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) || r == ';'
	}), unicode.IsSpace)

	var stmtType string
	if n := strings.IndexFunc(stmt, unicode.IsSpace); n >= 0 {
		stmtType = strings.ToLower(stmt[:n])
	}
	if stmtType == "begin" {
		if n := strings.LastIndexFunc(stmt, unicode.IsSpace); n >= 0 {
			stmtType = strings.ToLower(stmt[n+1:])
		}
	}
	switch stmtType {
	case "select", "insert", "update", "delete", "batch":
		return true
	}
	return false
}

// SetPrefetch sets the default threshold for pre-fetching new pages. If
// there are only p*pageSize rows remaining, the next page will be requested
// automatically.
func (q *Query) Prefetch(p float64) *Query {
	q.prefetch = p
	return q
}

// RetryPolicy sets the policy to use when retrying the query.
func (q *Query) RetryPolicy(r RetryPolicy) *Query {
	q.rt = r
	return q
}

// SetSpeculativeExecutionPolicy sets the execution policy
func (q *Query) SetSpeculativeExecutionPolicy(sp SpeculativeExecutionPolicy) *Query {
	q.spec = sp
	return q
}

// IsIdempotent returns whether the query is marked as idempotent.
// Non-idempotent query won't be retried.
// See "Retries and speculative execution" in package docs for more details.
func (q *Query) IsIdempotent() bool {
	return q.idempotent
}

// Idempotent marks the query as being idempotent or not depending on
// the value.
// Non-idempotent query won't be retried.
// See "Retries and speculative execution" in package docs for more details.
func (q *Query) Idempotent(value bool) *Query {
	q.idempotent = value
	return q
}

// Bind sets query arguments of query. This can also be used to rebind new query arguments
// to an existing query instance.
//
// For supported Go to CQL type conversions for query parameters, see Session.Query documentation.
func (q *Query) Bind(v ...interface{}) *Query {
	q.values = v
	return q
}

// SerialConsistency sets the consistency level for the
// serial phase of conditional updates. That consistency can only be
// either SERIAL or LOCAL_SERIAL and if not present, it defaults to
// SERIAL. This option will be ignored for anything else that a
// conditional update/insert.
func (q *Query) SerialConsistency(cons Consistency) *Query {
	if !cons.isSerial() {
		panic("serial consistency can only be SERIAL or LOCAL_SERIAL got " + cons.String())
	}
	q.serialCons = cons
	return q
}

// PageState sets the paging state for the query to resume paging from a specific
// point in time. Setting this will disable to query paging for this query, and
// must be used for all subsequent pages.
func (q *Query) PageState(state []byte) *Query {
	q.initialPageState = state
	q.disableAutoPage = true
	return q
}

// NoSkipMetadata will override the internal result metadata cache so that the driver does not
// send skip_metadata for queries, this means that the result will always contain
// the metadata to parse the rows and will not reuse the metadata from the prepared
// statement. This should only be used to work around cassandra bugs, such as when using
// CAS operations which do not end in Cas.
//
// See https://issues.apache.org/jira/browse/CASSANDRA-11099
// https://github.com/apache/cassandra-gocql-driver/issues/612
func (q *Query) NoSkipMetadata() *Query {
	q.disableSkipMetadata = true
	return q
}

// Exec executes the query without returning any rows.
func (q *Query) Exec() error {
	return q.Iter().Close()
}

// ExecContext executes the query with the provided context without returning any rows.
func (q *Query) ExecContext(ctx context.Context) error {
	return q.IterContext(ctx).Close()
}

func isUseStatement(stmt string) bool {
	if len(stmt) < 3 {
		return false
	}

	return strings.EqualFold(stmt[0:3], "use")
}

// Iter executes the query and returns an iterator capable of iterating
// over all results.
func (q *Query) Iter() *Iter {
	return q.IterContext(q.context)
}

// IterContext executes the query with the provided context and returns an iterator capable of iterating
// over all results.
func (q *Query) IterContext(ctx context.Context) *Iter {
	if isUseStatement(q.stmt) {
		return newErrIter(ErrUseStmt, &queryMetrics{}, q.Keyspace(), nil, q.getKeyspace)
	}

	internalQry := newInternalQuery(q, ctx)
	return q.session.executeQuery(internalQry)
}

func (q *Query) iterInternal(c *Conn, ctx context.Context) *Iter {
	internalQry := newInternalQuery(q, ctx)
	internalQry.conn = c

	iter := c.executeQuery(internalQry.Context(), internalQry)
	if iter != nil {
		// set iter.host so that the caller can retrieve the connect address which should be preferable (if valid) for the local host
		iter.host = c.host
	}
	return iter
}

// MapScan executes the query, copies the columns of the first selected
// row into the map pointed at by m and discards the rest. If no rows
// were selected, ErrNotFound is returned.
//
// Columns are automatically converted to Go types based on their CQL type.
// See Iter.SliceMap for the complete CQL to Go type mapping table and examples.
func (q *Query) MapScan(m map[string]interface{}) error {
	return q.MapScanContext(q.context, m)
}

// MapScanContext executes the query with the provided context, copies the columns of the first selected
// row into the map pointed at by m and discards the rest. If no rows
// were selected, ErrNotFound is returned.
func (q *Query) MapScanContext(ctx context.Context, m map[string]interface{}) error {
	iter := q.IterContext(ctx)
	if err := iter.checkErrAndNotFound(); err != nil {
		return err
	}
	iter.MapScan(m)
	return iter.Close()
}

// Scan executes the query, copies the columns of the first selected
// row into the values pointed at by dest and discards the rest. If no rows
// were selected, ErrNotFound is returned.
//
// For supported CQL to Go type conversions, see Iter.Scan documentation.
func (q *Query) Scan(dest ...interface{}) error {
	return q.ScanContext(q.context, dest...)
}

// ScanContext executes the query with the provided context, copies the columns of the first selected
// row into the values pointed at by dest and discards the rest. If no rows
// were selected, ErrNotFound is returned.
//
// For supported CQL to Go type conversions, see Iter.Scan documentation.
func (q *Query) ScanContext(ctx context.Context, dest ...interface{}) error {
	iter := q.IterContext(ctx)
	if err := iter.checkErrAndNotFound(); err != nil {
		return err
	}
	iter.Scan(dest...)
	return iter.Close()
}

// ScanCAS executes a lightweight transaction (i.e. an UPDATE or INSERT
// statement containing an IF clause). If the transaction fails because
// the existing values did not match, the previous values will be stored
// in dest.
//
// As for INSERT .. IF NOT EXISTS, previous values will be returned as if
// SELECT * FROM. So using ScanCAS with INSERT is inherently prone to
// column mismatching. Use MapScanCAS to capture them safely.
//
// For supported CQL to Go type conversions, see Iter.Scan documentation.
func (q *Query) ScanCAS(dest ...interface{}) (applied bool, err error) {
	return q.ScanCASContext(q.context, dest...)
}

// ScanCASContext executes a lightweight transaction (i.e. an UPDATE or INSERT
// statement containing an IF clause) with the provided context. If the transaction fails because
// the existing values did not match, the previous values will be stored
// in dest.
//
// As for INSERT .. IF NOT EXISTS, previous values will be returned as if
// SELECT * FROM. So using ScanCAS with INSERT is inherently prone to
// column mismatching. Use MapScanCAS to capture them safely.
//
// For supported CQL to Go type conversions, see Iter.Scan documentation.
func (q *Query) ScanCASContext(ctx context.Context, dest ...interface{}) (applied bool, err error) {
	q.disableSkipMetadata = true
	iter := q.IterContext(ctx)
	if err := iter.checkErrAndNotFound(); err != nil {
		return false, err
	}
	if len(iter.Columns()) > 1 {
		dest = append([]interface{}{&applied}, dest...)
		iter.Scan(dest...)
	} else {
		iter.Scan(&applied)
	}
	return applied, iter.Close()
}

// MapScanCAS executes a lightweight transaction (i.e. an UPDATE or INSERT
// statement containing an IF clause). If the transaction fails because
// the existing values did not match, the previous values will be stored
// in dest map.
//
// As for INSERT .. IF NOT EXISTS, previous values will be returned as if
// SELECT * FROM. So using ScanCAS with INSERT is inherently prone to
// column mismatching. MapScanCAS is added to capture them safely.
func (q *Query) MapScanCAS(dest map[string]interface{}) (applied bool, err error) {
	return q.MapScanCASContext(q.context, dest)
}

// MapScanCASContext executes a lightweight transaction (i.e. an UPDATE or INSERT
// statement containing an IF clause) with the provided context. If the transaction fails because
// the existing values did not match, the previous values will be stored
// in dest map.
//
// As for INSERT .. IF NOT EXISTS, previous values will be returned as if
// SELECT * FROM. So using ScanCAS with INSERT is inherently prone to
// column mismatching. MapScanCAS is added to capture them safely.
func (q *Query) MapScanCASContext(ctx context.Context, dest map[string]interface{}) (applied bool, err error) {
	q.disableSkipMetadata = true
	iter := q.IterContext(ctx)
	if err := iter.checkErrAndNotFound(); err != nil {
		return false, err
	}
	iter.MapScan(dest)
	if iter.err != nil {
		return false, iter.err
	}
	// check if [applied] was returned, otherwise it might not be CAS
	if _, ok := dest["[applied]"]; ok {
		applied = dest["[applied]"].(bool)
		delete(dest, "[applied]")
	}

	return applied, iter.Close()
}

// SetHostID allows to define the host the query should be executed against. If the
// host was filtered or otherwise unavailable, then the query will error. If an empty
// string is sent, the default behavior, using the configured HostSelectionPolicy will
// be used. A hostID can be obtained from HostInfo.HostID() after calling GetHosts().
func (q *Query) SetHostID(hostID string) *Query {
	q.hostID = hostID
	return q
}

// GetHostID returns id of the host on which query should be executed.
func (q *Query) GetHostID() string {
	return q.hostID
}

// SetKeyspace will enable keyspace flag on the query.
// It allows to specify the keyspace that the query should be executed in
//
// Only available on protocol >= 5.
func (q *Query) SetKeyspace(keyspace string) *Query {
	q.keyspace = keyspace
	return q
}

// WithNowInSeconds will enable the with now_in_seconds flag on the query.
// Also, it allows to define now_in_seconds value.
//
// Only available on protocol >= 5.
func (q *Query) WithNowInSeconds(now int) *Query {
	q.nowInSecondsValue = &now
	return q
}

// Iter represents the result that was returned by the execution of a statement.
//
// If the statement is a query then this can be seen as an iterator that can be used to iterate over all rows that
// were returned by the query. The iterator might send additional queries to the
// database during the iteration if paging was enabled.
//
// It also contains metadata about the request that can be accessed by Iter.Keyspace(), Iter.Table(), Iter.Attempts(), Iter.Latency().
type Iter struct {
	err     error
	pos     int
	meta    resultMetadata
	numRows int
	next    *nextIter
	host    *HostInfo
	metrics *queryMetrics

	getKeyspace func() string
	keyspace    string
	routingInfo *queryRoutingInfo

	framer *framer
	closed int32
}

func newErrIter(err error, metrics *queryMetrics, keyspace string, routingInfo *queryRoutingInfo, getKeyspace func() string) *Iter {
	iter := newIter(metrics, keyspace, routingInfo, getKeyspace)
	iter.err = err
	return iter
}

func newIter(metrics *queryMetrics, keyspace string, routingInfo *queryRoutingInfo, getKeyspace func() string) *Iter {
	return &Iter{metrics: metrics, keyspace: keyspace, routingInfo: routingInfo, getKeyspace: getKeyspace}
}

// Host returns the host which the statement was sent to.
func (iter *Iter) Host() *HostInfo {
	return iter.host
}

// Columns returns the name and type of the selected columns.
func (iter *Iter) Columns() []ColumnInfo {
	return iter.meta.columns
}

// Attempts returns the number of times the statement was executed.
func (iter *Iter) Attempts() int {
	return iter.metrics.attempts()
}

// Latency returns the average amount of nanoseconds per attempt of the statement.
func (iter *Iter) Latency() int64 {
	return iter.metrics.latency()
}

// Keyspace returns the keyspace the statement was executed against if the driver could determine it.
func (iter *Iter) Keyspace() string {
	if iter.getKeyspace != nil {
		return iter.getKeyspace()
	}

	if iter.routingInfo != nil {
		if ks := iter.routingInfo.getKeyspace(); ks != "" {
			return ks
		}
	}

	return iter.keyspace
}

// Table returns name of the table the statement was executed against if the driver could determine it.
func (iter *Iter) Table() string {
	if iter.routingInfo != nil {
		return iter.routingInfo.getTable()
	}
	return ""
}

type Scanner interface {
	// Next advances the row pointer to point at the next row, the row is valid until
	// the next call of Next. It returns true if there is a row which is available to be
	// scanned into with Scan.
	// Next must be called before every call to Scan.
	Next() bool

	// Scan copies the current row's columns into dest. If the length of dest does not equal
	// the number of columns returned in the row an error is returned. If an error is encountered
	// when unmarshalling a column into the value in dest an error is returned and the row is invalidated
	// until the next call to Next.
	// Next must be called before calling Scan, if it is not an error is returned.
	//
	// For supported CQL to Go type conversions, see Iter.Scan documentation.
	Scan(...interface{}) error

	// Err returns the if there was one during iteration that resulted in iteration being unable to complete.
	// Err will also release resources held by the iterator, the Scanner should not used after being called.
	Err() error
}

type iterScanner struct {
	iter  *Iter
	cols  [][]byte
	valid bool
}

func (is *iterScanner) Next() bool {
	iter := is.iter
	if iter.err != nil {
		return false
	}

	if iter.pos >= iter.numRows {
		if iter.next != nil {
			is.iter = iter.next.fetch()
			return is.Next()
		}
		return false
	}

	for i := 0; i < len(is.cols); i++ {
		col, err := iter.readColumn()
		if err != nil {
			iter.err = err
			return false
		}
		is.cols[i] = col
	}
	iter.pos++
	is.valid = true

	return true
}

func scanColumn(p []byte, col ColumnInfo, dest []interface{}) (int, error) {
	if dest[0] == nil {
		return 1, nil
	}

	if col.TypeInfo.Type() == TypeTuple {
		// this will panic, actually a bug, please report
		tuple := col.TypeInfo.(TupleTypeInfo)

		count := len(tuple.Elems)
		// here we pass in a slice of the struct which has the number number of
		// values as elements in the tuple
		if err := Unmarshal(col.TypeInfo, p, dest[:count]); err != nil {
			return 0, err
		}
		return count, nil
	} else {
		if err := Unmarshal(col.TypeInfo, p, dest[0]); err != nil {
			return 0, err
		}
		return 1, nil
	}
}

func (is *iterScanner) Scan(dest ...interface{}) error {
	if !is.valid {
		return errors.New("gocql: Scan called without calling Next")
	}

	iter := is.iter
	// currently only support scanning into an expand tuple, such that its the same
	// as scanning in more values from a single column
	if len(dest) != iter.meta.actualColCount {
		return fmt.Errorf("gocql: not enough columns to scan into: have %d want %d", len(dest), iter.meta.actualColCount)
	}

	// i is the current position in dest, could posible replace it and just use
	// slices of dest
	i := 0
	var err error
	for _, col := range iter.meta.columns {
		var n int
		n, err = scanColumn(is.cols[i], col, dest[i:])
		if err != nil {
			break
		}
		i += n
	}

	is.valid = false
	return err
}

func (is *iterScanner) Err() error {
	iter := is.iter
	is.iter = nil
	is.cols = nil
	is.valid = false
	return iter.Close()
}

// Scanner returns a row Scanner which provides an interface to scan rows in a manner which is
// similar to database/sql. The iter should NOT be used again after calling this method.
func (iter *Iter) Scanner() Scanner {
	if iter == nil {
		return nil
	}

	return &iterScanner{iter: iter, cols: make([][]byte, len(iter.meta.columns))}
}

func (iter *Iter) readColumn() ([]byte, error) {
	return iter.framer.readBytes()
}

// Scan consumes the next row of the iterator and copies the columns of the
// current row into the values pointed at by dest. Use nil as a dest value
// to skip the corresponding column. Scan might send additional queries
// to the database to retrieve the next set of rows if paging was enabled.
//
// Scan returns true if the row was successfully unmarshaled or false if the
// end of the result set was reached or if an error occurred. Close should
// be called afterwards to retrieve any potential errors.
//
// Supported CQL to Go type conversions are as follows, other type combinations may be added in the future:
//
//	CQL Type                     | Go Type (dest)              | Note
//	ascii, text, varchar         | *string                     |
//	ascii, text, varchar         | *[]byte                     | non-nil buffer is reused
//	bigint, counter              | *int64                      |
//	bigint, counter              | *int, *int32, *int16, *int8 | with range checking
//	bigint, counter              | *uint64, *uint32, *uint16   | with range checking
//	bigint, counter              | *big.Int                    |
//	bigint, counter              | *string                     | formatted as base 10 number
//	blob                         | *[]byte                     | non-nil buffer is reused
//	boolean                      | *bool                       |
//	date                         | *time.Time                  | start of day in UTC
//	date                         | *string                     | formatted as "2006-01-02"
//	decimal                      | *inf.Dec                    |
//	double                       | *float64                    |
//	duration                     | *gocql.Duration             |
//	duration                     | *time.Duration              | with range checking
//	float                        | *float32                    |
//	inet                         | *net.IP                     |
//	inet                         | *string                     | IPv4 or IPv6 address string
//	int                          | *int                        |
//	int                          | *int32, *int16, *int8       | with range checking
//	int                          | *uint32, *uint16, *uint8    | with range checking
//	list<T>, set<T>              | *[]T                        |
//	list<T>, set<T>              | *[N]T                       | array with compatible size
//	map<K,V>                     | *map[K]V                    |
//	smallint                     | *int16                      |
//	smallint                     | *int, *int32, *int8         | with range checking
//	smallint                     | *uint16, *uint8             | with range checking
//	time                         | *time.Duration              | nanoseconds since start of day
//	time                         | *int64                      | nanoseconds since start of day
//	timestamp                    | *time.Time                  |
//	timestamp                    | *int64                      | milliseconds since Unix epoch
//	timeuuid                     | *gocql.UUID                 |
//	timeuuid                     | *time.Time                  | timestamp of the UUID
//	timeuuid                     | *string                     | hex representation
//	timeuuid                     | *[]byte                     | 16-byte raw UUID
//	tinyint                      | *int8                       |
//	tinyint                      | *int, *int32, *int16        | with range checking
//	tinyint                      | *uint8                      | with range checking
//	tuple<T1,T2,...>             | *[]interface{}              |
//	tuple<T1,T2,...>             | *[N]interface{}             | array with compatible size
//	tuple<T1,T2,...>             | *struct                     | fields unmarshaled in declaration order
//	user-defined types           | gocql.UDTUnmarshaler        | UnmarshalUDT is called
//	user-defined types           | *map[string]interface{}     |
//	user-defined types           | *struct                     | cql tag or field name matching
//	uuid                         | *gocql.UUID                 |
//	uuid                         | *string                     | hex representation
//	uuid                         | *[]byte                     | 16-byte raw UUID
//	varint                       | *big.Int                    |
//	varint                       | *int64, *int32, *int16, *int8 | with range checking
//	varint                       | *string                     | formatted as base 10 number
//	vector<T,N>                  | *[]T                        |
//	vector<T,N>                  | *[N]T                       | array with exact size match
//
// Important Notes:
//   - NULL values are unmarshaled as zero values of the destination type
//   - Use **Type (pointer to pointer) to distinguish NULL from zero values
//   - Range checking prevents overflow when converting between numeric types
//   - For SliceMap/MapScan type mappings, see Iter.SliceMap documentation
func (iter *Iter) Scan(dest ...interface{}) bool {
	if iter.err != nil {
		return false
	}

	if iter.pos >= iter.numRows {
		if iter.next != nil {
			*iter = *iter.next.fetch()
			return iter.Scan(dest...)
		}
		return false
	}

	if iter.next != nil && iter.pos >= iter.next.pos {
		iter.next.fetchAsync()
	}

	// currently only support scanning into an expand tuple, such that its the same
	// as scanning in more values from a single column
	if len(dest) != iter.meta.actualColCount {
		iter.err = fmt.Errorf("gocql: not enough columns to scan into: have %d want %d", len(dest), iter.meta.actualColCount)
		return false
	}

	// i is the current position in dest, could posible replace it and just use
	// slices of dest
	i := 0
	for _, col := range iter.meta.columns {
		colBytes, err := iter.readColumn()
		if err != nil {
			iter.err = err
			return false
		}

		n, err := scanColumn(colBytes, col, dest[i:])
		if err != nil {
			iter.err = err
			return false
		}
		i += n
	}

	iter.pos++
	return true
}

// GetCustomPayload returns any parsed custom payload results if given in the
// response from Cassandra. Note that the result is not a copy.
//
// This additional feature of CQL Protocol v4
// allows additional results and query information to be returned by
// custom QueryHandlers running in your C* cluster.
// See https://datastax.github.io/java-driver/manual/custom_payloads/
func (iter *Iter) GetCustomPayload() map[string][]byte {
	if iter.framer != nil {
		return iter.framer.customPayload
	}
	return nil
}

// Warnings returns any warnings generated if given in the response from Cassandra.
//
// This is only available starting with CQL Protocol v4.
func (iter *Iter) Warnings() []string {
	if iter.framer != nil {
		return iter.framer.header.warnings
	}
	return nil
}

// Close closes the iterator and returns any errors that happened during
// the query or the iteration.
func (iter *Iter) Close() error {
	if atomic.CompareAndSwapInt32(&iter.closed, 0, 1) {
		if iter.framer != nil {
			iter.framer = nil
		}
	}

	return iter.err
}

// WillSwitchPage detects if iterator reached end of current page
// and the next page is available.
func (iter *Iter) WillSwitchPage() bool {
	return iter.pos >= iter.numRows && iter.next != nil
}

// checkErrAndNotFound handle error and NotFound in one method.
func (iter *Iter) checkErrAndNotFound() error {
	if iter.err != nil {
		return iter.err
	} else if iter.numRows == 0 {
		return ErrNotFound
	}
	return nil
}

// PageState return the current paging state for a query which can be used for
// subsequent queries to resume paging this point.
func (iter *Iter) PageState() []byte {
	return iter.meta.pagingState
}

// NumRows returns the number of rows in this pagination, it will update when new
// pages are fetched, it is not the value of the total number of rows this iter
// will return unless there is only a single page returned.
func (iter *Iter) NumRows() int {
	return iter.numRows
}

// nextIter holds state for fetching a single page in an iterator.
// single page might be attempted multiple times due to retries.
type nextIter struct {
	q     *internalQuery
	pos   int
	oncea sync.Once
	once  sync.Once
	next  *Iter
}

func (n *nextIter) fetchAsync() {
	n.oncea.Do(func() {
		go n.fetch()
	})
}

func (n *nextIter) fetch() *Iter {
	n.once.Do(func() {
		// if the query was specifically run on a connection then re-use that
		// connection when fetching the next results
		if n.q.conn != nil {
			n.next = n.q.conn.executeQuery(n.q.qryOpts.context, n.q)
		} else {
			n.next = n.q.session.executeQuery(n.q)
		}
	})
	return n.next
}

type Batch struct {
	Type                  BatchType
	Entries               []BatchEntry
	Cons                  Consistency
	routingKey            []byte
	CustomPayload         map[string][]byte
	rt                    RetryPolicy
	spec                  SpeculativeExecutionPolicy
	trace                 Tracer
	observer              BatchObserver
	session               *Session
	serialCons            Consistency
	defaultTimestamp      bool
	defaultTimestampValue int64
	context               context.Context
	keyspace              string
	nowInSeconds          *int
}

// Deprecated: use Session.Batch instead
// NewBatch creates a new batch operation using defaults defined in the cluster
//
// Deprecated: use Session.Batch instead
func (s *Session) NewBatch(typ BatchType) *Batch {
	return s.Batch(typ)
}

// Batch creates a new batch operation using defaults defined in the cluster
func (s *Session) Batch(typ BatchType) *Batch {
	batch := &Batch{
		Type:             typ,
		rt:               s.cfg.RetryPolicy,
		serialCons:       s.cfg.SerialConsistency,
		trace:            s.trace,
		observer:         s.batchObserver,
		session:          s,
		Cons:             s.cons,
		defaultTimestamp: s.cfg.DefaultTimestamp,
		keyspace:         s.cfg.Keyspace,
		spec:             &NonSpeculativeExecution{},
	}

	return batch
}

// Trace enables tracing of this batch. Look at the documentation of the
// Tracer interface to learn more about tracing.
func (b *Batch) Trace(trace Tracer) *Batch {
	b.trace = trace
	return b
}

// Observer enables batch-level observer on this batch.
// The provided observer will be called every time this batched query is executed.
func (b *Batch) Observer(observer BatchObserver) *Batch {
	b.observer = observer
	return b
}

func (b *Batch) Keyspace() string {
	return b.keyspace
}

// Consistency sets the consistency level for this batch. If no consistency
// level have been set, the default consistency level of the cluster
// is used.
func (b *Batch) Consistency(cons Consistency) *Batch {
	b.Cons = cons
	return b
}

// GetConsistency returns the currently configured consistency level for the batch
// operation.
func (b *Batch) GetConsistency() Consistency {
	return b.Cons
}

// Deprecated: Use Batch.Consistency
func (b *Batch) SetConsistency(c Consistency) {
	b.Cons = c
}

// Deprecated: Context retrieval is deprecated. Pass context directly to execution methods
// like ExecContext or IterContext instead.
func (b *Batch) Context() context.Context {
	if b.context == nil {
		return context.Background()
	}
	return b.context
}

func (b *Batch) IsIdempotent() bool {
	for _, entry := range b.Entries {
		if !entry.Idempotent {
			return false
		}
	}
	return true
}

func (b *Batch) speculativeExecutionPolicy() SpeculativeExecutionPolicy {
	return b.spec
}

func (b *Batch) SpeculativeExecutionPolicy(sp SpeculativeExecutionPolicy) *Batch {
	b.spec = sp
	return b
}

// Query adds the query to the batch operation.
//
// For supported Go to CQL type conversions for query parameters, see Session.Query documentation.
func (b *Batch) Query(stmt string, args ...interface{}) *Batch {
	b.Entries = append(b.Entries, BatchEntry{Stmt: stmt, Args: args})
	return b
}

// Bind adds the query to the batch operation and correlates it with a binding callback
// that will be invoked when the batch is executed. The binding callback allows the application
// to define which query argument values will be marshalled as part of the batch execution.
//
// For supported Go to CQL type conversions for query parameters, see Session.Query documentation.
func (b *Batch) Bind(stmt string, bind func(q *QueryInfo) ([]interface{}, error)) {
	b.Entries = append(b.Entries, BatchEntry{Stmt: stmt, binding: bind})
}

// RetryPolicy sets the retry policy to use when executing the batch operation
func (b *Batch) RetryPolicy(r RetryPolicy) *Batch {
	b.rt = r
	return b
}

// Deprecated: Use Batch.ExecContext or Batch.IterContext instead. This will be removed in a future major version.
//
// WithContext returns a shallow copy of b with its context
// set to ctx.
//
// The provided context controls the entire lifetime of executing a
// query, queries will be canceled and return once the context is
// canceled.
func (b *Batch) WithContext(ctx context.Context) *Batch {
	b2 := *b
	b2.context = ctx
	return &b2
}

// Size returns the number of batch statements to be executed by the batch operation.
func (b *Batch) Size() int {
	return len(b.Entries)
}

// SerialConsistency sets the consistency level for the
// serial phase of conditional updates. That consistency can only be
// either SERIAL or LOCAL_SERIAL and if not present, it defaults to
// SERIAL. This option will be ignored for anything else that a
// conditional update/insert.
//
// Only available for protocol 3 and above
func (b *Batch) SerialConsistency(cons Consistency) *Batch {
	if !cons.isSerial() {
		panic("serial consistency can only be SERIAL or LOCAL_SERIAL got " + cons.String())
	}
	b.serialCons = cons
	return b
}

// DefaultTimestamp will enable the with default timestamp flag on the query.
// If enable, this will replace the server side assigned
// timestamp as default timestamp. Note that a timestamp in the query itself
// will still override this timestamp. This is entirely optional.
//
// Only available on protocol >= 3
func (b *Batch) DefaultTimestamp(enable bool) *Batch {
	b.defaultTimestamp = enable
	return b
}

// WithTimestamp will enable the with default timestamp flag on the query
// like DefaultTimestamp does. But also allows to define value for timestamp.
// It works the same way as USING TIMESTAMP in the query itself, but
// should not break prepared query optimization.
//
// Only available on protocol >= 3
func (b *Batch) WithTimestamp(timestamp int64) *Batch {
	b.DefaultTimestamp(true)
	b.defaultTimestampValue = timestamp
	return b
}

func createRoutingKey(routingKeyInfo *routingKeyInfo, values []interface{}) ([]byte, error) {
	if routingKeyInfo == nil {
		return nil, nil
	}

	if len(routingKeyInfo.indexes) == 1 {
		// single column routing key
		routingKey, err := Marshal(
			routingKeyInfo.types[0],
			values[routingKeyInfo.indexes[0]],
		)
		if err != nil {
			return nil, err
		}
		return routingKey, nil
	}

	// composite routing key
	buf := bytes.NewBuffer(make([]byte, 0, 256))
	for i := range routingKeyInfo.indexes {
		encoded, err := Marshal(
			routingKeyInfo.types[i],
			values[routingKeyInfo.indexes[i]],
		)
		if err != nil {
			return nil, err
		}
		lenBuf := []byte{0x00, 0x00}
		binary.BigEndian.PutUint16(lenBuf, uint16(len(encoded)))
		buf.Write(lenBuf)
		buf.Write(encoded)
		buf.WriteByte(0x00)
	}
	routingKey := buf.Bytes()
	return routingKey, nil
}

// SetKeyspace will enable keyspace flag on the query.
// It allows to specify the keyspace that the query should be executed in
//
// Only available on protocol >= 5.
func (b *Batch) SetKeyspace(keyspace string) *Batch {
	b.keyspace = keyspace
	return b
}

// WithNowInSeconds will enable the with now_in_seconds flag on the query.
// Also, it allows to define now_in_seconds value.
//
// Only available on protocol >= 5.
func (b *Batch) WithNowInSeconds(now int) *Batch {
	b.nowInSeconds = &now
	return b
}

// BatchType represents the type of batch.
// Available types: LoggedBatch, UnloggedBatch, CounterBatch.
type BatchType byte

const (
	LoggedBatch   BatchType = 0
	UnloggedBatch BatchType = 1
	CounterBatch  BatchType = 2
)

// BatchEntry represents a single statement within a batch operation.
// It contains the statement, arguments, and execution metadata.
type BatchEntry struct {
	Stmt       string
	Args       []interface{}
	Idempotent bool
	binding    func(q *QueryInfo) ([]interface{}, error)
}

// ColumnInfo represents metadata about a column in a query result.
// It contains the keyspace, table, column name, and type information.
type ColumnInfo struct {
	Keyspace string
	Table    string
	Name     string
	TypeInfo TypeInfo
}

func (c ColumnInfo) String() string {
	return fmt.Sprintf("[column keyspace=%s table=%s name=%s type=%v]", c.Keyspace, c.Table, c.Name, c.TypeInfo)
}

// routing key indexes LRU cache
type routingKeyInfoLRU struct {
	lru *lru.Cache
	mu  sync.Mutex
}

type routingKeyInfo struct {
	indexes  []int
	types    []TypeInfo
	keyspace string
	table    string
}

func (r *routingKeyInfo) String() string {
	return fmt.Sprintf("routing key index=%v types=%v", r.indexes, r.types)
}

func (r *routingKeyInfoLRU) Remove(key string) {
	r.mu.Lock()
	r.lru.Remove(key)
	r.mu.Unlock()
}

// Max adjusts the maximum size of the cache and cleans up the oldest records if
// the new max is lower than the previous value. Not concurrency safe.
func (r *routingKeyInfoLRU) Max(max int) {
	r.mu.Lock()
	for r.lru.Len() > max {
		r.lru.RemoveOldest()
	}
	r.lru.MaxEntries = max
	r.mu.Unlock()
}

type inflightCachedEntry struct {
	wg    sync.WaitGroup
	err   error
	value interface{}
}

// Tracer is the interface implemented by query tracers. Tracers have the
// ability to obtain a detailed event log of all events that happened during
// the execution of a query from Cassandra. Gathering this information might
// be essential for debugging and optimizing queries, but this feature should
// not be used on production systems with very high load.
type Tracer interface {
	Trace(traceId []byte)
}

type traceWriter struct {
	session *Session
	w       io.Writer
	mu      sync.Mutex
}

// NewTraceWriter returns a simple Tracer implementation that outputs
// the event log in a textual format.
func NewTraceWriter(session *Session, w io.Writer) Tracer {
	return &traceWriter{session: session, w: w}
}

func (t *traceWriter) Trace(traceId []byte) {
	var (
		coordinator string
		duration    int
	)
	iter := t.session.control.query(`SELECT coordinator, duration
			FROM system_traces.sessions
			WHERE session_id = ?`, traceId)

	iter.Scan(&coordinator, &duration)
	if err := iter.Close(); err != nil {
		t.mu.Lock()
		fmt.Fprintln(t.w, "Error:", err)
		t.mu.Unlock()
		return
	}

	var (
		timestamp time.Time
		activity  string
		source    string
		elapsed   int
		thread    string
	)

	t.mu.Lock()
	defer t.mu.Unlock()

	fmt.Fprintf(t.w, "Tracing session %016x (coordinator: %s, duration: %v):\n",
		traceId, coordinator, time.Duration(duration)*time.Microsecond)

	iter = t.session.control.query(`SELECT event_id, activity, source, source_elapsed, thread
			FROM system_traces.events
			WHERE session_id = ?`, traceId)

	for iter.Scan(&timestamp, &activity, &source, &elapsed, &thread) {
		fmt.Fprintf(t.w, "%s: %s [%s] (source: %s, elapsed: %d)\n",
			timestamp.Format("2006/01/02 15:04:05.999999"), activity, thread, source, elapsed)
	}

	if err := iter.Close(); err != nil {
		fmt.Fprintln(t.w, "Error:", err)
	}
}

// GetHosts return a list of hosts in the ring the driver knows of.
func (s *Session) GetHosts() []*HostInfo {
	return s.ring.allHosts()
}

type ObservedQuery struct {
	Keyspace  string
	Statement string

	// Values holds a slice of bound values for the query.
	// Do not modify the values here, they are shared with multiple goroutines.
	Values []interface{}

	Start time.Time // time immediately before the query was called
	End   time.Time // time immediately after the query returned

	// Rows is the number of rows in the current iter.
	// In paginated queries, rows from previous scans are not counted.
	// Rows is not used in batch queries and remains at the default value
	Rows int

	// Host is the information about the host that performed the query
	Host *HostInfo

	// The metrics per this host
	Metrics *hostMetrics

	// Err is the error in the query.
	// It only tracks network errors or errors of bad cassandra syntax, in particular selects with no match return nil error
	Err error

	// Attempt is the index of attempt at executing this query.
	// The first attempt is number zero and any retries have non-zero attempt number.
	Attempt int

	// Query object associated with this request. Should be used as read only.
	Query *Query
}

// QueryObserver is the interface implemented by query observers / stat collectors.
//
// Experimental, this interface and use may change
type QueryObserver interface {
	// ObserveQuery gets called on every query to cassandra, including all queries in an iterator when paging is enabled.
	// It doesn't get called if there is no query because the session is closed or there are no connections available.
	// The error reported only shows query errors, i.e. if a SELECT is valid but finds no matches it will be nil.
	ObserveQuery(context.Context, ObservedQuery)
}

type ObservedBatch struct {
	Keyspace   string
	Statements []string

	// Values holds a slice of bound values for each statement.
	// Values[i] are bound values passed to Statements[i].
	// Do not modify the values here, they are shared with multiple goroutines.
	Values [][]interface{}

	Start time.Time // time immediately before the batch query was called
	End   time.Time // time immediately after the batch query returned

	// Host is the informations about the host that performed the batch
	Host *HostInfo

	// Err is the error in the batch query.
	// It only tracks network errors or errors of bad cassandra syntax, in particular selects with no match return nil error
	Err error

	// The metrics per this host
	Metrics *hostMetrics

	// Attempt is the index of attempt at executing this query.
	// The first attempt is number zero and any retries have non-zero attempt number.
	Attempt int

	// Batch object associated with this request. Should be used as read only.
	Batch *Batch
}

// BatchObserver is the interface implemented by batch observers / stat collectors.
type BatchObserver interface {
	// ObserveBatch gets called on every batch query to cassandra.
	// It also gets called once for each query in a batch.
	// It doesn't get called if there is no query because the session is closed or there are no connections available.
	// The error reported only shows query errors, i.e. if a SELECT is valid but finds no matches it will be nil.
	// Unlike QueryObserver.ObserveQuery it does no reporting on rows read.
	ObserveBatch(context.Context, ObservedBatch)
}

type ObservedConnect struct {
	// Host is the information about the host about to connect
	Host *HostInfo

	Start time.Time // time immediately before the dial is called
	End   time.Time // time immediately after the dial returned

	// Err is the connection error (if any)
	Err error
}

// ConnectObserver is the interface implemented by connect observers / stat collectors.
type ConnectObserver interface {
	// ObserveConnect gets called when a new connection to cassandra is made.
	ObserveConnect(ObservedConnect)
}

// Deprecated: Unused
type Error struct {
	Code    int
	Message string
}

func (e Error) Error() string {
	return e.Message
}

var (
	ErrNotFound             = errors.New("not found")
	ErrUnavailable          = errors.New("unavailable")
	ErrUnsupported          = errors.New("feature not supported")
	ErrTooManyStmts         = errors.New("too many statements")
	ErrUseStmt              = errors.New("use statements aren't supported. Please see https://github.com/apache/cassandra-gocql-driver for explanation.")
	ErrSessionClosed        = errors.New("session has been closed")
	ErrNoConnections        = errors.New("gocql: no hosts available in the pool")
	ErrNoKeyspace           = errors.New("no keyspace provided")
	ErrKeyspaceDoesNotExist = errors.New("keyspace does not exist")
	ErrNoMetadata           = errors.New("no metadata available")
)

// ErrProtocol represents a protocol-level error.
type ErrProtocol struct{ error }

// NewErrProtocol creates a new protocol error with the specified format and arguments.
func NewErrProtocol(format string, args ...interface{}) error {
	return ErrProtocol{fmt.Errorf(format, args...)}
}

// BatchSizeMaximum is the maximum number of statements a batch operation can have.
// This limit is set by Cassandra and could change in the future.
const BatchSizeMaximum = 65535
