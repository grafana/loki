// Copyright (c) 2015 HPE Software Inc. All rights reserved.
// Copyright (c) 2013 ActiveState Software Inc. All rights reserved.

package tail

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/hpcloud/tail/ratelimiter"
	"github.com/hpcloud/tail/util"
	"github.com/hpcloud/tail/watch"
	"gopkg.in/tomb.v1"
)

var (
	ErrStop = errors.New("tail should now stop")
)

type Line struct {
	Text string
	Time time.Time
	Err  error // Error from tail
}

// NewLine returns a Line with present time.
func NewLine(text string) *Line {
	return &Line{text, time.Now(), nil}
}

// SeekInfo represents arguments to `os.Seek`
type SeekInfo struct {
	Offset int64
	Whence int // os.SEEK_*
}

type logger interface {
	Fatal(v ...interface{})
	Fatalf(format string, v ...interface{})
	Fatalln(v ...interface{})
	Panic(v ...interface{})
	Panicf(format string, v ...interface{})
	Panicln(v ...interface{})
	Print(v ...interface{})
	Printf(format string, v ...interface{})
	Println(v ...interface{})
}

// Config is used to specify how a file must be tailed.
type Config struct {
	// File-specifc
	Location    *SeekInfo // Seek to this location before tailing
	ReOpen      bool      // Reopen recreated files (tail -F)
	MustExist   bool      // Fail early if the file does not exist
	Poll        bool      // Poll for file changes instead of using inotify
	Pipe        bool      // Is a named pipe (mkfifo)
	RateLimiter *ratelimiter.LeakyBucket

	// Generic IO
	Follow      bool // Continue looking for new lines (tail -f)
	MaxLineSize int  // If non-zero, split longer lines into multiple lines

	// Logger, when nil, is set to tail.DefaultLogger
	// To disable logging: set field to tail.DiscardingLogger
	Logger logger
}

type Tail struct {
	Filename string
	Lines    chan *Line
	Config

	file   *os.File
	reader *bufio.Reader

	watcher watch.FileWatcher
	changes *watch.FileChanges

	tomb.Tomb // provides: Done, Kill, Dying

	lk sync.Mutex
}

var (
	// DefaultLogger is used when Config.Logger == nil
	DefaultLogger = log.New(os.Stderr, "", log.LstdFlags)
	// DiscardingLogger can be used to disable logging output
	DiscardingLogger = log.New(ioutil.Discard, "", 0)
)

// TailFile begins tailing the file. Output stream is made available
// via the `Tail.Lines` channel. To handle errors during tailing,
// invoke the `Wait` or `Err` method after finishing reading from the
// `Lines` channel.
func TailFile(filename string, config Config) (*Tail, error) {
	if config.ReOpen && !config.Follow {
		util.Fatal("cannot set ReOpen without Follow.")
	}

	t := &Tail{
		Filename: filename,
		Lines:    make(chan *Line),
		Config:   config,
	}

	// when Logger was not specified in config, use default logger
	if t.Logger == nil {
		t.Logger = log.New(os.Stderr, "", log.LstdFlags)
	}

	if t.Poll {
		t.watcher = watch.NewPollingFileWatcher(filename)
	} else {
		t.watcher = watch.NewInotifyFileWatcher(filename)
	}

	if t.MustExist {
		var err error
		t.file, err = OpenFile(t.Filename)
		if err != nil {
			return nil, err
		}
	}

	go t.tailFileSync()

	return t, nil
}

// Return the file's current position, like stdio's ftell().
// But this value is not very accurate.
// it may readed one line in the chan(tail.Lines),
// so it may lost one line.
func (tail *Tail) Tell() (offset int64, err error) {
	if tail.file == nil {
		return
	}
	offset, err = tail.file.Seek(0, io.SeekCurrent)
	if err != nil {
		return
	}

	tail.lk.Lock()
	defer tail.lk.Unlock()
	if tail.reader == nil {
		return
	}

	offset -= int64(tail.reader.Buffered())
	return
}

// Size returns the length in bytes of the file being tailed,
// or 0 with an error if there was an error Stat'ing the file.
func (tail *Tail) Size() (int64, error) {
	fi, err := tail.file.Stat()
	if err != nil {
		return 0, err
	}
	size := fi.Size()
	return size, nil
}

// Stop stops the tailing activity.
func (tail *Tail) Stop() error {
	tail.Kill(nil)
	return tail.Wait()
}

// StopAtEOF stops tailing as soon as the end of the file is reached.
func (tail *Tail) StopAtEOF() error {
	tail.Kill(errStopAtEOF)
	return tail.Wait()
}

