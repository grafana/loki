package objectstore

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"go.uber.org/atomic"

	"github.com/grafana/loki/clients/pkg/promtail/api"
	"github.com/grafana/loki/clients/pkg/promtail/positions"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/storage/chunk"
)

type objectReader struct {
	metrics      *Metrics
	storeName    string
	handler      api.EntryHandler
	objectClient chunk.ObjectClient
	ackMessage   ackMessage
	logger       log.Logger
	readerMtx    sync.RWMutex
	labels       model.LabelSet
	positions    positions.Positions
	object       *promtailObject
	active       *atomic.Bool
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
}

type promtailObject struct {
	Key        string
	ModifiedAt time.Time
	BytesRead  int64
}

func newObjectReader(
	metrics *Metrics,
	storeName string,
	object chunk.StorageObject,
	ackMessage ackMessage,
	objectClient chunk.ObjectClient,
	logger log.Logger,
	labels model.LabelSet,
	positions positions.Positions,
	handler api.EntryHandler,
	pos int64) *objectReader {
	ctx, cancel := context.WithCancel(context.Background())
	objectReader := &objectReader{
		metrics:      metrics,
		storeName:    storeName,
		logger:       logger,
		labels:       labels,
		positions:    positions,
		handler:      handler,
		objectClient: objectClient,
		ackMessage:   ackMessage,
		active:       atomic.NewBool(true),
		ctx:          ctx,
		cancel:       cancel,
		object: &promtailObject{
			Key:        object.Key,
			ModifiedAt: object.ModifiedAt,
			BytesRead:  pos,
		},
	}

	go objectReader.run()
	metrics.objectActive.Add(1.)
	return objectReader
}

func (r *objectReader) run() {
	level.Info(r.logger).Log("msg", "start reading object", "object", r.object.Key)
	r.wg.Add(1)
	positionSyncPeriod := r.positions.SyncPeriod()
	positionWait := time.NewTicker(positionSyncPeriod)
	defer func() {
		positionWait.Stop()
		r.wg.Done()
	}()

	bufReader, err := r.GetObject()
	if err != nil {
		level.Error(r.logger).Log("msg", "error in fetching and initializing object reader", "err", err)
		r.active.Store(false)
		return
	}

	// add s3_object label
	labels := r.labels.Merge(model.LabelSet{
		model.LabelName("s3_object"): model.LabelValue(r.object.Key),
	})

	for {
		select {
		case <-r.ctx.Done():
			return
		case <-positionWait.C:
			r.markPositionAndSize()
		default:
			bytes, err := bufReader.ReadBytes('\n')
			if err != nil {
				if err == io.EOF {
					level.Debug(r.logger).Log("msg", "reached EOF while reading object", "object", r.object.Key)
					r.sendLines(bytes, labels) // send any pending bytes
					r.object.BytesRead += int64(len(bytes))
					r.markPositionAndSize()
					r.active.Store(false)
					r.ackMessage()
					return

				}
				level.Error(r.logger).Log("msg", "error in reading line", "object", r.object.Key)
			}
			r.sendLines(bytes, labels)
		}
	}

}

func (r *objectReader) sendLines(bytes []byte, labels model.LabelSet) {
	if len(bytes) < 1 {
		return
	}
	time := time.Now().UTC()
	line := strings.TrimRight(string(bytes), "\r\n")

	// add metrics
	r.metrics.objectReadLines.WithLabelValues(r.object.Key, r.storeName).Inc()
	r.metrics.objectLogLengthHistogram.WithLabelValues(r.object.Key, r.storeName).Observe(float64(len(bytes)))

	r.handler.Chan() <- api.Entry{
		Labels: labels,
		Entry: logproto.Entry{
			Timestamp: time,
			Line:      string(line),
		},
	}
	// update bytes read
	r.object.BytesRead += int64(len(bytes))
}
func (r *objectReader) markPositionAndSize() {
	r.readerMtx.Lock()
	defer r.readerMtx.Unlock()

	// update positions
	positionKey := fmt.Sprintf("object-%s", r.object.Key)
	positionValue := fmt.Sprintf("%s:%s", strconv.FormatInt(r.object.ModifiedAt.UnixNano(), 10), strconv.FormatInt(r.object.BytesRead, 10))
	r.positions.PutString(positions.CursorKey(positionKey), positionValue)

	r.metrics.objectReadBytes.WithLabelValues(r.object.Key, r.storeName).Set(float64(r.object.BytesRead))
}

func (r *objectReader) GetObject() (*bufio.Reader, error) {
	reader, err := r.objectClient.GetObject(context.Background(), r.object.Key)
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch object")
	}

	bufReader, err := r.getReader(reader)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get reader")
	}

	if r.object.BytesRead == 0 {
		return bufReader, nil
	}

	// if the object is previously known, we have to skip the previously read bytes.
	_, err = bufReader.Peek(int(r.object.BytesRead))
	if err != nil {
		level.Warn(r.logger).Log("msg", "object size is less than previous known size. object will be tailed from begining", "object", r.object.Key)
		r.positions.Remove(fmt.Sprintf("s3object-%s", r.object.Key))
		r.object.BytesRead = 0
		return bufReader, nil
	}

	// skip the bytes which we have already read
	_, err = bufReader.Discard(int(r.object.BytesRead))
	if err != nil {
		return nil, err
	}

	level.Info(r.logger).Log("msg", "previously known object. object will be tailed from the previous known position", "object", r.object.Key, "position", r.object.BytesRead)
	return bufReader, nil
}

func (r *objectReader) getReader(reader io.ReadCloser) (*bufio.Reader, error) {
	// identify the file type
	buf := [3]byte{}
	n, err := io.ReadAtLeast(reader, buf[:], len(buf))
	if err != nil {
		return nil, errors.Wrap(err, "failed to read initial bytes")
	}

	rd := io.MultiReader(bytes.NewReader(buf[:n]), reader)

	if isGzip(buf) {
		gzipReader, err := gzip.NewReader(reader)
		if err != nil {
			return nil, errors.Wrap(err, "failed to initialize gzip reader")
		}
		rd = gzipReader
	}

	return bufio.NewReader(rd), nil
}

func isGzip(buf [3]byte) bool {
	if buf[0] == 0x1F && buf[1] == 0x8B && buf[2] == 0x8 {
		return true
	}
	return false
}

func (r *objectReader) stop() {
	// Save the current position before shutting down object reader
	r.markPositionAndSize()

	r.cancel()
	r.wg.Wait()
	r.metrics.objectActive.Add(-1.)

	// un-export metrics related to the file
	r.metrics.objectReadLines.DeleteLabelValues(r.object.Key, r.storeName)
	r.metrics.objectReadBytes.DeleteLabelValues(r.object.Key, r.storeName)
	r.metrics.objectLogLengthHistogram.DeleteLabelValues(r.object.Key, r.storeName)

	level.Info(r.logger).Log("msg", "stopped reading object", "object", r.object.Key)
}
