//go:build linux
// +build linux

package uv

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

// newPollReader creates a new pollReader for the given io.Reader.
func newPollReader(reader io.Reader) (pollReader, error) {
	file, ok := reader.(File)
	if !ok {
		return newFallbackReader(reader)
	}

	epoll, err := unix.EpollCreate1(0)
	if err != nil {
		return nil, fmt.Errorf("create epoll: %w", err)
	}

	r := &epollReader{
		reader: reader,
		file:   file,
		epoll:  epoll,
	}

	r.cancelSignalReader, r.cancelSignalWriter, err = os.Pipe()
	if err != nil {
		_ = unix.Close(epoll)
		return nil, err
	}

	err = unix.EpollCtl(epoll, unix.EPOLL_CTL_ADD, int(file.Fd()), &unix.EpollEvent{
		Events: unix.EPOLLIN,
		Fd:     int32(file.Fd()),
	})
	if err != nil {
		_ = unix.Close(epoll)
		_ = r.cancelSignalReader.Close()
		_ = r.cancelSignalWriter.Close()
		return nil, fmt.Errorf("add reader to epoll interest list: %w", err)
	}

	err = unix.EpollCtl(epoll, unix.EPOLL_CTL_ADD, int(r.cancelSignalReader.Fd()), &unix.EpollEvent{
		Events: unix.EPOLLIN,
		Fd:     int32(r.cancelSignalReader.Fd()),
	})
	if err != nil {
		_ = unix.Close(epoll)
		_ = r.cancelSignalReader.Close()
		_ = r.cancelSignalWriter.Close()
		return nil, fmt.Errorf("add cancel signal to epoll interest list: %w", err)
	}

	return r, nil
}

// epollReader implements pollReader using the Linux epoll API.
type epollReader struct {
	reader             io.Reader
	file               File
	cancelSignalReader *os.File
	cancelSignalWriter *os.File
	epoll              int
	mu                 sync.Mutex
	canceled           bool
}

// Read reads data from the underlying reader.
func (r *epollReader) Read(p []byte) (int, error) {
	r.mu.Lock()
	if r.canceled {
		r.mu.Unlock()
		return 0, ErrCanceled
	}
	r.mu.Unlock()

	return r.reader.Read(p)
}

// Poll waits for data to be available to read with the given timeout.
func (r *epollReader) Poll(timeout time.Duration) (bool, error) {
	r.mu.Lock()
	if r.canceled {
		r.mu.Unlock()
		return false, ErrCanceled
	}
	r.mu.Unlock()

	events := make([]unix.EpollEvent, 1)

	timeoutMs := -1
	if timeout >= 0 {
		timeoutMs = int(timeout.Milliseconds())
	}

	for {
		n, err := unix.EpollWait(r.epoll, events, timeoutMs)
		if errors.Is(err, unix.EINTR) {
			continue // try again if the syscall was interrupted
		}

		if err != nil {
			return false, fmt.Errorf("epoll wait: %w", err)
		}

		if n == 0 {
			return false, nil // timeout
		}

		break
	}

	switch events[0].Fd {
	case int32(r.file.Fd()):
		return true, nil
	case int32(r.cancelSignalReader.Fd()):
		// remove signal from pipe
		var b [1]byte
		_, readErr := r.cancelSignalReader.Read(b[:])
		if readErr != nil {
			return false, fmt.Errorf("reading cancel signal: %w", readErr)
		}
		return false, ErrCanceled
	}

	return false, fmt.Errorf("unknown error")
}

// Cancel cancels any ongoing poll or read operations.
func (r *epollReader) Cancel() bool {
	r.mu.Lock()
	r.canceled = true
	r.mu.Unlock()

	// send cancel signal
	_, err := r.cancelSignalWriter.Write([]byte{'c'})
	return err == nil
}

// Close closes the reader and releases any resources.
func (r *epollReader) Close() error {
	var errMsgs []error

	// close epoll
	err := unix.Close(r.epoll)
	if err != nil {
		errMsgs = append(errMsgs, fmt.Errorf("closing epoll: %w", err))
	}

	// close pipe
	err = r.cancelSignalWriter.Close()
	if err != nil {
		errMsgs = append(errMsgs, fmt.Errorf("closing cancel signal writer: %w", err))
	}

	err = r.cancelSignalReader.Close()
	if err != nil {
		errMsgs = append(errMsgs, fmt.Errorf("closing cancel signal reader: %w", err))
	}

	if len(errMsgs) > 0 {
		return errors.Join(errMsgs...)
	}

	return nil
}
