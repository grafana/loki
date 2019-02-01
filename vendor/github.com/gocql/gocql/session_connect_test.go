package gocql

import (
	"context"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"
)

type OneConnTestServer struct {
	Err  error
	Addr net.IP
	Port int

	listener   net.Listener
	acceptChan chan struct{}
	mu         sync.Mutex
	closed     bool
}

func NewOneConnTestServer() (*OneConnTestServer, error) {
	lstn, err := net.Listen("tcp4", "localhost:0")
	if err != nil {
		return nil, err
	}
	addr, port := parseAddressPort(lstn.Addr().String())
	return &OneConnTestServer{
		listener:   lstn,
		acceptChan: make(chan struct{}),
		Addr:       addr,
		Port:       port,
	}, nil
}

func (c *OneConnTestServer) Accepted() chan struct{} {
	return c.acceptChan
}

func (c *OneConnTestServer) Close() {
	c.lockedClose()
}

func (c *OneConnTestServer) Serve() {
	conn, err := c.listener.Accept()
	c.Err = err
	if conn != nil {
		conn.Close()
	}
	c.lockedClose()
}

func (c *OneConnTestServer) lockedClose() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.closed {
		close(c.acceptChan)
		c.listener.Close()
		c.closed = true
	}
}

func parseAddressPort(hostPort string) (net.IP, int) {
	host, portStr, err := net.SplitHostPort(hostPort)
	if err != nil {
		return net.ParseIP(""), 0
	}
	port, _ := strconv.Atoi(portStr)
	return net.ParseIP(host), port
}

func testConnErrorHandler(t *testing.T) ConnErrorHandler {
	return connErrorHandlerFn(func(conn *Conn, err error, closed bool) {
		t.Errorf("in connection handler: %v", err)
	})
}

func assertConnectionEventually(t *testing.T, wait time.Duration, srvr *OneConnTestServer) {
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()

	select {
	case <-ctx.Done():
		if ctx.Err() != nil {
			t.Errorf("waiting for connection: %v", ctx.Err())
		}
	case <-srvr.Accepted():
		if srvr.Err != nil {
			t.Errorf("accepting connection: %v", srvr.Err)
		}
	}
}
