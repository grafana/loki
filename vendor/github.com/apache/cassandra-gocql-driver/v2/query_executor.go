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
 * Copyright (c) 2016, The Gocql authors,
 * provided under the BSD-3-Clause License.
 * See the NOTICE file distributed with this work for additional information.
 */

package gocql

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// Deprecated: Will be removed in a future major release. Also Query and Batch no longer implement this interface.
//
// Please use Statement (for Query / Batch objects) or ExecutableStatement (in HostSelectionPolicy implementations) instead.
type ExecutableQuery = ExecutableStatement

// ExecutableStatement is an interface that represents a query or batch statement that
// exposes the correct functions for the HostSelectionPolicy to operate correctly.
type ExecutableStatement interface {
	GetRoutingKey() ([]byte, error)
	Keyspace() string
	Table() string
	IsIdempotent() bool
	GetHostID() string
	Statement() Statement
}

// Statement is an interface that represents a CQL statement that the driver can execute
// (currently Query and Batch via Session.Query and Session.Batch)
type Statement interface {
	Iter() *Iter
	IterContext(ctx context.Context) *Iter
	Exec() error
	ExecContext(ctx context.Context) error
}

type internalRequest interface {
	execute(ctx context.Context, conn *Conn) *Iter
	attempt(keyspace string, end, start time.Time, iter *Iter, host *HostInfo)
	retryPolicy() RetryPolicy
	speculativeExecutionPolicy() SpeculativeExecutionPolicy
	getQueryMetrics() *queryMetrics
	getRoutingInfo() *queryRoutingInfo
	getKeyspaceFunc() func() string
	RetryableQuery
	ExecutableStatement
}

type queryExecutor struct {
	pool   *policyConnPool
	policy HostSelectionPolicy
}

func (q *queryExecutor) attemptQuery(ctx context.Context, qry internalRequest, conn *Conn) *Iter {
	start := time.Now()
	iter := qry.execute(ctx, conn)
	end := time.Now()

	qry.attempt(q.pool.keyspace, end, start, iter, conn.host)

	return iter
}

func (q *queryExecutor) speculate(ctx context.Context, qry internalRequest, sp SpeculativeExecutionPolicy,
	hostIter NextHost, results chan *Iter) *Iter {
	ticker := time.NewTicker(sp.Delay())
	defer ticker.Stop()

	for i := 0; i < sp.Attempts(); i++ {
		select {
		case <-ticker.C:
			go q.run(ctx, qry, hostIter, results)
		case <-ctx.Done():
			return newErrIter(ctx.Err(), qry.getQueryMetrics(), qry.Keyspace(), qry.getRoutingInfo(), qry.getKeyspaceFunc())
		case iter := <-results:
			return iter
		}
	}

	return nil
}

func (q *queryExecutor) executeQuery(qry internalRequest) (*Iter, error) {
	var hostIter NextHost

	// check if the host id is specified for the query,
	// if it is, the query should be executed at the corresponding host.
	if hostID := qry.GetHostID(); hostID != "" {
		host, ok := q.pool.session.ring.getHost(hostID)
		if !ok {
			return nil, ErrNoConnections
		}
		var returnedHostOnce int32 = 0
		hostIter = func() SelectedHost {
			if atomic.CompareAndSwapInt32(&returnedHostOnce, 0, 1) {
				return (*selectedHost)(host)
			}
			return nil
		}
	}

	// if host is not specified for the query,
	// then a host will be picked by HostSelectionPolicy
	if hostIter == nil {
		hostIter = q.policy.Pick(qry)
	}

	// check if the query is not marked as idempotent, if
	// it is, we force the policy to NonSpeculative
	sp := qry.speculativeExecutionPolicy()
	if qry.GetHostID() != "" || !qry.IsIdempotent() || sp.Attempts() == 0 {
		return q.do(qry.Context(), qry, hostIter), nil
	}

	// When speculative execution is enabled, we could be accessing the host iterator from multiple goroutines below.
	// To ensure we don't call it concurrently, we wrap the returned NextHost function here to synchronize access to it.
	var mu sync.Mutex
	origHostIter := hostIter
	hostIter = func() SelectedHost {
		mu.Lock()
		defer mu.Unlock()
		return origHostIter()
	}

	ctx, cancel := context.WithCancel(qry.Context())
	defer cancel()

	results := make(chan *Iter, 1)

	// Launch the main execution
	go q.run(ctx, qry, hostIter, results)

	// The speculative executions are launched _in addition_ to the main
	// execution, on a timer. So Speculation{2} would make 3 executions running
	// in total.
	if iter := q.speculate(ctx, qry, sp, hostIter, results); iter != nil {
		return iter, nil
	}

	select {
	case iter := <-results:
		return iter, nil
	case <-ctx.Done():
		return newErrIter(ctx.Err(), qry.getQueryMetrics(), qry.Keyspace(), qry.getRoutingInfo(), qry.getKeyspaceFunc()), nil
	}
}

