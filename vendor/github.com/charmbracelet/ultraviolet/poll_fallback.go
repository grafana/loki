package uv

import (
	"bufio"
	"io"
	"sync"
	"time"
)

// newFallbackReader creates a new fallbackReader for the given io.Reader.
func newFallbackReader(reader io.Reader) (pollReader, error) {
	return &fallbackReader{
		reader:     bufio.NewReader(reader),
		cancelChan: make(chan struct{}),
		dataChan:   make(chan struct{}, 1),
	}, nil
}

// fallbackReader implements pollReader using goroutines and buffered I/O.
// This is a fallback implementation that works on all platforms.
type fallbackReader struct {
	reader     *bufio.Reader
	cancelChan chan struct{}
	dataChan   chan struct{}
	mu         sync.Mutex
	canceled   bool
	started    bool
}

// Read reads data from the underlying reader.
func (r *fallbackReader) Read(p []byte) (int, error) {
	r.mu.Lock()
	if r.canceled {
		r.mu.Unlock()
		return 0, ErrCanceled
	}
	r.mu.Unlock()

	n, err := r.reader.Read(p)
	// If we get an error during a concurrent cancel, prefer ErrCanceled
	if err != nil {
		r.mu.Lock()
		if r.canceled {
			r.mu.Unlock()
			return 0, ErrCanceled
		}
		r.mu.Unlock()
	}
	return n, err
}

// Poll waits for data to be available to read with the given timeout.
// This implementation starts a background goroutine to check for buffered
// data availability.
func (r *fallbackReader) Poll(timeout time.Duration) (bool, error) {
	r.mu.Lock()
	if r.canceled {
		r.mu.Unlock()
		return false, ErrCanceled
	}

	// Start the background reader goroutine if not already started
	if !r.started {
		r.started = true
		go r.checkBuffered()
	}
	r.mu.Unlock()

	if timeout < 0 {
		// Wait indefinitely
		select {
		case <-r.dataChan:
			// Put it back for next poll/read
			select {
			case r.dataChan <- struct{}{}:
			default:
			}
			return true, nil
		case <-r.cancelChan:
			return false, ErrCanceled
		}
	}

	// Wait with timeout
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-r.dataChan:
		// Put it back for next poll/read
		select {
		case r.dataChan <- struct{}{}:
		default:
		}
		return true, nil
	case <-timer.C:
		return false, nil
	case <-r.cancelChan:
		return false, ErrCanceled
	}
}

// checkBuffered runs in a background goroutine to signal when data is available.
func (r *fallbackReader) checkBuffered() {
	for {
		select {
		case <-r.cancelChan:
			return
		default:
		}

		// Check if data is buffered
		r.mu.Lock()
		if r.canceled {
			r.mu.Unlock()
			return
		}
		r.mu.Unlock()

		// Peek at one byte to check if data is available
		// This will block until data arrives
		_, err := r.reader.Peek(1)
		if err != nil {
			// If error (including EOF), stop the goroutine
			return
		}

		// Signal that data is available
		select {
		case r.dataChan <- struct{}{}:
		case <-r.cancelChan:
			return
		}

		// Wait a bit before checking again to avoid busy loop
		time.Sleep(10 * time.Millisecond)
	}
}

// Cancel cancels any ongoing poll or read operations.
func (r *fallbackReader) Cancel() bool {
	r.mu.Lock()
	if r.canceled {
		r.mu.Unlock()
		return false
	}
	r.canceled = true
	r.mu.Unlock()

	close(r.cancelChan)
	return true
}

// Close closes the reader and releases any resources.
func (r *fallbackReader) Close() error {
	r.Cancel()
	return nil
}
