package gocql

import (
	"net"
	"sync"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
)

type eventDebouncer struct {
	logger log.Logger

	name   string
	timer  *time.Timer
	mu     sync.Mutex
	events []frame

	callback func([]frame)
	quit     chan struct{}
}

func newEventDebouncer(logger log.Logger, name string, eventHandler func([]frame)) *eventDebouncer {
	e := &eventDebouncer{
		logger:   logger,
		name:     name,
		quit:     make(chan struct{}),
		timer:    time.NewTimer(eventDebounceTime),
		callback: eventHandler,
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
		level.Info(e.logger).Log("msg", "buffer full, dropping event frame", "name", e.name, "frame", frame)
	}

	e.mu.Unlock()
}

func (s *Session) handleEvent(framer *framer) {
	frame, err := framer.parseFrame()
	if err != nil {
		level.Error(s.logger).Log("msg", "unable to parse event frame", "error", err)
		return
	}

	level.Debug(s.logger).Log("msg", "handling frame", "frame", frame)
	switch f := frame.(type) {
	case *schemaChangeKeyspace, *schemaChangeFunction,
		*schemaChangeTable, *schemaChangeAggregate, *schemaChangeType:

		s.schemaEvents.debounce(frame)
	case *topologyChangeEventFrame, *statusChangeEventFrame:
		s.nodeEvents.debounce(frame)
	default:
		level.Error(s.logger).Log("msg", "invalid event frame", "frame", f)
	}
}

func (s *Session) handleSchemaEvent(frames []frame) {
	// TODO: debounce events
	for _, frame := range frames {
		switch f := frame.(type) {
		case *schemaChangeKeyspace:
			s.schemaDescriber.clearSchema(f.keyspace)
			s.handleKeyspaceChange(f.keyspace, f.change)
		case *schemaChangeTable:
			s.schemaDescriber.clearSchema(f.keyspace)
		case *schemaChangeAggregate:
			s.schemaDescriber.clearSchema(f.keyspace)
		case *schemaChangeFunction:
			s.schemaDescriber.clearSchema(f.keyspace)
		case *schemaChangeType:
			s.schemaDescriber.clearSchema(f.keyspace)
		}
	}
}

func (s *Session) handleKeyspaceChange(keyspace, change string) {
	s.control.awaitSchemaAgreement()
	s.policy.KeyspaceChanged(KeyspaceUpdateEvent{Keyspace: keyspace, Change: change})
}

func (s *Session) handleNodeEvent(frames []frame) {
	type nodeEvent struct {
		change string
		host   net.IP
		port   int
	}

	events := make(map[string]*nodeEvent)

	for _, frame := range frames {
		// TODO: can we be sure the order of events in the buffer is correct?
		switch f := frame.(type) {
		case *topologyChangeEventFrame:
			event, ok := events[f.host.String()]
			if !ok {
				event = &nodeEvent{change: f.change, host: f.host, port: f.port}
				events[f.host.String()] = event
			}
			event.change = f.change

		case *statusChangeEventFrame:
			event, ok := events[f.host.String()]
			if !ok {
				event = &nodeEvent{change: f.change, host: f.host, port: f.port}
				events[f.host.String()] = event
			}
			event.change = f.change
		}
	}

	for _, f := range events {
		level.Debug(s.logger).Log("msg", "dispatching event", "event", f)

		switch f.change {
		case "NEW_NODE":
			s.handleNewNode(f.host, f.port, true)
		case "REMOVED_NODE":
			s.handleRemovedNode(f.host, f.port)
		case "MOVED_NODE":
		// java-driver handles this, not mentioned in the spec
		// TODO(zariel): refresh token map
		case "UP":
			s.handleNodeUp(f.host, f.port, true)
		case "DOWN":
			s.handleNodeDown(f.host, f.port)
		}
	}
}

func (s *Session) addNewNode(host *HostInfo) {
	if s.cfg.filterHost(host) {
		return
	}

	host.setState(NodeUp)
	s.pool.addHost(host)
	s.policy.AddHost(host)
}

func (s *Session) handleNewNode(ip net.IP, port int, waitForBinary bool) {
	level.Info(s.logger).Log("msg", "Session.handleNewNode", "ip", ip.String(), "port", port)

	ip, port = s.cfg.translateAddressPort(ip, port)

	// Get host info and apply any filters to the host
	hostInfo, err := s.hostSource.getHostInfo(ip, port)
	if err != nil {
		level.Error(s.logger).Log("msg", "events: unable to fetch host info", "ip", ip.String(), "port", port, "error", err)
		return
	} else if hostInfo == nil {
		// If hostInfo is nil, this host was filtered out by cfg.HostFilter
		return
	}

	if t := hostInfo.Version().nodeUpDelay(); t > 0 && waitForBinary {
		time.Sleep(t)
	}

	// should this handle token moving?
	hostInfo = s.ring.addOrUpdate(hostInfo)

	s.addNewNode(hostInfo)

	if s.control != nil && !s.cfg.IgnorePeerAddr {
		// TODO(zariel): debounce ring refresh
		s.hostSource.refreshRing()
	}
}

func (s *Session) handleRemovedNode(ip net.IP, port int) {
	level.Debug(s.logger).Log("msg", "Session.handleRemovedNode", "ip", ip.String(), "port", port)

	ip, port = s.cfg.translateAddressPort(ip, port)

	// we remove all nodes but only add ones which pass the filter
	host := s.ring.getHost(ip)
	if host == nil {
		host = &HostInfo{connectAddress: ip, port: port}
	}

	if s.cfg.HostFilter != nil && !s.cfg.HostFilter.Accept(host) {
		return
	}

	host.setState(NodeDown)
	s.policy.RemoveHost(host)
	s.pool.removeHost(ip)
	s.ring.removeHost(ip)

	if !s.cfg.IgnorePeerAddr {
		s.hostSource.refreshRing()
	}
}

func (s *Session) handleNodeUp(eventIp net.IP, eventPort int, waitForBinary bool) {
	level.Info(s.logger).Log("msg", "Session.handleNodeUp", "ip", eventIp.String(), "port", eventPort)

	ip, _ := s.cfg.translateAddressPort(eventIp, eventPort)

	host := s.ring.getHost(ip)
	if host == nil {
		// TODO(zariel): avoid the need to translate twice in this
		// case
		s.handleNewNode(eventIp, eventPort, waitForBinary)
		return
	}

	if s.cfg.HostFilter != nil && !s.cfg.HostFilter.Accept(host) {
		return
	}

	if t := host.Version().nodeUpDelay(); t > 0 && waitForBinary {
		time.Sleep(t)
	}

	s.addNewNode(host)
}

func (s *Session) handleNodeDown(ip net.IP, port int) {
	level.Info(s.logger).Log("msg", "Session.handleNodeDown", "ip", ip.String(), "port", port)

	host := s.ring.getHost(ip)
	if host == nil {
		host = &HostInfo{connectAddress: ip, port: port}
	}

	if s.cfg.HostFilter != nil && !s.cfg.HostFilter.Accept(host) {
		return
	}

	host.setState(NodeDown)
	s.policy.HostDown(host)
	s.pool.hostDown(ip)
}
