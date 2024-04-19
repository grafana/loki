package file

import (
	"bufio"
	"compress/bzip2"
	"compress/gzip"
	"compress/zlib"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"go.uber.org/atomic"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/ianaindex"
	"golang.org/x/text/transform"

	"github.com/grafana/loki/v3/pkg/logproto"

	"github.com/grafana/loki/v3/clients/pkg/promtail/api"
	"github.com/grafana/loki/v3/clients/pkg/promtail/positions"
	"github.com/grafana/loki/v3/clients/pkg/promtail/scrapeconfig"
)

func supportedCompressedFormats() map[string]struct{} {
	return map[string]struct{}{
		".gz":     {},
		".tar.gz": {},
		".z":      {},
		".bz2":    {},
		// TODO: add support for .zip extension.
	}
}

type decompressor struct {
	metrics   *Metrics
	logger    log.Logger
	handler   api.EntryHandler
	positions positions.Positions

	path string

	posAndSizeMtx sync.Mutex
	stopOnce      sync.Once

	running *atomic.Bool
	posquit chan struct{}
	posdone chan struct{}
	done    chan struct{}

	decoder *encoding.Decoder

	cfg *scrapeconfig.DecompressionConfig

	position int64
	size     int64
}

func newDecompressor(metrics *Metrics, logger log.Logger, handler api.EntryHandler, positions positions.Positions, path string, encodingFormat string, cfg *scrapeconfig.DecompressionConfig) (*decompressor, error) {
	logger = log.With(logger, "component", "decompressor")

	pos, err := positions.Get(path)
	if err != nil {
		return nil, errors.Wrap(err, "get positions")
	}

	var decoder *encoding.Decoder
	if encodingFormat != "" {
		level.Info(logger).Log("msg", "decompressor will decode messages", "from", encodingFormat, "to", "UTF8")
		encoder, err := ianaindex.IANA.Encoding(encodingFormat)
		if err != nil {
			return nil, errors.Wrap(err, "error doing IANA encoding")
		}
		decoder = encoder.NewDecoder()
	}

	decompressor := &decompressor{
		metrics:   metrics,
		logger:    logger,
		handler:   api.AddLabelsMiddleware(model.LabelSet{FilenameLabel: model.LabelValue(path)}).Wrap(handler),
		positions: positions,
		path:      path,
		running:   atomic.NewBool(false),
		posquit:   make(chan struct{}),
		posdone:   make(chan struct{}),
		done:      make(chan struct{}),
		position:  pos,
		decoder:   decoder,
		cfg:       cfg,
	}

	go decompressor.readLines()
	go decompressor.updatePosition()
	metrics.filesActive.Add(1.)
	return decompressor, nil
}

// mountReader instantiate a reader ready to be used by the decompressor.
//
// The selected reader implementation is based on the extension of the given file name.
// It'll error if the extension isn't supported.
func mountReader(f *os.File, logger log.Logger, format string) (reader io.Reader, err error) {
	var decompressLib string

	switch format {
	case "gz":
		decompressLib = "compress/gzip"
		reader, err = gzip.NewReader(f)
	case "z":
		decompressLib = "compress/zlib"
		reader, err = zlib.NewReader(f)
	case "bz2":
		decompressLib = "bzip2"
		reader = bzip2.NewReader(f)
	}

	if err != nil && err != io.EOF {
		return nil, err
	}

	if reader == nil {
		supportedFormatsList := strings.Builder{}
		for format := range supportedCompressedFormats() {
			supportedFormatsList.WriteString(format)
		}
		return nil, fmt.Errorf("file %q has unsupported format, it has to be one of %q", f.Name(), supportedFormatsList.String())
	}

	level.Debug(logger).Log("msg", fmt.Sprintf("using %q to decompress file %q", decompressLib, f.Name()))
	return reader, nil
}

func (t *decompressor) updatePosition() {
	positionSyncPeriod := t.positions.SyncPeriod()
	positionWait := time.NewTicker(positionSyncPeriod)
	defer func() {
		positionWait.Stop()
		level.Info(t.logger).Log("msg", "position timer: exited", "path", t.path)
		close(t.posdone)
	}()

	for {
		select {
		case <-positionWait.C:
			if err := t.MarkPositionAndSize(); err != nil {
				level.Error(t.logger).Log("msg", "position timer: error getting position and/or size, stopping decompressor", "path", t.path, "error", err)
				return
			}
		case <-t.posquit:
			return
		}
	}
}

