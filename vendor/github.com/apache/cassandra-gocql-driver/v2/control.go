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
	crand "crypto/rand"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

var (
	randr    *rand.Rand
	mutRandr sync.Mutex
)

func init() {
	b := make([]byte, 4)
	if _, err := crand.Read(b); err != nil {
		panic(fmt.Sprintf("unable to seed random number generator: %v", err))
	}

	randr = rand.New(rand.NewSource(int64(readInt(b))))
}

const (
	controlConnStarting = 0
	controlConnStarted  = 1
	controlConnClosing  = -1
)

// Ensure that the atomic variable is aligned to a 64bit boundary
// so that atomic operations can be applied on 32bit architectures.
type controlConn struct {
	state        int32
	reconnecting int32

	session *Session
	conn    atomic.Value

	retry RetryPolicy

	quit chan struct{}
}

func createControlConn(session *Session) *controlConn {
	control := &controlConn{
		session: session,
		quit:    make(chan struct{}),
		retry:   &SimpleRetryPolicy{NumRetries: 3},
	}

	control.conn.Store((*connHost)(nil))

	return control
}

func (c *controlConn) heartBeat() {
	if !atomic.CompareAndSwapInt32(&c.state, controlConnStarting, controlConnStarted) {
		return
	}

	sleepTime := 1 * time.Second
	timer := time.NewTimer(sleepTime)
	defer timer.Stop()

	for {
		timer.Reset(sleepTime)

		select {
		case <-c.quit:
			return
		case <-timer.C:
		}

		resp, err := c.writeFrame(&writeOptionsFrame{})
		if err != nil {
			c.session.logger.Debug("Control connection failed to send heartbeat.", NewLogFieldError("err", err))
			goto reconn
		}

		switch actualResp := resp.(type) {
		case *supportedFrame:
			// Everything ok
			sleepTime = 5 * time.Second
			continue
		case error:
			c.session.logger.Debug("Control connection heartbeat failed.", NewLogFieldError("err", actualResp))
			goto reconn
		default:
			c.session.logger.Error("Unknown frame in response to options.", NewLogFieldString("frame_type", fmt.Sprintf("%T", resp)))
		}

	reconn:
		// try to connect a bit faster
		sleepTime = 1 * time.Second
		c.reconnect()
		continue
	}
}

var hostLookupPreferV4 = os.Getenv("GOCQL_HOST_LOOKUP_PREFER_V4") == "true"

func hostInfo(addr string, defaultPort int) ([]*HostInfo, error) {
	var port int
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
		port = defaultPort
	} else {
		port, err = strconv.Atoi(portStr)
		if err != nil {
			return nil, err
		}
	}

	var hosts []*HostInfo

	// Check if host is a literal IP address
	if ip := net.ParseIP(host); ip != nil {
		h, err := NewHostInfoFromAddrPort(ip, port)
		if err != nil {
			return nil, err
		}
		hosts = append(hosts, h)
		return hosts, nil
	}

	// Look up host in DNS
	ips, err := LookupIP(host)
	if err != nil {
		return nil, err
	} else if len(ips) == 0 {
		return nil, fmt.Errorf("no IP's returned from DNS lookup for %q", addr)
	}

	// Filter to v4 addresses if any present
	if hostLookupPreferV4 {
		var preferredIPs []net.IP
		for _, v := range ips {
			if v4 := v.To4(); v4 != nil {
				preferredIPs = append(preferredIPs, v4)
			}
		}
		if len(preferredIPs) != 0 {
			ips = preferredIPs
		}
	}

	for _, ip := range ips {
		h, err := NewHostInfoFromAddrPort(ip, port)
		if err != nil {
			return nil, err
		}

		hosts = append(hosts, h)
	}

	return hosts, nil
}

func shuffleHosts(hosts []*HostInfo) []*HostInfo {
	shuffled := make([]*HostInfo, len(hosts))
	copy(shuffled, hosts)

	mutRandr.Lock()
	randr.Shuffle(len(hosts), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})
	mutRandr.Unlock()

	return shuffled
}

