package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/containerd/fifo"
	"github.com/docker/docker/api/types/backend"
	"github.com/docker/docker/api/types/plugins/logdriver"
	"github.com/docker/docker/daemon/logger"
	"github.com/docker/docker/daemon/logger/jsonfilelog"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	protoio "github.com/gogo/protobuf/io"
	"github.com/pkg/errors"
)

type driver struct {
	mu     sync.Mutex
	logs   map[string]*logPair
	idx    map[string]*logPair
	logger log.Logger
}

type logPair struct {
	jsonl  logger.Logger
	lokil  logger.Logger
	stream io.ReadCloser
	info   logger.Info
	logger log.Logger
	// folder where json log files will be created.
	folder string
	// keep created files after stopping the container.
	keepFile bool
}

func (l *logPair) Close() {
	if err := l.stream.Close(); err != nil {
		level.Error(l.logger).Log("msg", "error while closing fifo stream", "err", err)
	}
	if err := l.lokil.Close(); err != nil {
		level.Error(l.logger).Log("msg", "error while closing loki logger", "err", err)
	}
	if l.jsonl == nil {
		return
	}
	if err := l.jsonl.Close(); err != nil {
		level.Error(l.logger).Log("msg", "error while closing json logger", "err", err)
	}
}

func newDriver(logger log.Logger) *driver {
	return &driver{
		logs:   make(map[string]*logPair),
		idx:    make(map[string]*logPair),
		logger: logger,
	}
}

func (d *driver) StartLogging(file string, logCtx logger.Info) error {
	d.mu.Lock()
	if _, exists := d.logs[file]; exists {
		d.mu.Unlock()
		return fmt.Errorf("logger for %q already exists", file)
	}
	d.mu.Unlock()
	folder := fmt.Sprintf("/var/log/docker/%s/", logCtx.ContainerID)
	logCtx.LogPath = filepath.Join(folder, "json.log")
	level.Info(d.logger).Log("msg", "starting logging driver for container", "id", logCtx.ContainerID, "config", fmt.Sprintf("%+v", logCtx.Config), "file", file, "logpath", logCtx.LogPath)

	noFile, err := parseBoolean(cfgNofile, logCtx, false)
	if err != nil {
		return err
	}

	keepFile, err := parseBoolean(cfgKeepFile, logCtx, false)
	if err != nil {
		return err
	}

	var jsonl logger.Logger
	if !noFile {
		if err := os.MkdirAll(folder, 0755); err != nil {
			return errors.Wrap(err, "error setting up logger dir")
		}

		jsonl, err = jsonfilelog.New(logCtx)
		if err != nil {
			return errors.Wrap(err, "error creating jsonfile logger")
		}
	}

	lokil, err := New(logCtx, d.logger)
	if err != nil {
		return errors.Wrap(err, "error creating loki logger")
	}
	f, err := fifo.OpenFifo(context.Background(), file, syscall.O_RDONLY, 0700)
	if err != nil {
		return errors.Wrapf(err, "error opening logger fifo: %q", file)
	}

	d.mu.Lock()
	lf := &logPair{jsonl, lokil, f, logCtx, d.logger, folder, keepFile}
	d.logs[file] = lf
	d.idx[logCtx.ContainerID] = lf
	d.mu.Unlock()

	go consumeLog(lf)
	return nil
}

func (d *driver) StopLogging(file string) {
	level.Debug(d.logger).Log("msg", "Stop logging", "file", file)
	d.mu.Lock()
	defer d.mu.Unlock()
	lf, ok := d.logs[file]
	if !ok {
		return
	}
	lf.Close()
	delete(d.logs, file)
	if !lf.keepFile && lf.jsonl != nil {
		// delete the folder where all log files were created.
		if err := os.RemoveAll(lf.folder); err != nil {
			level.Debug(d.logger).Log("msg", "error deleting folder", "folder", lf.folder)
		}
	}
}

func consumeLog(lf *logPair) {
	dec := protoio.NewUint32DelimitedReader(lf.stream, binary.BigEndian, 1e6)
	defer dec.Close()
	defer lf.Close()
	var buf logdriver.LogEntry
	for {
		if err := dec.ReadMsg(&buf); err != nil {
			if err == io.EOF || err == os.ErrClosed || strings.Contains(err.Error(), "file already closed") {
				level.Debug(lf.logger).Log("msg", "shutting down log logger", "id", lf.info.ContainerID, "err", err)
				return
			}
			dec = protoio.NewUint32DelimitedReader(lf.stream, binary.BigEndian, 1e6)
		}
		var msg logger.Message
		msg.Line = buf.Line
		msg.Source = buf.Source
		if buf.PartialLogMetadata != nil {
			if msg.PLogMetaData == nil {
				msg.PLogMetaData = &backend.PartialLogMetaData{}
			}
			msg.PLogMetaData.ID = buf.PartialLogMetadata.Id
			msg.PLogMetaData.Last = buf.PartialLogMetadata.Last
			msg.PLogMetaData.Ordinal = int(buf.PartialLogMetadata.Ordinal)
		}
		msg.Timestamp = time.Unix(0, buf.TimeNano)

		// loki goes first as the json logger reset the message on completion.
		if err := lf.lokil.Log(&msg); err != nil {
			level.Error(lf.logger).Log("msg", "error pushing message to loki", "id", lf.info.ContainerID, "err", err, "message", msg)
		}
		if lf.jsonl != nil {
			if err := lf.jsonl.Log(&msg); err != nil {
				level.Error(lf.logger).Log("msg", "error writing log message", "id", lf.info.ContainerID, "err", err, "message", msg)
				continue
			}
		}

		buf.Reset()
	}
}

func (d *driver) ReadLogs(info logger.Info, config logger.ReadConfig) (io.ReadCloser, error) {
	d.mu.Lock()
	lf, exists := d.idx[info.ContainerID]
	d.mu.Unlock()
	if !exists {
		return nil, fmt.Errorf("logger does not exist for %s", info.ContainerID)
	}

	if lf.jsonl == nil {
		return nil, fmt.Errorf("%s option set to true, no reading capability", cfgNofile)
	}

	r, w := io.Pipe()
	lr, ok := lf.jsonl.(logger.LogReader)
	if !ok {
		return nil, errors.New("logger does not support reading")
	}

	go func() {
		watcher := lr.ReadLogs(config)

		enc := protoio.NewUint32DelimitedWriter(w, binary.BigEndian)
		defer enc.Close()
		defer watcher.ConsumerGone()

		var buf logdriver.LogEntry
		for {
			select {
			case msg, ok := <-watcher.Msg:
				if !ok {
					w.Close()
					return
				}

				buf.Line = msg.Line
				buf.Partial = msg.PLogMetaData != nil
				buf.TimeNano = msg.Timestamp.UnixNano()
				buf.Source = msg.Source

				if err := enc.WriteMsg(&buf); err != nil {
					_ = w.CloseWithError(err)
					return
				}
			case err := <-watcher.Err:
				_ = w.CloseWithError(err)
				return
			}

			buf.Reset()
		}
	}()

	return r, nil
}