// readLines read all existing lines of the given compressed file.
//
// It first decompress the file as a whole using a reader and then it will iterate
// over its chunks, separated by '\n'.
// During each iteration, the parsed and decoded log line is then sent to the API with the current timestamp.
func (t *decompressor) readLines() {
	level.Info(t.logger).Log("msg", "read lines routine: started", "path", t.path)
	t.running.Store(true)

	if t.cfg.InitialDelay > 0 {
		level.Info(t.logger).Log("msg", "sleeping before starting decompression", "path", t.path, "duration", t.cfg.InitialDelay.String())
		time.Sleep(t.cfg.InitialDelay)
	}

	defer func() {
		t.cleanupMetrics()
		level.Info(t.logger).Log("msg", "read lines routine finished", "path", t.path)
		close(t.done)
	}()
	entries := t.handler.Chan()

	f, err := os.Open(t.path)
	if err != nil {
		level.Error(t.logger).Log("msg", "error reading file", "path", t.path, "error", err)
		return
	}
	defer f.Close()

	r, err := mountReader(f, t.logger, t.cfg.Format)
	if err != nil {
		level.Error(t.logger).Log("msg", "error mounting new reader", "err", err)
		return
	}

	level.Info(t.logger).Log("msg", "successfully mounted reader", "path", t.path, "ext", filepath.Ext(t.path))

	bufferSize := 4096
	buffer := make([]byte, bufferSize)
	maxLoglineSize := 2000000 // 2 MB
	scanner := bufio.NewScanner(r)
	scanner.Buffer(buffer, maxLoglineSize)
	for line := 1; ; line++ {
		if !scanner.Scan() {
			break
		}

		if scannerErr := scanner.Err(); scannerErr != nil {
			if scannerErr != io.EOF {
				level.Error(t.logger).Log("msg", "error scanning", "err", scannerErr)
			}

			break
		}

		if line <= int(t.position) {
			// skip already seen lines.
			continue
		}

		text := scanner.Text()
		var finalText string
		if t.decoder != nil {
			var err error
			finalText, err = t.convertToUTF8(text)
			if err != nil {
				level.Debug(t.logger).Log("msg", "failed to convert encoding", "error", err)
				t.metrics.encodingFailures.WithLabelValues(t.path).Inc()
				finalText = fmt.Sprintf("the requested encoding conversion for this line failed in Promtail/Grafana Agent: %s", err.Error())
			}
		} else {
			finalText = text
		}

		t.metrics.readLines.WithLabelValues(t.path).Inc()

		entries <- api.Entry{
			Labels: model.LabelSet{},
			Entry: logproto.Entry{
				Timestamp: time.Now(),
				Line:      finalText,
			},
		}

		t.size = int64(unsafe.Sizeof(finalText))
		t.position++
	}
}

func (t *decompressor) MarkPositionAndSize() error {
	// Lock this update as there are 2 timers calling this routine, the sync in filetarget and the positions sync in this file.
	t.posAndSizeMtx.Lock()
	defer t.posAndSizeMtx.Unlock()

	t.metrics.totalBytes.WithLabelValues(t.path).Set(float64(t.size))
	t.metrics.readBytes.WithLabelValues(t.path).Set(float64(t.position))
	t.positions.Put(t.path, t.position)

	return nil
}

func (t *decompressor) Stop() {
	// stop can be called by two separate threads in filetarget, to avoid a panic closing channels more than once
	// we wrap the stop in a sync.Once.
	t.stopOnce.Do(func() {
		// Shut down the position marker thread
		close(t.posquit)
		<-t.posdone

		// Save the current position before shutting down tailer
		if err := t.MarkPositionAndSize(); err != nil {
			level.Error(t.logger).Log("msg", "error marking file position when stopping decompressor", "path", t.path, "error", err)
		}

		// Wait for readLines() to consume all the remaining messages and exit when the channel is closed
		<-t.done
		level.Info(t.logger).Log("msg", "stopped decompressor", "path", t.path)
		t.handler.Stop()
	})
}

func (t *decompressor) IsRunning() bool {
	return t.running.Load()
}

func (t *decompressor) convertToUTF8(text string) (string, error) {
	res, _, err := transform.String(t.decoder, text)
	if err != nil {
		return "", errors.Wrap(err, "error decoding text")
	}

	return res, nil
}

// cleanupMetrics removes all metrics exported by this tailer
func (t *decompressor) cleanupMetrics() {
	// When we stop tailing the file, also un-export metrics related to the file
	t.metrics.filesActive.Add(-1.)
	t.metrics.readLines.DeleteLabelValues(t.path)
	t.metrics.readBytes.DeleteLabelValues(t.path)
	t.metrics.totalBytes.DeleteLabelValues(t.path)
}

func (t *decompressor) Path() string {
	return t.path
}