func (c *controlConn) discoverProtocol(hosts []*HostInfo) (int, error) {
	hosts = shuffleHosts(hosts)

	handler := connErrorHandlerFn(func(c *Conn, err error, closed bool) {
		// we should never get here, but if we do it means we connected to a
		// host successfully which means our attempted protocol version worked
		if !closed {
			c.Close()
		}
	})

	var err error
	var proto int
	for _, host := range hosts {
		proto, err = c.tryProtocolVersionsForHost(host, handler)
		if err == nil {
			return proto, nil
		}

		c.session.logger.Debug("Failed to discover protocol version for host.",
			NewLogFieldIP("host_addr", host.ConnectAddress()),
			NewLogFieldError("err", err))
	}

	return 0, err
}

func (c *controlConn) tryProtocolVersionsForHost(host *HostInfo, handler ConnErrorHandler) (int, error) {
	connCfg := *c.session.connCfg

	var triedVersions []int

	for proto := highestProtocolVersionSupported; proto >= lowestProtocolVersionSupported; proto-- {
		connCfg.ProtoVersion = proto

		conn, err := c.session.dial(c.session.ctx, host, &connCfg, handler)
		if conn != nil {
			conn.Close()
		}

		if err == nil {
			return proto, nil
		}

		var unsupportedErr *unsupportedProtocolVersionError
		if errors.As(err, &unsupportedErr) {
			// the host does not support this protocol version, try a lower version
			c.session.logger.Debug("Failed to connect to host during protocol negotiation.",
				NewLogFieldIP("host_addr", host.ConnectAddress()),
				NewLogFieldInt("proto_version", proto),
				NewLogFieldError("err", err))
			triedVersions = append(triedVersions, connCfg.ProtoVersion)
			continue
		}

		c.session.logger.Debug("Error connecting to host during protocol negotiation.",
			NewLogFieldIP("host_addr", host.ConnectAddress()),
			NewLogFieldError("err", err))
		return 0, err
	}

	return 0, fmt.Errorf("gocql: failed to discover protocol version for host %s, tried versions: %v", host.ConnectAddress(), triedVersions)
}

func (c *controlConn) connect(hosts []*HostInfo, sessionInit bool) error {
	if len(hosts) == 0 {
		return errors.New("control: no endpoints specified")
	}

	// shuffle endpoints so not all drivers will connect to the same initial
	// node.
	hosts = shuffleHosts(hosts)

	cfg := *c.session.connCfg
	cfg.disableCoalesce = true

	var conn *Conn
	var err error
	for _, host := range hosts {
		conn, err = c.session.dial(c.session.ctx, host, &cfg, c)
		if err != nil {
			c.session.logger.Info("Control connection failed to establish a connection to host.",
				NewLogFieldIP("host_addr", host.ConnectAddress()),
				NewLogFieldInt("port", host.Port()),
				NewLogFieldString("host_id", host.HostID()),
				NewLogFieldError("err", err))
			continue
		}
		err = c.setupConn(conn, sessionInit)
		if err == nil {
			break
		}
		c.session.logger.Info("Control connection setup failed after connecting to host.",
			NewLogFieldIP("host_addr", host.ConnectAddress()),
			NewLogFieldInt("port", host.Port()),
			NewLogFieldString("host_id", host.HostID()),
			NewLogFieldError("err", err))
		conn.Close()
		conn = nil
	}
	if conn == nil {
		return fmt.Errorf("unable to connect to initial hosts: %v", err)
	}

	// we could fetch the initial ring here and update initial host data. So that
	// when we return from here we have a ring topology ready to go.

	go c.heartBeat()

	return nil
}

type connHost struct {
	conn *Conn
	host *HostInfo
}

