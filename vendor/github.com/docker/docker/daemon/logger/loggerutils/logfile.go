package loggerutils // import "github.com/docker/docker/daemon/logger/loggerutils"

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/daemon/logger"
	"github.com/docker/docker/pkg/filenotify"
	"github.com/docker/docker/pkg/pools"
	"github.com/docker/docker/pkg/pubsub"
	"github.com/fsnotify/fsnotify"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const tmpLogfileSuffix = ".tmp"

// rotateFileMetadata is a metadata of the gzip header of the compressed log file
type rotateFileMetadata struct {
	LastTime time.Time `json:"lastTime,omitempty"`
}

// refCounter is a counter of logfile being referenced
type refCounter struct {
	mu      sync.Mutex
	counter map[string]int
}

// Reference increase the reference counter for specified logfile
func (rc *refCounter) GetReference(fileName string, openRefFile func(fileName string, exists bool) (*os.File, error)) (*os.File, error) {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	var (
		file *os.File
		err  error
	)
	_, ok := rc.counter[fileName]
	file, err = openRefFile(fileName, ok)
	if err != nil {
		return nil, err
	}

	if ok {
		rc.counter[fileName]++
	} else if file != nil {
		rc.counter[file.Name()] = 1
	}

	return file, nil
}

// Dereference reduce the reference counter for specified logfile
func (rc *refCounter) Dereference(fileName string) error {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	rc.counter[fileName]--
	if rc.counter[fileName] <= 0 {
		delete(rc.counter, fileName)
		err := os.Remove(fileName)
		if err != nil {
			return err
		}
	}
	return nil
}

// LogFile is Logger implementation for default Docker logging.
type LogFile struct {
	mu              sync.RWMutex // protects the logfile access
	f               *os.File     // store for closing
	closed          bool
	rotateMu        sync.Mutex // blocks the next rotation until the current rotation is completed
	capacity        int64      // maximum size of each file
	currentSize     int64      // current size of the latest file
	maxFiles        int        // maximum number of files
	compress        bool       // whether old versions of log files are compressed
	lastTimestamp   time.Time  // timestamp of the last log
	filesRefCounter refCounter // keep reference-counted of decompressed files
	notifyRotate    *pubsub.Publisher
	marshal         logger.MarshalFunc
	createDecoder   MakeDecoderFn
	getTailReader   GetTailReaderFunc
	perms           os.FileMode
}

// MakeDecoderFn creates a decoder
type MakeDecoderFn func(rdr io.Reader) Decoder

// Decoder is for reading logs
// It is created by the log reader by calling the `MakeDecoderFunc`
type Decoder interface {
	// Reset resets the decoder
	// Reset is called for certain events, such as log rotations
	Reset(io.Reader)
	// Decode decodes the next log messeage from the stream
	Decode() (*logger.Message, error)
	// Close signals to the decoder that it can release whatever resources it was using.
	Close()
}

// SizeReaderAt defines a ReaderAt that also reports its size.
// This is used for tailing log files.
type SizeReaderAt interface {
	io.ReaderAt
	Size() int64
}

// GetTailReaderFunc is used to truncate a reader to only read as much as is required
// in order to get the passed in number of log lines.
// It returns the sectioned reader, the number of lines that the section reader
// contains, and any error that occurs.
type GetTailReaderFunc func(ctx context.Context, f SizeReaderAt, nLogLines int) (rdr io.Reader, nLines int, err error)

// NewLogFile creates new LogFile
func NewLogFile(logPath string, capacity int64, maxFiles int, compress bool, marshaller logger.MarshalFunc, decodeFunc MakeDecoderFn, perms os.FileMode, getTailReader GetTailReaderFunc) (*LogFile, error) {
	log, err := openFile(logPath, os.O_WRONLY|os.O_APPEND|os.O_CREATE, perms)
	if err != nil {
		return nil, err
	}

	size, err := log.Seek(0, io.SeekEnd)
	if err != nil {
		return nil, err
	}

	return &LogFile{
		f:               log,
		capacity:        capacity,
		currentSize:     size,
		maxFiles:        maxFiles,
		compress:        compress,
		filesRefCounter: refCounter{counter: make(map[string]int)},
		notifyRotate:    pubsub.NewPublisher(0, 1),
		marshal:         marshaller,
		createDecoder:   decodeFunc,
		perms:           perms,
		getTailReader:   getTailReader,
	}, nil
}