var errStopAtEOF = errors.New("tail: stop at eof")

func (tail *Tail) close() {
	close(tail.Lines)
	tail.closeFile()
}

func (tail *Tail) closeFile() {
	if tail.file != nil {
		tail.file.Close()
		tail.file = nil
	}
}

func (tail *Tail) reopen(truncated bool) error {

	// There are cases where the file is reopened so quickly it's still the same file
	// which causes the poller to hang on an open file handle to a file no longer being written to
	// and which eventually gets deleted.  Save the current file handle info to make sure we only
	// start tailing a different file.
	cf, err := tail.file.Stat()
	if !truncated && err != nil {
		log.Print("stat of old file returned, this is not expected and may result in unexpected behavior")
		// We don't action on this error but are logging it, not expecting to see it happen and not sure if we
		// need to action on it, cf is checked for nil later on to accommodate this
	}

	tail.closeFile()
	for {
		var err error
		tail.file, err = OpenFile(tail.Filename)
		if err != nil {
			if os.IsNotExist(err) {
				tail.Logger.Printf("Waiting for %s to appear...", tail.Filename)
				if err := tail.watcher.BlockUntilExists(&tail.Tomb); err != nil {
					if err == tomb.ErrDying {
						return err
					}
					return fmt.Errorf("Failed to detect creation of %s: %s", tail.Filename, err)
				}
				continue
			}
			return fmt.Errorf("Unable to open file %s: %s", tail.Filename, err)
		}

		// File exists and is opened, get information about it.
		nf, err := tail.file.Stat()
		if err != nil {
			tail.Logger.Print("Failed to stat new file to be tailed, will try to open it again")
			tail.closeFile()
			continue
		}

		// Check to see if we are trying to reopen and tail the exact same file (and it was not truncated).
		retries := 20
		if !truncated && cf != nil && os.SameFile(cf, nf) {
			retries--
			if retries <= 0 {
				return errors.New("gave up trying to reopen log file with a different handle")
			}
			select {
			case <-time.After(watch.POLL_DURATION):
				tail.closeFile()
				continue
			case <-tail.Tomb.Dying():
				return tomb.ErrDying
			}
		}
		break
	}
	return nil
}

func (tail *Tail) readLine() (string, error) {
	tail.lk.Lock()
	line, err := tail.reader.ReadString('\n')
	tail.lk.Unlock()
	if err != nil {
		// Note ReadString "returns the data read before the error" in
		// case of an error, including EOF, so we return it as is. The
		// caller is expected to process it if err is EOF.
		return line, err
	}

	line = strings.TrimRight(line, "\n")

	return line, err
}

func (tail *Tail) tailFileSync() {
	defer tail.Done()
	defer tail.close()

	if !tail.MustExist {
		// deferred first open, not technically truncated but we don't need to check for changed files
		err := tail.reopen(true)
		if err != nil {
			if err != tomb.ErrDying {
				tail.Kill(err)
			}
			return
		}
	}

	// Seek to requested location on first open of the file.
	if tail.Location != nil {
		_, err := tail.file.Seek(tail.Location.Offset, tail.Location.Whence)
		tail.Logger.Printf("Seeked %s - %+v\n", tail.Filename, tail.Location)
		if err != nil {
			tail.Killf("Seek error on %s: %s", tail.Filename, err)
			return
		}
	}

	tail.openReader()

	var offset int64
	var err error
	oneMoreRun := false

	// Read line by line.
	for {
		// do not seek in named pipes
		if !tail.Pipe {
			// grab the position in case we need to back up in the event of a half-line
			offset, err = tail.Tell()
			if err != nil {
				tail.Kill(err)
				return
			}
		}

		line, err := tail.readLine()

		// Process `line` even if err is EOF.
		if err == nil {
			cooloff := !tail.sendLine(line)
			if cooloff {
				// Wait a second before seeking till the end of
				// file when rate limit is reached.
				msg := ("Too much log activity; waiting a second " +
					"before resuming tailing")
				tail.Lines <- &Line{msg, time.Now(), errors.New(msg)}
				select {
				case <-time.After(time.Second):
				case <-tail.Dying():
					return
				}
				if err := tail.seekEnd(); err != nil {
					tail.Kill(err)
					return
				}
			}
		} else if err == io.EOF {
			if !tail.Follow {
				if line != "" {
					tail.sendLine(line)
				}
				return
			}

			if tail.Follow && line != "" {
				// this has the potential to never return the last line if
				// it's not followed by a newline; seems a fair trade here
				err := tail.seekTo(SeekInfo{Offset: offset, Whence: 0})
				if err != nil {
					tail.Kill(err)
					return
				}
			}

			// oneMoreRun is set true when a file is deleted,
			// this is to catch events which might get missed in polling mode.
			// now that the last run is completed, finish deleting the file
			if oneMoreRun {
				oneMoreRun = false
				err = tail.finishDelete()
				if err != nil {
					if err != ErrStop {
						tail.Kill(err)
					}
					return
				}
			}

			// When EOF is reached, wait for more data to become
			// available. Wait strategy is based on the `tail.watcher`
			// implementation (inotify or polling).
			oneMoreRun, err = tail.waitForChanges()
			if err != nil {
				if err != ErrStop {
					tail.Kill(err)
				}
				return
			}
		} else {
			// non-EOF error
			tail.Killf("Error reading %s: %s", tail.Filename, err)
			return
		}

		select {
		case <-tail.Dying():
			if tail.Err() == errStopAtEOF {
				continue
			}
			return
		default:
		}
	}
}