func (c *controlConn) setupConn(conn *Conn, sessionInit bool) error {
	// we need up-to-date host info for the filterHost call below
	iter := conn.querySystemLocal(context.TODO())
	host, err := c.session.hostInfoFromIter(iter, conn.host.ConnectAddress(), conn.r.RemoteAddr().(*net.TCPAddr).Port)
	if err != nil {
		// just cleanup
		iter.Close()
		return fmt.Errorf("could not retrieve control host info: %w", err)
	}
	if host == nil {
		return errors.New("could not retrieve control host info: query returned 0 rows")
	}

	var exists bool
	host, exists = c.session.ring.addOrUpdate(host)

	if c.session.cfg.filterHost(host) {
		return fmt.Errorf("host was filtered: %v (%s)", host.ConnectAddress(), host.HostID())
	}

	if !exists {
		logLevel := LogLevelInfo
		msg := "Added control host."
		if sessionInit {
			logLevel = LogLevelDebug
			msg = "Added control host (session initialization)."
		}
		logHelper(c.session.logger, logLevel, msg,
			NewLogFieldIP("host_addr", host.ConnectAddress()), NewLogFieldString("host_id", host.HostID()))
	}

	if err := c.registerEvents(conn); err != nil {
		return fmt.Errorf("register events: %v", err)
	}

	ch := &connHost{
		conn: conn,
		host: host,
	}

	c.conn.Store(ch)

	c.session.logger.Info("Control connection connected to host.",
		NewLogFieldIP("host_addr", host.ConnectAddress()), NewLogFieldString("host_id", host.HostID()))

	if c.session.initialized() {
		refreshErr := c.session.schemaDescriber.refreshSchemaMetadata()
		if refreshErr != nil {
			c.session.logger.Warning("Failed to refresh schema metadata after reconnecting. "+
				"Schema might be stale or missing, causing token-aware routing to fall back to the configured fallback policy. "+
				"Keyspace metadata queries might fail with ErrKeyspaceDoesNotExist until schema refresh succeeds.",
				NewLogFieldError("err", refreshErr))
		}
		// We connected to control conn, so add the connect the host in pool as well.
		// Notify session we can start trying to connect to the node.
		// We can't start the fill before the session is initialized, otherwise the fill would interfere
		// with the fill called by Session.init. Session.init needs to wait for its fill to finish and that
		// would return immediately if we started the fill here.
		// TODO(martin-sucha): Trigger pool refill for all hosts, like in reconnectDownedHosts?
		go c.session.startPoolFill(host)
	}
	return nil
}

func (c *controlConn) registerEvents(conn *Conn) error {
	var events []string

	if !c.session.cfg.Events.DisableTopologyEvents {
		events = append(events, "TOPOLOGY_CHANGE")
	}
	if !c.session.cfg.Events.DisableNodeStatusEvents {
		events = append(events, "STATUS_CHANGE")
	}
	if !c.session.cfg.Events.DisableSchemaEvents {
		events = append(events, "SCHEMA_CHANGE")
	}

	if len(events) == 0 {
		return nil
	}

	framer, err := conn.exec(context.Background(),
		&writeRegisterFrame{
			events: events,
		}, nil)
	if err != nil {
		return err
	}

	frame, err := framer.parseFrame()
	if err != nil {
		return err
	} else if _, ok := frame.(*readyFrame); !ok {
		return fmt.Errorf("unexpected frame in response to register: got %T: %v", frame, frame)
	}

	return nil
}

func (c *controlConn) reconnect() {
	if atomic.LoadInt32(&c.state) == controlConnClosing {
		return
	}
	if !atomic.CompareAndSwapInt32(&c.reconnecting, 0, 1) {
		return
	}
	defer atomic.StoreInt32(&c.reconnecting, 0)

	_, err := c.attemptReconnect()

	if err != nil {
		c.session.logger.Error("Unable to reconnect control connection.",
			NewLogFieldError("err", err))
		return
	}

	err = c.session.refreshRing()
	if err != nil {
		c.session.logger.Warning("Unable to refresh ring.",
			NewLogFieldError("err", err))
	}
}

func (c *controlConn) attemptReconnect() (*Conn, error) {

	c.session.logger.Debug("Reconnecting the control connection.")

	hosts := c.session.ring.allHosts()
	hosts = shuffleHosts(hosts)

	// keep the old behavior of connecting to the old host first by moving it to
	// the front of the slice
	ch := c.getConn()
	if ch != nil {
		for i := range hosts {
			if hosts[i].Equal(ch.host) {
				hosts[0], hosts[i] = hosts[i], hosts[0]
				break
			}
		}
		ch.conn.Close()
	}

	conn, err := c.attemptReconnectToAnyOfHosts(hosts)

	if conn != nil {
		return conn, err
	}

	c.session.logger.Error("Unable to connect to any ring node, control connection falling back to initial contact points.", NewLogFieldError("err", err))
	// Fallback to initial contact points, as it may be the case that all known initialHosts
	// changed their IPs while keeping the same hostname(s).
	initialHosts, resolvErr := addrsToHosts(c.session.cfg.Hosts, c.session.cfg.Port, c.session.logger)
	if resolvErr != nil {
		return nil, fmt.Errorf("resolve contact points' hostnames: %v", resolvErr)
	}

	return c.attemptReconnectToAnyOfHosts(initialHosts)
}