// WriteLogEntry writes the provided log message to the current log file.
// This may trigger a rotation event if the max file/capacity limits are hit.
func (w *LogFile) WriteLogEntry(msg *logger.Message) error {
	b, err := w.marshal(msg)
	if err != nil {
		return errors.Wrap(err, "error marshalling log message")
	}

	logger.PutMessage(msg)

	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return errors.New("cannot write because the output file was closed")
	}

	if err := w.checkCapacityAndRotate(); err != nil {
		w.mu.Unlock()
		return err
	}

	n, err := w.f.Write(b)
	if err == nil {
		w.currentSize += int64(n)
		w.lastTimestamp = msg.Timestamp
	}
	w.mu.Unlock()
	return err
}

func (w *LogFile) checkCapacityAndRotate() error {
	if w.capacity == -1 {
		return nil
	}

	if w.currentSize >= w.capacity {
		w.rotateMu.Lock()
		fname := w.f.Name()
		if err := w.f.Close(); err != nil {
			// if there was an error during a prior rotate, the file could already be closed
			if !errors.Is(err, os.ErrClosed) {
				w.rotateMu.Unlock()
				return errors.Wrap(err, "error closing file")
			}
		}
		if err := rotate(fname, w.maxFiles, w.compress); err != nil {
			w.rotateMu.Unlock()
			return err
		}
		file, err := openFile(fname, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, w.perms)
		if err != nil {
			w.rotateMu.Unlock()
			return err
		}
		w.f = file
		w.currentSize = 0
		w.notifyRotate.Publish(struct{}{})

		if w.maxFiles <= 1 || !w.compress {
			w.rotateMu.Unlock()
			return nil
		}

		go func() {
			compressFile(fname+".1", w.lastTimestamp)
			w.rotateMu.Unlock()
		}()
	}

	return nil
}

func rotate(name string, maxFiles int, compress bool) error {
	if maxFiles < 2 {
		return nil
	}

	var extension string
	if compress {
		extension = ".gz"
	}

	lastFile := fmt.Sprintf("%s.%d%s", name, maxFiles-1, extension)
	err := os.Remove(lastFile)
	if err != nil && !os.IsNotExist(err) {
		return errors.Wrap(err, "error removing oldest log file")
	}

	for i := maxFiles - 1; i > 1; i-- {
		toPath := name + "." + strconv.Itoa(i) + extension
		fromPath := name + "." + strconv.Itoa(i-1) + extension
		if err := os.Rename(fromPath, toPath); err != nil && !os.IsNotExist(err) {
			return err
		}
	}

	if err := os.Rename(name, name+".1"); err != nil && !os.IsNotExist(err) {
		return err
	}

	return nil
}

func compressFile(fileName string, lastTimestamp time.Time) {
	file, err := os.Open(fileName)
	if err != nil {
		logrus.Errorf("Failed to open log file: %v", err)
		return
	}
	defer func() {
		file.Close()
		err := os.Remove(fileName)
		if err != nil {
			logrus.Errorf("Failed to remove source log file: %v", err)
		}
	}()

	outFile, err := openFile(fileName+".gz", os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0640)
	if err != nil {
		logrus.Errorf("Failed to open or create gzip log file: %v", err)
		return
	}
	defer func() {
		outFile.Close()
		if err != nil {
			os.Remove(fileName + ".gz")
		}
	}()

	compressWriter := gzip.NewWriter(outFile)
	defer compressWriter.Close()

	// Add the last log entry timestamp to the gzip header
	extra := rotateFileMetadata{}
	extra.LastTime = lastTimestamp
	compressWriter.Header.Extra, err = json.Marshal(&extra)
	if err != nil {
		// Here log the error only and don't return since this is just an optimization.
		logrus.Warningf("Failed to marshal gzip header as JSON: %v", err)
	}

	_, err = pools.Copy(compressWriter, file)
	if err != nil {
		logrus.WithError(err).WithField("module", "container.logs").WithField("file", fileName).Error("Error compressing log file")
		return
	}
}

