package gossiphttp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"
)

// streamClient is implemented by http2Stream.
type streamClient interface {
	Send([]byte) error
	Recv() ([]byte, error)
}

// packetsClientConn implements a bidirectional communication channel with a
// remote peer exchanging gossiphttp messages.
type packetsClientConn struct {
	cli     streamClient
	onClose func()
	closed  chan struct{}
	metrics *metrics

	localAddr, remoteAddr net.Addr

	readCnd           *sync.Cond         // Used to wake sleeping readers.
	spawnReader       sync.Once          // Used to lazily launch a cli reader.
	readMessages      chan readResult    // Messages read from the cli reader.
	readTimeout       time.Time          // Read deadline.
	readTimeoutCancel context.CancelFunc // Cancels read deadline and wakes up goroutines.
	readBuffer        bytes.Buffer       // Data buffer ready for immediate reading.

	writeMut sync.Mutex
}

type readResult struct {
	Message []byte
	Error   error
}

func (c *packetsClientConn) Read(b []byte) (n int, err error) {
	defer func() {
		c.metrics.streamRxTotal.Inc()
		c.metrics.streamRxBytesTotal.Add(float64(n))
		if err != nil {
			c.metrics.streamRxFailedTotal.Inc()
		}
	}()

	// Lazily spawn a background goroutine to reaed from our stream client.
	c.spawnReader.Do(func() {
		go func() {
			defer close(c.readMessages)

			for {
				msg, err := c.cli.Recv()
				c.readCnd.Broadcast() // Wake up sleeping goroutines.

				res := readResult{Message: msg, Error: err}
				select {
				case c.readMessages <- res:
				case <-c.closed:
					return
				}

				if err != nil {
					return
				}
			}
		}()
	})

	for n == 0 {
		n2, err := c.readOrBlock(b)
		if err != nil {
			return n2, err
		}
		n += n2
	}
	return n, nil
}

func (c *packetsClientConn) readOrBlock(b []byte) (n int, err error) {
	c.readCnd.L.Lock()
	defer c.readCnd.L.Unlock()
	if !c.readTimeout.IsZero() && !time.Now().Before(c.readTimeout) {
		return 0, os.ErrDeadlineExceeded
	}

	// Read from the existing buffer first.
	n, err = c.readBuffer.Read(b)
	if err != nil && !errors.Is(err, io.EOF) {
		return
	} else if n != 0 {
		return
	}

	// We've emptied our buffer. Pull the next message in or wait for a message
	// to be available.
	select {
	case msg, ok := <-c.readMessages:
		if !ok {
			return n, io.EOF
		}
		switch {
		case msg.Error != nil:
			return n, msg.Error
		case msg.Message == nil:
			return n, fmt.Errorf("nil message")
		default:
			_, err = c.readBuffer.Write(msg.Message)
			return n, err
		}
	default:
		c.readCnd.Wait() // Wait for something to be written or for the timeout to fire.
		return 0, nil
	}
}

func (c *packetsClientConn) Write(b []byte) (n int, err error) {
	defer func() {
		c.metrics.streamTxTotal.Inc()
		c.metrics.streamTxBytesTotal.Add(float64(n))
		if err != nil {
			c.metrics.streamTxFailedTotal.Inc()
		}
	}()

	c.writeMut.Lock()
	defer c.writeMut.Unlock()

	err = c.cli.Send(b)
	if err != nil {
		return 0, err
	}
	return len(b), nil
}

func (c *packetsClientConn) Close() error {
	c.writeMut.Lock()
	defer c.writeMut.Unlock()

	select {
	case <-c.closed:
		// no-op: already closed
		return nil
	default:
		close(c.closed)

		if c.onClose != nil {
			c.onClose()
		}

		return c.cli.(*http2Stream).r.Close()
	}
}

func (c *packetsClientConn) LocalAddr() net.Addr {
	return c.localAddr
}

func (c *packetsClientConn) RemoteAddr() net.Addr {
	return c.remoteAddr
}

func (c *packetsClientConn) SetDeadline(t time.Time) error {
	return c.SetReadDeadline(t)
}

func (c *packetsClientConn) SetReadDeadline(t time.Time) error {
	c.readCnd.L.Lock()
	defer c.readCnd.L.Unlock()

	c.readTimeout = t

	// There should only be one deadline goroutine at a time, so cancel it if it
	// already exists.
	if c.readTimeoutCancel != nil {
		c.readTimeoutCancel()
		c.readTimeoutCancel = nil
	}
	c.readTimeoutCancel = c.deadlineTimer(t)
	return nil
}

func (c *packetsClientConn) deadlineTimer(t time.Time) context.CancelFunc {
	if t.IsZero() {
		// Deadline of zero means to wait forever.
		return nil
	}
	if t.Before(time.Now()) {
		c.readCnd.Broadcast()
	}
	ctx, cancel := context.WithDeadline(context.Background(), t)
	go func() {
		<-ctx.Done()
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			c.readCnd.Broadcast()
		}
	}()
	return cancel
}

func (c *packetsClientConn) SetWriteDeadline(t time.Time) error {
	// no-op: writes finish immediately
	return nil
}
