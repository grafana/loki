package testconn

import (
	"bytes"
	"errors"
	"net"
	"time"
)

func New(data []byte) *Conn {
	c := &Conn{
		data: bytes.Split(data, []byte("SPLIT\n")),
		done: make(chan struct{}),
		err:  make(chan error, 1),
	}
	return c
}

type Conn struct {
	data [][]byte
	// data         []byte
	done         chan struct{}
	err          chan error
	readDeadline *time.Timer
}

func (c *Conn) Read(b []byte) (int, error) {
	if len(c.data) == 0 {
		select {
		case <-c.done:
			return 0, errors.New("connection closed")
		case err := <-c.err:
			return 0, err
		}
	}
	time.Sleep(1 * time.Millisecond)
	n := copy(b, c.data[0])
	c.data = c.data[1:]
	return n, nil
}

func (c *Conn) Write(b []byte) (n int, err error) {
	return len(b), nil
}

func (c *Conn) Close() error {
	close(c.done)
	return nil
}

func (c *Conn) LocalAddr() net.Addr {
	return &net.TCPAddr{
		IP:   net.IP{127, 0, 0, 1},
		Port: 49706,
	}
}

func (c *Conn) RemoteAddr() net.Addr {
	return &net.TCPAddr{
		IP:   net.IP{127, 0, 0, 1},
		Port: 49706,
	}
}

func (c *Conn) SetDeadline(t time.Time) error {
	return nil
}

func (c *Conn) SetReadDeadline(t time.Time) error {
	if c.readDeadline != nil {
		c.readDeadline.Stop()
	}
	if t.IsZero() {
		return nil
	}
	c.readDeadline = time.AfterFunc(t.Sub(time.Now()), func() {
		select {
		case c.err <- errors.New("timeout"):
		case <-c.done:
		default:
		}
	})
	return nil
}

func (c *Conn) SetWriteDeadline(t time.Time) error {
	return nil
}