// MaxFiles return maximum number of files
func (w *LogFile) MaxFiles() int {
	return w.maxFiles
}

// Close closes underlying file and signals all readers to stop.
func (w *LogFile) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	if err := w.f.Close(); err != nil {
		return err
	}
	w.closed = true
	return nil
}

// ReadLogs decodes entries from log files and sends them the passed in watcher
//
// Note: Using the follow option can become inconsistent in cases with very frequent rotations and max log files is 1.
// TODO: Consider a different implementation which can effectively follow logs under frequent rotations.
func (w *LogFile) ReadLogs(config logger.ReadConfig, watcher *logger.LogWatcher) {
	w.mu.RLock()
	currentFile, err := os.Open(w.f.Name())
	if err != nil {
		w.mu.RUnlock()
		watcher.Err <- err
		return
	}
	defer currentFile.Close()

	dec := w.createDecoder(nil)
	defer dec.Close()

	currentChunk, err := newSectionReader(currentFile)
	if err != nil {
		w.mu.RUnlock()
		watcher.Err <- err
		return
	}

	if config.Tail != 0 {
		// TODO(@cpuguy83): Instead of opening every file, only get the files which
		// are needed to tail.
		// This is especially costly when compression is enabled.
		files, err := w.openRotatedFiles(config)
		w.mu.RUnlock()
		if err != nil {
			watcher.Err <- err
			return
		}

		closeFiles := func() {
			for _, f := range files {
				f.Close()
				fileName := f.Name()
				if strings.HasSuffix(fileName, tmpLogfileSuffix) {
					err := w.filesRefCounter.Dereference(fileName)
					if err != nil {
						logrus.Errorf("Failed to dereference the log file %q: %v", fileName, err)
					}
				}
			}
		}

		readers := make([]SizeReaderAt, 0, len(files)+1)
		for _, f := range files {
			stat, err := f.Stat()
			if err != nil {
				watcher.Err <- errors.Wrap(err, "error reading size of rotated file")
				closeFiles()
				return
			}
			readers = append(readers, io.NewSectionReader(f, 0, stat.Size()))
		}
		if currentChunk.Size() > 0 {
			readers = append(readers, currentChunk)
		}

		tailFiles(readers, watcher, dec, w.getTailReader, config)
		closeFiles()

		w.mu.RLock()
	}

	if !config.Follow || w.closed {
		w.mu.RUnlock()
		return
	}
	w.mu.RUnlock()

	notifyRotate := w.notifyRotate.Subscribe()
	defer w.notifyRotate.Evict(notifyRotate)
	followLogs(currentFile, watcher, notifyRotate, dec, config.Since, config.Until)
}

func (w *LogFile) openRotatedFiles(config logger.ReadConfig) (files []*os.File, err error) {
	w.rotateMu.Lock()
	defer w.rotateMu.Unlock()

	defer func() {
		if err == nil {
			return
		}
		for _, f := range files {
			f.Close()
			if strings.HasSuffix(f.Name(), tmpLogfileSuffix) {
				err := os.Remove(f.Name())
				if err != nil && !os.IsNotExist(err) {
					logrus.Warnf("Failed to remove logfile: %v", err)
				}
			}
		}
	}()

	for i := w.maxFiles; i > 1; i-- {
		f, err := os.Open(fmt.Sprintf("%s.%d", w.f.Name(), i-1))
		if err != nil {
			if !os.IsNotExist(err) {
				return nil, errors.Wrap(err, "error opening rotated log file")
			}

			fileName := fmt.Sprintf("%s.%d.gz", w.f.Name(), i-1)
			decompressedFileName := fileName + tmpLogfileSuffix
			tmpFile, err := w.filesRefCounter.GetReference(decompressedFileName, func(refFileName string, exists bool) (*os.File, error) {
				if exists {
					return os.Open(refFileName)
				}
				return decompressfile(fileName, refFileName, config.Since)
			})

			if err != nil {
				if !errors.Is(err, os.ErrNotExist) {
					return nil, errors.Wrap(err, "error getting reference to decompressed log file")
				}
				continue
			}
			if tmpFile == nil {
				// The log before `config.Since` does not need to read
				break
			}

			files = append(files, tmpFile)
			continue
		}
		files = append(files, f)
	}

	return files, nil
}

