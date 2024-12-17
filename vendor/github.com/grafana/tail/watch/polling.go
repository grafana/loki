// Copyright (c) 2015 HPE Software Inc. All rights reserved.
// Copyright (c) 2013 ActiveState Software Inc. All rights reserved.

package watch

import (
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/grafana/tail/util"
	"gopkg.in/tomb.v1"
)

// PollingFileWatcher polls the file for changes.
type PollingFileWatcher struct {
	File     *os.File
	Filename string
	Size     int64
	Options  PollingFileWatcherOptions
}

// PollingFileWatcherOptions customizes a PollingFileWatcher.
type PollingFileWatcherOptions struct {
	// MinPollFrequency and MaxPollFrequency specify how frequently a
	// PollingFileWatcher should poll the file.
	//
	// PollingFileWatcher starts polling at MinPollFrequency, and will
	// exponentially increase the polling frequency up to MaxPollFrequency if no
	// new entries are found. The polling frequency is reset to MinPollFrequency
	// whenever a new log entry is found or if the polled file changes.
	MinPollFrequency, MaxPollFrequency time.Duration
}

// DefaultPollingFileWatcherOptions holds default values for
// PollingFileWatcherOptions.
var DefaultPollingFileWatcherOptions = PollingFileWatcherOptions{
	MinPollFrequency: 250 * time.Millisecond,
	MaxPollFrequency: 250 * time.Millisecond,
}

func NewPollingFileWatcher(filename string, opts PollingFileWatcherOptions) (*PollingFileWatcher, error) {
	if opts == (PollingFileWatcherOptions{}) {
		opts = DefaultPollingFileWatcherOptions
	}

	if opts.MinPollFrequency == 0 || opts.MaxPollFrequency == 0 {
		return nil, fmt.Errorf("MinPollFrequency and MaxPollFrequency must be greater than 0")
	} else if opts.MaxPollFrequency < opts.MinPollFrequency {
		return nil, fmt.Errorf("MaxPollFrequency must be larger than MinPollFrequency")
	}

	return &PollingFileWatcher{
		File:     nil,
		Filename: filename,
		Size:     0,
		Options:  opts,
	}, nil
}

func (fw *PollingFileWatcher) BlockUntilExists(t *tomb.Tomb) error {
	bo := newPollBackoff(fw.Options)

	for {
		if _, err := os.Stat(fw.Filename); err == nil {
			return nil
		} else if !os.IsNotExist(err) {
			return err
		}
		select {
		case <-time.After(bo.WaitTime()):
			bo.Backoff()
			continue
		case <-t.Dying():
			return tomb.ErrDying
		}
	}
	panic("unreachable")
}

func (fw *PollingFileWatcher) ChangeEvents(t *tomb.Tomb, pos int64) (*FileChanges, error) {
	origFi, err := os.Stat(fw.Filename)
	if err != nil {
		return nil, err
	}

	changes := NewFileChanges()
	var prevModTime time.Time

	// XXX: use tomb.Tomb to cleanly manage these goroutines. replace
	// the fatal (below) with tomb's Kill.

	fw.Size = pos

	bo := newPollBackoff(fw.Options)

	go func() {
		prevSize := fw.Size
		for {
			select {
			case <-t.Dying():
				return
			default:
			}

			time.Sleep(bo.WaitTime())
			deletePending, err := IsDeletePending(fw.File)

			// DeletePending is a windows state where the file has been queued
			// for delete but won't actually get deleted until all handles are
			// closed. It's a variation on the NotifyDeleted call below.
			//
			// IsDeletePending may fail in cases where the file handle becomes
			// invalid, so we treat a failed call the same as a pending delete.
			if err != nil || deletePending {
				fw.closeFile()
				changes.NotifyDeleted()
				return
			}

			fi, err := os.Stat(fw.Filename)
			if err != nil {
				// Windows cannot delete a file if a handle is still open (tail keeps one open)
				// so it gives access denied to anything trying to read it until all handles are released.
				if os.IsNotExist(err) || (runtime.GOOS == "windows" && os.IsPermission(err)) {
					// File does not exist (has been deleted).
					changes.NotifyDeleted()
					return
				}

				// XXX: report this error back to the user
				util.Fatal("Failed to stat file %v: %v", fw.Filename, err)
			}

			// File got moved/renamed?
			if !os.SameFile(origFi, fi) {
				changes.NotifyDeleted()
				return
			}

			// File got truncated?
			fw.Size = fi.Size()
			if prevSize > 0 && prevSize > fw.Size {
				changes.NotifyTruncated()
				prevSize = fw.Size
				bo.Reset()
				continue
			}
			// File got bigger?
			if prevSize > 0 && prevSize < fw.Size {
				changes.NotifyModified()
				prevSize = fw.Size
				bo.Reset()
				continue
			}
			prevSize = fw.Size

			// File was appended to (changed)?
			modTime := fi.ModTime()
			if modTime != prevModTime {
				prevModTime = modTime
				changes.NotifyModified()
				bo.Reset()
				continue
			}

			// File hasn't changed; increase backoff for next sleep.
			bo.Backoff()
		}
	}()

	return changes, nil
}

func (fw *PollingFileWatcher) SetFile(f *os.File) {
	fw.File = f
}

func (fw *PollingFileWatcher) closeFile() {
	if fw.File != nil {
		_ = fw.File.Close() // Best effort close
	}
}

type pollBackoff struct {
	current time.Duration
	opts    PollingFileWatcherOptions
}

func newPollBackoff(opts PollingFileWatcherOptions) *pollBackoff {
	return &pollBackoff{
		current: opts.MinPollFrequency,
		opts:    opts,
	}
}

func (pb *pollBackoff) WaitTime() time.Duration {
	return pb.current
}

func (pb *pollBackoff) Reset() {
	pb.current = pb.opts.MinPollFrequency
}

func (pb *pollBackoff) Backoff() {
	pb.current = pb.current * 2
	if pb.current > pb.opts.MaxPollFrequency {
		pb.current = pb.opts.MaxPollFrequency
	}
}