// waitForChanges waits until the file has been appended, deleted,
// moved or truncated. When moved or deleted - the file will be
// reopened if ReOpen is true. Truncated files are always reopened.
func (tail *Tail) waitForChanges() (bool, error) {
	if tail.changes == nil {
		pos, err := tail.file.Seek(0, io.SeekCurrent)
		if err != nil {
			return false, err
		}
		tail.changes, err = tail.watcher.ChangeEvents(&tail.Tomb, pos)
		if err != nil {
			return false, err
		}
	}

	select {
	case <-tail.changes.Modified:
		return false, nil
	case <-tail.changes.Deleted:
		// In polling mode we could miss events when a file is deleted, so before we give up our file handle
		// run the poll one more time to catch anything we may have missed since the last poll.
		return true, nil
	case <-tail.changes.Truncated:
		// Always reopen truncated files (Follow is true)
		tail.Logger.Printf("Re-opening truncated file %s ...", tail.Filename)
		if err := tail.reopen(true); err != nil {
			return false, err
		}
		tail.Logger.Printf("Successfully reopened truncated %s", tail.Filename)
		tail.openReader()
		return false, nil
	case <-tail.Dying():
		return false, ErrStop
	}
	panic("unreachable")
}

func (tail *Tail) finishDelete() error {
	tail.changes = nil
	if tail.ReOpen {
		// XXX: we must not log from a library.
		tail.Logger.Printf("Re-opening moved/deleted file %s ...", tail.Filename)
		if err := tail.reopen(false); err != nil {
			return err
		}
		tail.Logger.Printf("Successfully reopened %s", tail.Filename)
		tail.openReader()
		return nil
	} else {
		tail.Logger.Printf("Stopping tail as file no longer exists: %s", tail.Filename)
		return ErrStop
	}
}

func (tail *Tail) openReader() {
	if tail.MaxLineSize > 0 {
		// add 2 to account for newline characters
		tail.reader = bufio.NewReaderSize(tail.file, tail.MaxLineSize+2)
	} else {
		tail.reader = bufio.NewReader(tail.file)
	}
}

func (tail *Tail) seekEnd() error {
	return tail.seekTo(SeekInfo{Offset: 0, Whence: os.SEEK_END})
}

func (tail *Tail) seekTo(pos SeekInfo) error {
	_, err := tail.file.Seek(pos.Offset, pos.Whence)
	if err != nil {
		return fmt.Errorf("Seek error on %s: %s", tail.Filename, err)
	}
	// Reset the read buffer whenever the file is re-seek'ed
	tail.reader.Reset(tail.file)
	return nil
}

// sendLine sends the line(s) to Lines channel, splitting longer lines
// if necessary. Return false if rate limit is reached.
func (tail *Tail) sendLine(line string) bool {
	now := time.Now()
	lines := []string{line}

	// Split longer lines
	if tail.MaxLineSize > 0 && len(line) > tail.MaxLineSize {
		lines = util.PartitionString(line, tail.MaxLineSize)
	}

	for _, line := range lines {
		tail.Lines <- &Line{line, now, nil}
	}

	if tail.Config.RateLimiter != nil {
		ok := tail.Config.RateLimiter.Pour(uint16(len(lines)))
		if !ok {
			tail.Logger.Printf("Leaky bucket full (%v); entering 1s cooloff period.\n",
				tail.Filename)
			return false
		}
	}

	return true
}

// Cleanup removes inotify watches added by the tail package. This function is
// meant to be invoked from a process's exit handler. Linux kernel may not
// automatically remove inotify watches after the process exits.
func (tail *Tail) Cleanup() {
	watch.Cleanup(tail.Filename)
}