func (q *queryExecutor) do(ctx context.Context, qry internalRequest, hostIter NextHost) *Iter {
	selectedHost := hostIter()
	rt := qry.retryPolicy()

	var lastErr error
	var iter *Iter
	for selectedHost != nil {
		host := selectedHost.Info()
		if host == nil || !host.IsUp() {
			selectedHost = hostIter()
			continue
		}

		pool, ok := q.pool.getPool(host)
		if !ok {
			selectedHost = hostIter()
			continue
		}

		conn := pool.Pick()
		if conn == nil {
			selectedHost = hostIter()
			continue
		}

		iter = q.attemptQuery(ctx, qry, conn)
		iter.host = selectedHost.Info()
		// Update host
		switch iter.err {
		case context.Canceled, context.DeadlineExceeded, ErrNotFound:
			// those errors represents logical errors, they should not count
			// toward removing a node from the pool
			selectedHost.Mark(nil)
			return iter
		default:
			selectedHost.Mark(iter.err)
		}

		// Exit if the query was successful
		// or query is not idempotent or no retry policy defined
		if iter.err == nil || !qry.IsIdempotent() || rt == nil {
			return iter
		}

		attemptsReached := !rt.Attempt(qry)
		retryType := rt.GetRetryType(iter.err)

		var stopRetries bool

		// If query is unsuccessful, check the error with RetryPolicy to retry
		switch retryType {
		case Retry:
			// retry on the same host
		case RetryNextHost:
			// retry on the next host
			selectedHost = hostIter()
		case Ignore:
			iter.err = nil
			stopRetries = true
		case Rethrow:
			stopRetries = true
		default:
			// Undefined? Return nil and error, this will panic in the requester
			return newErrIter(ErrUnknownRetryType, qry.getQueryMetrics(), qry.Keyspace(), qry.getRoutingInfo(), qry.getKeyspaceFunc())
		}

		if stopRetries || attemptsReached {
			return iter
		}

		lastErr = iter.err
		continue
	}

	if lastErr != nil {
		return newErrIter(lastErr, qry.getQueryMetrics(), qry.Keyspace(), qry.getRoutingInfo(), qry.getKeyspaceFunc())
	}

	return newErrIter(ErrNoConnections, qry.getQueryMetrics(), qry.Keyspace(), qry.getRoutingInfo(), qry.getKeyspaceFunc())
}

func (q *queryExecutor) run(ctx context.Context, qry internalRequest, hostIter NextHost, results chan<- *Iter) {
	select {
	case results <- q.do(ctx, qry, hostIter):
	case <-ctx.Done():
	}
}

type queryOptions struct {
	stmt string

	// Paging
	pageSize        int
	disableAutoPage bool

	// Monitoring
	trace    Tracer
	observer QueryObserver

	// Parameters
	values  []interface{}
	binding func(q *QueryInfo) ([]interface{}, error)

	// Timestamp
	defaultTimestamp      bool
	defaultTimestampValue int64

	// Consistency
	serialCons SerialConsistency

	// Protocol flag
	disableSkipMetadata bool

	customPayload     map[string][]byte
	prefetch          float64
	rt                RetryPolicy
	spec              SpeculativeExecutionPolicy
	context           context.Context
	idempotent        bool
	keyspace          string
	skipPrepare       bool
	routingKey        []byte
	nowInSecondsValue *int
	hostID            string

	// getKeyspace is field so that it can be overriden in tests
	getKeyspace func() string
}