func (c *controlConn) attemptReconnectToAnyOfHosts(hosts []*HostInfo) (*Conn, error) {
	var conn *Conn
	var err error
	for _, host := range hosts {
		conn, err = c.session.connect(c.session.ctx, host, c)
		if err != nil {
			c.session.logger.Info("During reconnection, control connection failed to establish a connection to host.",
				NewLogFieldIP("host_addr", host.ConnectAddress()),
				NewLogFieldInt("port", host.Port()),
				NewLogFieldString("host_id", host.HostID()),
				NewLogFieldError("err", err))
			continue
		}
		err = c.setupConn(conn, false)
		if err == nil {
			break
		}
		c.session.logger.Info("During reconnection, control connection setup failed after connecting to host.",
			NewLogFieldIP("host_addr", host.ConnectAddress()),
			NewLogFieldInt("port", host.Port()),
			NewLogFieldString("host_id", host.HostID()),
			NewLogFieldError("err", err))
		conn.Close()
		conn = nil
	}
	return conn, err
}

func (c *controlConn) HandleError(conn *Conn, err error, closed bool) {
	if !closed {
		return
	}

	oldConn := c.getConn()

	// If connection has long gone, and not been attempted for awhile,
	// it's possible to have oldConn as nil here (#1297).
	if oldConn != nil && oldConn.conn != conn {
		return
	}

	c.session.logger.Warning("Control connection error.",
		NewLogFieldIP("host_addr", conn.host.ConnectAddress()),
		NewLogFieldString("host_id", conn.host.HostID()),
		NewLogFieldError("err", err))

	c.reconnect()
}

func (c *controlConn) getConn() *connHost {
	return c.conn.Load().(*connHost)
}

func (c *controlConn) writeFrame(w frameBuilder) (frame, error) {
	ch := c.getConn()
	if ch == nil {
		return nil, errNoControl
	}

	framer, err := ch.conn.exec(context.Background(), w, nil)
	if err != nil {
		return nil, err
	}

	return framer.parseFrame()
}

func (c *controlConn) withConnHost(fn func(*connHost) *Iter) *Iter {
	const maxConnectAttempts = 5
	connectAttempts := 0

	for i := 0; i < maxConnectAttempts; i++ {
		ch := c.getConn()
		if ch == nil {
			if connectAttempts > maxConnectAttempts {
				break
			}

			connectAttempts++

			c.reconnect()
			continue
		}

		return fn(ch)
	}

	return newErrIter(errNoControl, &queryMetrics{}, "", nil, nil)
}

func (c *controlConn) withConn(fn func(*Conn) *Iter) *Iter {
	return c.withConnHost(func(ch *connHost) *Iter {
		return fn(ch.conn)
	})
}

// query will return nil if the connection is closed or nil
func (c *controlConn) query(statement string, values ...interface{}) (iter *Iter) {
	q := c.session.Query(statement, values...).Consistency(One).RoutingKey([]byte{}).Trace(nil)
	qry := newInternalQuery(q, context.TODO())

	for {
		iter = c.withConn(func(conn *Conn) *Iter {
			qry.conn = conn
			return conn.executeQuery(qry.Context(), qry)
		})

		if iter.err != nil {
			c.session.logger.Warning("Error executing control connection statement.",
				NewLogFieldString("statement", statement), NewLogFieldError("err", iter.err))
		}

		qry.metrics.attempt(0)
		qry.hostMetricsManager.attempt(0, c.getConn().host)
		if iter.err == nil || !c.retry.Attempt(qry) {
			break
		}
	}

	return
}

func (c *controlConn) awaitSchemaAgreement() error {
	return c.withConn(func(conn *Conn) *Iter {
		return newErrIter(conn.awaitSchemaAgreement(context.TODO()), &queryMetrics{}, "", nil, nil)
	}).err
}

func (c *controlConn) awaitSchemaAgreementWithTimeout(timeout time.Duration) error {
	return c.withConn(func(conn *Conn) *Iter {
		return newErrIter(conn.awaitSchemaAgreementWithTimeout(context.TODO(), timeout), &queryMetrics{}, "", nil, nil)
	}).err
}

func (c *controlConn) close() {
	if atomic.CompareAndSwapInt32(&c.state, controlConnStarted, controlConnClosing) {
		c.quit <- struct{}{}
	}

	ch := c.getConn()
	if ch != nil {
		ch.conn.Close()
	}
}

var errNoControl = errors.New("gocql: no control connection available")
