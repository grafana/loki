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
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

type eventDebouncer struct {
	name   string
	timer  *time.Timer
	mu     sync.Mutex
	events []frame

	callback func([]frame)
	quit     chan struct{}

	logger StructuredLogger
}

func newEventDebouncer(name string, eventHandler func([]frame), logger StructuredLogger) *eventDebouncer {
	e := &eventDebouncer{
		name:     name,
		quit:     make(chan struct{}),
		timer:    time.NewTimer(eventDebounceTime),
		callback: eventHandler,
		logger:   logger,
	}
	e.timer.Stop()
	go e.flusher()

	return e
}

func (e *eventDebouncer) stop() {
	e.quit <- struct{}{} // sync with flusher
	close(e.quit)
}

func (e *eventDebouncer) flusher() {
	for {
		select {
		case <-e.timer.C:
			e.mu.Lock()
			e.flush()
			e.mu.Unlock()
		case <-e.quit:
			return
		}
	}
}

const (
	eventBufferSize   = 1000
	eventDebounceTime = 1 * time.Second
)

// flush must be called with mu locked
func (e *eventDebouncer) flush() {
	if len(e.events) == 0 {
		return
	}

	// if the flush interval is faster than the callback then we will end up calling
	// the callback multiple times, probably a bad idea. In this case we could drop
	// frames?
	go e.callback(e.events)
	e.events = make([]frame, 0, eventBufferSize)
}

func (e *eventDebouncer) debounce(frame frame) {
	e.mu.Lock()
	e.timer.Reset(eventDebounceTime)

	// TODO: probably need a warning to track if this threshold is too low
	if len(e.events) < eventBufferSize {
		e.events = append(e.events, frame)
	} else {
		e.logger.Warning("Event buffer full, dropping event frame.",
			NewLogFieldString("event_name", e.name), NewLogFieldStringer("frame", frame))
	}

	e.mu.Unlock()
}

func (s *Session) handleEvent(framer *framer) {
	frame, err := framer.parseFrame()
	if err != nil {
		s.logger.Error("Unable to parse event frame.", NewLogFieldError("err", err))
		return
	}

	s.logger.Debug("Handling event frame.", NewLogFieldStringer("frame", frame))

	switch f := frame.(type) {
	case *schemaChangeKeyspace, *schemaChangeFunction,
		*schemaChangeTable, *schemaChangeAggregate, *schemaChangeType:
		s.schemaDescriber.debounceRefreshSchemaMetadata()
	case *topologyChangeEventFrame, *statusChangeEventFrame:
		s.nodeEvents.debounce(frame)
	default:
		s.logger.Error("Invalid event frame.",
			NewLogFieldString("frame_type", fmt.Sprintf("%T", f)), NewLogFieldStringer("frame", f))
	}
}

// handleNodeEvent handles inbound status and topology change events.
//
// Status events are debounced by host IP; only the latest event is processed.
//
// Topology events are debounced by performing a single full topology refresh
// whenever any topology event comes in.
//
// Processing topology change events before status change events ensures
// that a NEW_NODE event is not dropped in favor of a newer UP event (which
// would itself be dropped/ignored, as the node is not yet known).
func (s *Session) handleNodeEvent(frames []frame) {
	type nodeEvent struct {
		change string
		host   net.IP
		port   int
	}

	topologyEventReceived := false
	// status change events
	sEvents := make(map[string]*nodeEvent)

	for _, frame := range frames {
		switch f := frame.(type) {
		case *topologyChangeEventFrame:
			s.logger.Info("Received topology change event.",
				NewLogFieldString("frame", strings.Join([]string{f.change, "->", f.host.String(), ":", strconv.Itoa(f.port)}, "")))
			topologyEventReceived = true
		case *statusChangeEventFrame:
			event, ok := sEvents[f.host.String()]
			if !ok {
				event = &nodeEvent{change: f.change, host: f.host, port: f.port}
				sEvents[f.host.String()] = event
			}
			event.change = f.change
		}
	}

	if topologyEventReceived && !s.cfg.Events.DisableTopologyEvents {
		s.debounceRingRefresh()
	}

	for _, f := range sEvents {
		s.logger.Info("Dispatching status change event.",
			NewLogFieldString("frame", strings.Join([]string{f.change, "->", f.host.String(), ":", strconv.Itoa(f.port)}, "")))

		// ignore events we received if they were disabled
		// see https://github.com/apache/cassandra-gocql-driver/issues/1591
		switch f.change {
		case nodeStateChangeUp:
			if !s.cfg.Events.DisableNodeStatusEvents {
				s.handleNodeUp(f.host, f.port)
			}
		case nodeStateChangeDown:
			if !s.cfg.Events.DisableNodeStatusEvents {
				s.handleNodeDown(f.host, f.port)
			}
		}
	}
}

func (s *Session) handleNodeUp(eventIp net.IP, eventPort int) {
	s.logger.Info("Node is UP.",
		NewLogFieldStringer("event_ip", eventIp), NewLogFieldInt("event_port", eventPort))

	host, ok := s.ring.getHostByIP(eventIp.String())
	if !ok {
		s.debounceRingRefresh()
		return
	}

	if s.cfg.filterHost(host) {
		return
	}

	if d := host.Version().nodeUpDelay(); d > 0 {
		time.Sleep(d)
	}
	s.startPoolFill(host)
}

func (s *Session) startPoolFill(host *HostInfo) {
	// we let the pool call handleNodeConnected to change the host state
	s.pool.addHost(host)
	s.policy.AddHost(host)
}

func (s *Session) handleNodeConnected(host *HostInfo) {
	s.logger.Debug("Pool connected to node.",
		NewLogFieldIP("host_addr", host.ConnectAddress()), NewLogFieldInt("port", host.Port()), NewLogFieldString("host_id", host.HostID()))

	host.setState(NodeUp)

	if !s.cfg.filterHost(host) {
		s.policy.HostUp(host)
		s.hostListeners.OnHostUp(HostUpEvent{Host: host})
	}
}

func (s *Session) handleNodeDown(ip net.IP, port int) {
	s.logger.Warning("Node is DOWN.",
		NewLogFieldIP("host_addr", ip), NewLogFieldInt("port", port))

	host, ok := s.ring.getHostByIP(ip.String())
	if ok {
		host.setState(NodeDown)
		if s.cfg.filterHost(host) {
			return
		}

		s.policy.HostDown(host)
		hostID := host.HostID()
		s.pool.removeHost(hostID)
		s.hostListeners.OnHostDown(HostDownEvent{Host: host})
	}
}

const (
	nodeStateChangeUp   = "UP"
	nodeStateChangeDown = "DOWN"
)