func newQueryOptions(q *Query, ctx context.Context) *queryOptions {
	var newRoutingKey []byte
	if q.routingKey != nil {
		routingKey := q.routingKey
		newRoutingKey = make([]byte, len(routingKey))
		copy(newRoutingKey, routingKey)
	}
	if ctx == nil {
		ctx = q.Context()
	}
	return &queryOptions{
		stmt:                  q.stmt,
		values:                q.values,
		pageSize:              q.pageSize,
		prefetch:              q.prefetch,
		trace:                 q.trace,
		observer:              q.observer,
		rt:                    q.rt,
		spec:                  q.spec,
		binding:               q.binding,
		serialCons:            q.serialCons,
		defaultTimestamp:      q.defaultTimestamp,
		defaultTimestampValue: q.defaultTimestampValue,
		disableSkipMetadata:   q.disableSkipMetadata,
		context:               ctx,
		idempotent:            q.idempotent,
		customPayload:         q.customPayload,
		disableAutoPage:       q.disableAutoPage,
		skipPrepare:           q.skipPrepare,
		routingKey:            newRoutingKey,
		getKeyspace:           q.getKeyspace,
		nowInSecondsValue:     q.nowInSecondsValue,
		keyspace:              q.keyspace,
		hostID:                q.hostID,
	}
}

type internalQuery struct {
	originalQuery      *Query
	qryOpts            *queryOptions
	pageState          []byte
	conn               *Conn
	consistency        uint32
	session            *Session
	routingInfo        *queryRoutingInfo
	metrics            *queryMetrics
	hostMetricsManager hostMetricsManager
}

func newInternalQuery(q *Query, ctx context.Context) *internalQuery {
	var newPageState []byte
	if q.initialPageState != nil {
		pageState := q.initialPageState
		newPageState = make([]byte, len(pageState))
		copy(newPageState, pageState)
	}
	var hostMetricsMgr hostMetricsManager
	if q.observer != nil {
		hostMetricsMgr = newHostMetricsManager()
	} else {
		hostMetricsMgr = emptyHostMetricsManager
	}
	return &internalQuery{
		originalQuery:      q,
		qryOpts:            newQueryOptions(q, ctx),
		metrics:            &queryMetrics{},
		hostMetricsManager: hostMetricsMgr,
		consistency:        uint32(q.initialConsistency),
		pageState:          newPageState,
		conn:               nil,
		session:            q.session,
		routingInfo:        &queryRoutingInfo{},
	}
}

// Attempts returns the number of times the query was executed.
func (q *internalQuery) Attempts() int {
	return q.metrics.attempts()
}

func (q *internalQuery) attempt(keyspace string, end, start time.Time, iter *Iter, host *HostInfo) {
	latency := end.Sub(start)
	attempt := q.metrics.attempt(latency)

	if q.qryOpts.observer != nil {
		metricsForHost := q.hostMetricsManager.attempt(latency, host)
		q.qryOpts.observer.ObserveQuery(q.qryOpts.context, ObservedQuery{
			Keyspace:  keyspace,
			Statement: q.qryOpts.stmt,
			Values:    q.qryOpts.values,
			Start:     start,
			End:       end,
			Rows:      iter.numRows,
			Host:      host,
			Metrics:   metricsForHost,
			Err:       iter.err,
			Attempt:   attempt,
			Query:     q.originalQuery,
		})
	}
}

func (q *internalQuery) execute(ctx context.Context, conn *Conn) *Iter {
	return conn.executeQuery(ctx, q)
}

func (q *internalQuery) retryPolicy() RetryPolicy {
	return q.qryOpts.rt
}

func (q *internalQuery) speculativeExecutionPolicy() SpeculativeExecutionPolicy {
	return q.qryOpts.spec
}