func decompressfile(fileName, destFileName string, since time.Time) (*os.File, error) {
	cf, err := os.Open(fileName)
	if err != nil {
		return nil, errors.Wrap(err, "error opening file for decompression")
	}
	defer cf.Close()

	rc, err := gzip.NewReader(cf)
	if err != nil {
		return nil, errors.Wrap(err, "error making gzip reader for compressed log file")
	}
	defer rc.Close()

	// Extract the last log entry timestramp from the gzip header
	extra := &rotateFileMetadata{}
	err = json.Unmarshal(rc.Header.Extra, extra)
	if err == nil && extra.LastTime.Before(since) {
		return nil, nil
	}

	rs, err := openFile(destFileName, os.O_CREATE|os.O_RDWR, 0640)
	if err != nil {
		return nil, errors.Wrap(err, "error creating file for copying decompressed log stream")
	}

	_, err = pools.Copy(rs, rc)
	if err != nil {
		rs.Close()
		rErr := os.Remove(rs.Name())
		if rErr != nil && !os.IsNotExist(rErr) {
			logrus.Errorf("Failed to remove logfile: %v", rErr)
		}
		return nil, errors.Wrap(err, "error while copying decompressed log stream to file")
	}

	return rs, nil
}

func newSectionReader(f *os.File) (*io.SectionReader, error) {
	// seek to the end to get the size
	// we'll leave this at the end of the file since section reader does not advance the reader
	size, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return nil, errors.Wrap(err, "error getting current file size")
	}
	return io.NewSectionReader(f, 0, size), nil
}

func tailFiles(files []SizeReaderAt, watcher *logger.LogWatcher, dec Decoder, getTailReader GetTailReaderFunc, config logger.ReadConfig) {
	nLines := config.Tail

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// TODO(@cpuguy83): we should plumb a context through instead of dealing with `WatchClose()` here.
	go func() {
		select {
		case <-ctx.Done():
		case <-watcher.WatchConsumerGone():
			cancel()
		}
	}()

	readers := make([]io.Reader, 0, len(files))

	if config.Tail > 0 {
		for i := len(files) - 1; i >= 0 && nLines > 0; i-- {
			tail, n, err := getTailReader(ctx, files[i], nLines)
			if err != nil {
				watcher.Err <- errors.Wrap(err, "error finding file position to start log tailing")
				return
			}
			nLines -= n
			readers = append([]io.Reader{tail}, readers...)
		}
	} else {
		for _, r := range files {
			readers = append(readers, &wrappedReaderAt{ReaderAt: r})
		}
	}

	rdr := io.MultiReader(readers...)
	dec.Reset(rdr)

	for {
		msg, err := dec.Decode()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				watcher.Err <- err
			}
			return
		}
		if !config.Since.IsZero() && msg.Timestamp.Before(config.Since) {
			continue
		}
		if !config.Until.IsZero() && msg.Timestamp.After(config.Until) {
			return
		}
		select {
		case <-ctx.Done():
			return
		case watcher.Msg <- msg:
		}
	}
}

