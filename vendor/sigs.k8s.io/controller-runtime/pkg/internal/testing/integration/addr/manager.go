package addr

import (
	"fmt"
	"net"
	"sync"
	"time"
)

const (
	portReserveTime   = 1 * time.Minute
	portConflictRetry = 100
)

type portCache struct {
	lock  sync.Mutex
	ports map[int]time.Time
}

func (c *portCache) add(port int) bool {
	c.lock.Lock()
	defer c.lock.Unlock()
	// remove outdated port
	for p, t := range c.ports {
		if time.Since(t) > portReserveTime {
			delete(c.ports, p)
		}
	}
	// try allocating new port
	if _, ok := c.ports[port]; ok {
		return false
	}
	c.ports[port] = time.Now()
	return true
}

var cache = &portCache{
	ports: make(map[int]time.Time),
}

func suggest() (port int, resolvedHost string, err error) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return
	}
	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return
	}
	port = l.Addr().(*net.TCPAddr).Port
	defer func() {
		err = l.Close()
	}()
	resolvedHost = addr.IP.String()
	return
}

// Suggest suggests an address a process can listen on. It returns
// a tuple consisting of a free port and the hostname resolved to its IP.
// It makes sure that new port allocated does not conflict with old ports
// allocated within 1 minute.
func Suggest() (port int, resolvedHost string, err error) {
	for i := 0; i < portConflictRetry; i++ {
		port, resolvedHost, err = suggest()
		if err != nil {
			return
		}
		if cache.add(port) {
			return
		}
	}
	err = fmt.Errorf("no free ports found after %d retries", portConflictRetry)
	return
}