func (q *internalQuery) GetRoutingKey() ([]byte, error) {
	if q.qryOpts.routingKey != nil {
		return q.qryOpts.routingKey, nil
	}

	if q.qryOpts.binding != nil && len(q.qryOpts.values) == 0 {
		// If this query was created using session.Bind we wont have the query
		// values yet, so we have to pass down to the next policy.
		// TODO: Remove this and handle this case
		return nil, nil
	}

	// try to determine the routing key
	routingKeyInfo, err := q.session.routingKeyInfo(q.Context(), q.qryOpts.stmt, q.qryOpts.keyspace)
	if err != nil {
		return nil, err
	}

	if routingKeyInfo != nil {
		q.routingInfo.mu.Lock()
		q.routingInfo.keyspace = routingKeyInfo.keyspace
		q.routingInfo.table = routingKeyInfo.table
		q.routingInfo.mu.Unlock()
	}
	return createRoutingKey(routingKeyInfo, q.qryOpts.values)
}

func (q *internalQuery) Keyspace() string {
	if q.qryOpts.getKeyspace != nil {
		return q.qryOpts.getKeyspace()
	}

	qrKs := q.routingInfo.getKeyspace()
	if qrKs != "" {
		return qrKs
	}
	if q.qryOpts.keyspace != "" {
		return q.qryOpts.keyspace
	}

	if q.session == nil {
		return ""
	}
	// TODO(chbannis): this should be parsed from the query or we should let
	// this be set by users.
	return q.session.cfg.Keyspace
}

func (q *internalQuery) Table() string {
	return q.routingInfo.getTable()
}

func (q *internalQuery) IsIdempotent() bool {
	return q.qryOpts.idempotent
}

func (q *internalQuery) getQueryMetrics() *queryMetrics {
	return q.metrics
}

func (q *internalQuery) SetConsistency(c Consistency) {
	atomic.StoreUint32(&q.consistency, uint32(c))
}

func (q *internalQuery) GetConsistency() Consistency {
	return Consistency(atomic.LoadUint32(&q.consistency))
}

func (q *internalQuery) Context() context.Context {
	return q.qryOpts.context
}

func (q *internalQuery) Statement() Statement {
	return q.originalQuery
}

func (q *internalQuery) GetHostID() string {
	return q.qryOpts.hostID
}

func (q *internalQuery) getRoutingInfo() *queryRoutingInfo {
	return q.routingInfo
}

func (q *internalQuery) getKeyspaceFunc() func() string {
	return q.qryOpts.getKeyspace
}

type batchOptions struct {
	trace    Tracer
	observer BatchObserver

	bType   BatchType
	entries []BatchEntry

	defaultTimestamp      bool
	defaultTimestampValue int64

	serialCons SerialConsistency

	customPayload map[string][]byte
	rt            RetryPolicy
	spec          SpeculativeExecutionPolicy
	context       context.Context
	keyspace      string
	idempotent    bool
	routingKey    []byte
	nowInSeconds  *int
}

func newBatchOptions(b *Batch, ctx context.Context) *batchOptions {
	// make a new array so if user keeps appending entries on the Batch object it doesn't affect this execution
	newEntries := make([]BatchEntry, len(b.Entries))
	for i, e := range b.Entries {
		newEntries[i] = e
	}
	var newRoutingKey []byte
	if b.routingKey != nil {
		routingKey := b.routingKey
		newRoutingKey = make([]byte, len(routingKey))
		copy(newRoutingKey, routingKey)
	}
	if ctx == nil {
		ctx = b.Context()
	}
	return &batchOptions{
		bType:                 b.Type,
		entries:               newEntries,
		customPayload:         b.CustomPayload,
		rt:                    b.rt,
		spec:                  b.spec,
		trace:                 b.trace,
		observer:              b.observer,
		serialCons:            b.serialCons,
		defaultTimestamp:      b.defaultTimestamp,
		defaultTimestampValue: b.defaultTimestampValue,
		context:               ctx,
		keyspace:              b.Keyspace(),
		idempotent:            b.IsIdempotent(),
		routingKey:            newRoutingKey,
		nowInSeconds:          b.nowInSeconds,
	}
}

type internalBatch struct {
	originalBatch      *Batch
	batchOpts          *batchOptions
	consistency        uint32
	routingInfo        *queryRoutingInfo
	session            *Session
	metrics            *queryMetrics
	hostMetricsManager hostMetricsManager
}