func followLogs(f *os.File, logWatcher *logger.LogWatcher, notifyRotate chan interface{}, dec Decoder, since, until time.Time) {
	dec.Reset(f)

	name := f.Name()
	fileWatcher, err := watchFile(name)
	if err != nil {
		logWatcher.Err <- err
		return
	}
	defer func() {
		f.Close()
		fileWatcher.Close()
	}()

	var retries int
	handleRotate := func() error {
		f.Close()
		fileWatcher.Remove(name)

		// retry when the file doesn't exist
		for retries := 0; retries <= 5; retries++ {
			f, err = os.Open(name)
			if err == nil || !os.IsNotExist(err) {
				break
			}
		}
		if err != nil {
			return err
		}
		if err := fileWatcher.Add(name); err != nil {
			return err
		}
		dec.Reset(f)
		return nil
	}

	errRetry := errors.New("retry")
	errDone := errors.New("done")
	waitRead := func() error {
		select {
		case e := <-fileWatcher.Events():
			switch e.Op {
			case fsnotify.Write:
				dec.Reset(f)
				return nil
			case fsnotify.Rename, fsnotify.Remove:
				select {
				case <-notifyRotate:
				case <-logWatcher.WatchProducerGone():
					return errDone
				case <-logWatcher.WatchConsumerGone():
					return errDone
				}
				if err := handleRotate(); err != nil {
					return err
				}
				return nil
			}
			return errRetry
		case err := <-fileWatcher.Errors():
			logrus.Debugf("logger got error watching file: %v", err)
			// Something happened, let's try and stay alive and create a new watcher
			if retries <= 5 {
				fileWatcher.Close()
				fileWatcher, err = watchFile(name)
				if err != nil {
					return err
				}
				retries++
				return errRetry
			}
			return err
		case <-logWatcher.WatchProducerGone():
			return errDone
		case <-logWatcher.WatchConsumerGone():
			return errDone
		}
	}

	oldSize := int64(-1)
	handleDecodeErr := func(err error) error {
		if !errors.Is(err, io.EOF) {
			return err
		}

		// Handle special case (#39235): max-file=1 and file was truncated
		st, stErr := f.Stat()
		if stErr == nil {
			size := st.Size()
			defer func() { oldSize = size }()
			if size < oldSize { // truncated
				f.Seek(0, 0)
				return nil
			}
		} else {
			logrus.WithError(stErr).Warn("logger: stat error")
		}

		for {
			err := waitRead()
			if err == nil {
				break
			}
			if err == errRetry {
				continue
			}
			return err
		}
		return nil
	}

	// main loop
	for {
		msg, err := dec.Decode()
		if err != nil {
			if err := handleDecodeErr(err); err != nil {
				if err == errDone {
					return
				}
				// we got an unrecoverable error, so return
				logWatcher.Err <- err
				return
			}
			// ready to try again
			continue
		}

		retries = 0 // reset retries since we've succeeded
		if !since.IsZero() && msg.Timestamp.Before(since) {
			continue
		}
		if !until.IsZero() && msg.Timestamp.After(until) {
			return
		}
		// send the message, unless the consumer is gone
		select {
		case logWatcher.Msg <- msg:
		case <-logWatcher.WatchConsumerGone():
			return
		}
	}
}

func watchFile(name string) (filenotify.FileWatcher, error) {
	var fileWatcher filenotify.FileWatcher

	if runtime.GOOS == "windows" {
		// FileWatcher on Windows files is based on the syscall notifications which has an issue because of file caching.
		// It is based on ReadDirectoryChangesW() which doesn't detect writes to the cache. It detects writes to disk only.
		// Because of the OS lazy writing, we don't get notifications for file writes and thereby the watcher
		// doesn't work. Hence for Windows we will use poll based notifier.
		fileWatcher = filenotify.NewPollingWatcher()
	} else {
		var err error
		fileWatcher, err = filenotify.New()
		if err != nil {
			return nil, err
		}
	}

	logger := logrus.WithFields(logrus.Fields{
		"module": "logger",
		"file":   name,
	})

	if err := fileWatcher.Add(name); err != nil {
		// we will retry using file poller.
		logger.WithError(err).Warnf("falling back to file poller")
		fileWatcher.Close()
		fileWatcher = filenotify.NewPollingWatcher()

		if err := fileWatcher.Add(name); err != nil {
			fileWatcher.Close()
			logger.WithError(err).Debugf("error watching log file for modifications")
			return nil, err
		}
	}

	return fileWatcher, nil
}

type wrappedReaderAt struct {
	io.ReaderAt
	pos int64
}

func (r *wrappedReaderAt) Read(p []byte) (int, error) {
	n, err := r.ReaderAt.ReadAt(p, r.pos)
	r.pos += int64(n)
	return n, err
}
