//go:build solaris || darwin || freebsd || netbsd || openbsd || dragonfly
// +build solaris darwin freebsd netbsd openbsd dragonfly

package uv

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

// newSelectPollReader creates a new SelectReader for the given io.Reader.
func newSelectPollReader(reader io.Reader) (pollReader, error) {
	file, ok := reader.(File)
	if !ok || file.Fd() >= unix.FD_SETSIZE {
		return newFallbackReader(reader)
	}

	r := &selectReader{
		reader: reader,
		file:   file,
	}

	var err error
	r.cancelSignalReader, r.cancelSignalWriter, err = os.Pipe()
	if err != nil {
		return nil, err
	}

	return r, nil
}

// selectReader implements pollReader using the POSIX select API.
type selectReader struct {
	reader             io.Reader
	file               File
	cancelSignalReader *os.File
	cancelSignalWriter *os.File
	mu                 sync.Mutex
	canceled           bool
}

// Read reads data from the underlying reader.
func (r *selectReader) Read(p []byte) (int, error) {
	r.mu.Lock()
	if r.canceled {
		r.mu.Unlock()
		return 0, ErrCanceled
	}
	r.mu.Unlock()

	return r.reader.Read(p)
}

// Poll waits for data to be available to read with the given timeout.
func (r *selectReader) Poll(timeout time.Duration) (bool, error) {
	r.mu.Lock()
	if r.canceled {
		r.mu.Unlock()
		return false, ErrCanceled
	}
	r.mu.Unlock()

	for {
		readerFd := int(r.file.Fd())
		abortFd := int(r.cancelSignalReader.Fd())

		maxFd := readerFd
		if abortFd > maxFd {
			maxFd = abortFd
		}

		// this is a limitation of the select syscall
		if maxFd >= unix.FD_SETSIZE {
			return false, fmt.Errorf("cannot select on file descriptor %d which is larger than %d", maxFd, unix.FD_SETSIZE)
		}

		fdSet := &unix.FdSet{}
		fdSet.Set(readerFd)
		fdSet.Set(abortFd)

		var tv *unix.Timeval
		if timeout >= 0 {
			t := unix.NsecToTimeval(timeout.Nanoseconds())
			tv = &t
		}

		n, err := unix.Select(maxFd+1, fdSet, nil, nil, tv)
		if errors.Is(err, unix.EINTR) {
			continue // try again if the syscall was interrupted
		}

		if err != nil {
			return false, fmt.Errorf("select: %w", err)
		}

		if n == 0 {
			return false, nil // timeout
		}

		if fdSet.IsSet(abortFd) {
			// remove signal from pipe
			var b [1]byte
			_, readErr := r.cancelSignalReader.Read(b[:])
			if readErr != nil {
				return false, fmt.Errorf("reading cancel signal: %w", readErr)
			}
			return false, ErrCanceled
		}

		if fdSet.IsSet(readerFd) {
			return true, nil
		}

		return false, fmt.Errorf("select returned without setting a file descriptor")
	}
}

// Cancel cancels any ongoing poll or read operations.
func (r *selectReader) Cancel() bool {
	r.mu.Lock()
	r.canceled = true
	r.mu.Unlock()

	// send cancel signal
	_, err := r.cancelSignalWriter.Write([]byte{'c'})
	return err == nil
}

// Close closes the reader and releases any resources.
func (r *selectReader) Close() error {
	var errMsgs []string

	// close pipe
	err := r.cancelSignalWriter.Close()
	if err != nil {
		errMsgs = append(errMsgs, fmt.Sprintf("closing cancel signal writer: %v", err))
	}

	err = r.cancelSignalReader.Close()
	if err != nil {
		errMsgs = append(errMsgs, fmt.Sprintf("closing cancel signal reader: %v", err))
	}

	if len(errMsgs) > 0 {
		return fmt.Errorf("%s", strings.Join(errMsgs, ", "))
	}

	return nil
}