func newInternalBatch(batch *Batch, ctx context.Context) *internalBatch {
	var hostMetricsMgr hostMetricsManager
	if batch.observer != nil {
		hostMetricsMgr = newHostMetricsManager()
	} else {
		hostMetricsMgr = emptyHostMetricsManager
	}
	return &internalBatch{
		originalBatch:      batch,
		batchOpts:          newBatchOptions(batch, ctx),
		routingInfo:        &queryRoutingInfo{},
		session:            batch.session,
		consistency:        uint32(batch.GetConsistency()),
		metrics:            &queryMetrics{},
		hostMetricsManager: hostMetricsMgr,
	}
}

// Attempts returns the number of attempts made to execute the batch.
func (b *internalBatch) Attempts() int {
	return b.metrics.attempts()
}

func (b *internalBatch) attempt(keyspace string, end, start time.Time, iter *Iter, host *HostInfo) {
	latency := end.Sub(start)
	attempt := b.metrics.attempt(latency)

	if b.batchOpts.observer == nil {
		return
	}

	metricsForHost := b.hostMetricsManager.attempt(latency, host)

	statements := make([]string, len(b.batchOpts.entries))
	values := make([][]interface{}, len(b.batchOpts.entries))

	for i, entry := range b.batchOpts.entries {
		statements[i] = entry.Stmt
		values[i] = entry.Args
	}

	b.batchOpts.observer.ObserveBatch(b.batchOpts.context, ObservedBatch{
		Keyspace:   keyspace,
		Statements: statements,
		Values:     values,
		Start:      start,
		End:        end,
		// Rows not used in batch observations // TODO - might be able to support it when using BatchCAS
		Host:    host,
		Metrics: metricsForHost,
		Err:     iter.err,
		Attempt: attempt,
		Batch:   b.originalBatch,
	})
}

func (b *internalBatch) retryPolicy() RetryPolicy {
	return b.batchOpts.rt
}

func (b *internalBatch) speculativeExecutionPolicy() SpeculativeExecutionPolicy {
	return b.batchOpts.spec
}

func (b *internalBatch) GetRoutingKey() ([]byte, error) {
	if b.batchOpts.routingKey != nil {
		return b.batchOpts.routingKey, nil
	}

	if len(b.batchOpts.entries) == 0 {
		return nil, nil
	}

	entry := b.batchOpts.entries[0]
	if entry.binding != nil {
		// bindings do not have the values let's skip it like Query does.
		return nil, nil
	}
	// try to determine the routing key
	routingKeyInfo, err := b.session.routingKeyInfo(b.Context(), entry.Stmt, b.batchOpts.keyspace)
	if err != nil {
		return nil, err
	}

	if routingKeyInfo != nil {
		b.routingInfo.mu.Lock()
		b.routingInfo.keyspace = routingKeyInfo.keyspace
		b.routingInfo.table = routingKeyInfo.table
		b.routingInfo.mu.Unlock()
	}

	return createRoutingKey(routingKeyInfo, entry.Args)
}

func (b *internalBatch) Keyspace() string {
	return b.batchOpts.keyspace
}

func (b *internalBatch) Table() string {
	return b.routingInfo.getTable()
}

func (b *internalBatch) IsIdempotent() bool {
	return b.batchOpts.idempotent
}

func (b *internalBatch) getQueryMetrics() *queryMetrics {
	return b.metrics
}

func (b *internalBatch) SetConsistency(c Consistency) {
	atomic.StoreUint32(&b.consistency, uint32(c))
}

func (b *internalBatch) GetConsistency() Consistency {
	return Consistency(atomic.LoadUint32(&b.consistency))
}

func (b *internalBatch) Context() context.Context {
	return b.batchOpts.context
}

func (b *internalBatch) Statement() Statement {
	return b.originalBatch
}

func (b *internalBatch) GetHostID() string {
	return ""
}

func (b *internalBatch) getRoutingInfo() *queryRoutingInfo {
	return b.routingInfo
}

func (b *internalBatch) getKeyspaceFunc() func() string {
	return nil
}

func (b *internalBatch) execute(ctx context.Context, conn *Conn) *Iter {
	return conn.executeBatch(ctx, b)
}
