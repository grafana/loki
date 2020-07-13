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

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/promtail/api"
	"github.com/grafana/loki/pkg/promtail/positions"
)

type objectReader struct {
	storeName    string
	handler      api.EntryHandler
	objectClient chunk.ObjectClient
	logger       log.Logger
	readerMtx    *sync.RWMutex
	labels       model.LabelSet
	positions    positions.Positions
	quit         chan struct{}
	done         chan struct{}
	object       *promtailObject
	active       bool
}

type promtailObject struct {
	Key        string
	ModifiedAt time.Time
	Size       int64
	BytesRead  int64
}

func newObjectReader(storeName string, object chunk.StorageObject, objectClient chunk.ObjectClient, logger log.Logger, labels model.LabelSet, positions positions.Positions, handler api.EntryHandler, pos int64) (*objectReader, error) {
	objectReader := &objectReader{
		storeName:    storeName,
		logger:       logger,
		labels:       labels,
		positions:    positions,
		handler:      handler,
		quit:         make(chan struct{}),
		done:         make(chan struct{}),
		objectClient: objectClient,
		active:       true,
		object: &promtailObject{
			Key:        object.Key,
			ModifiedAt: object.ModifiedAt,
			BytesRead:  pos,
			Size:       0,
		},
	}

	go objectReader.run()
	ObjectsActive.Add(1.)
	return objectReader, nil
}

func (r *objectReader) run() {
	level.Info(r.logger).Log("msg", "start reading object", "object", r.object.Key)
	bufReader, err := r.GetObject()
	if err != nil {
		level.Error(r.logger).Log("msg", "error in fetching and initializing object reader", "err", err)
		r.readerMtx.Lock()
		r.active = false
		r.readerMtx.Unlock()
		return
	}

	// add s3_object label
	labels := r.labels.Merge(model.LabelSet{
		model.LabelName("s3_object"): model.LabelValue(r.object.Key),
	})

	positionSyncPeriod := r.positions.SyncPeriod()
	positionWait := time.NewTicker(positionSyncPeriod)

	defer func() {
		positionWait.Stop()
		close(r.done)
	}()

	for {
		select {
		case <-r.quit:
			return
		case <-positionWait.C:
			r.markPositionAndSize()
		default:
			bytes, err := bufReader.ReadBytes('\n')
			if err != nil {
				if err == io.EOF {
					level.Debug(r.logger).Log("msg", "reached EOF while reading object", "object", r.object.Key)
					r.object.BytesRead += int64(len(bytes))
					r.markPositionAndSize()
					r.readerMtx.Lock()
					r.active = false
					r.readerMtx.Unlock()
					return
				}
				level.Error(r.logger).Log("msg", "error in reading line", "object", r.object.Key)
			}

			time := time.Now().UTC()
			line := strings.TrimRight(string(bytes), "\n")

			// add metrics
			objectReadLines.WithLabelValues(r.object.Key, r.storeName).Inc()
			objectLogLengthHistogram.WithLabelValues(r.object.Key, r.storeName).Observe(float64(len(bytes)))

			// send line, timestamp and labels
			if err := r.handler.Handle(labels, time, line); err != nil {
				level.Error(r.logger).Log("msg", "error handling line", "line", string(bytes), "error", err)
			}

			// update bytes read
			r.object.BytesRead += int64(len(bytes))
		}
	}

}

func (r *objectReader) markPositionAndSize() {
	r.readerMtx.Lock()
	defer r.readerMtx.Unlock()

	// update positions
	positionKey := fmt.Sprintf("s3object-%s", r.object.Key)
	positionValue := fmt.Sprintf("%s:%s:%s", strconv.FormatInt(r.object.ModifiedAt.UnixNano(), 10), strconv.FormatInt(r.object.BytesRead, 10), strconv.FormatInt(r.object.Size, 10))
	r.positions.PutString(positionKey, positionValue)

	objectReadBytes.WithLabelValues(r.object.Key, r.storeName).Set(float64(r.object.BytesRead))
	objectTotalBytes.WithLabelValues(r.object.Key, r.storeName).Set(float64(r.object.Size))
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

	if err = r.setObjectSize(bufReader); err != nil {
		return nil, errors.Wrap(err, "failed to set object size")
	}

	availableBytes, err := bufReader.Peek(int(r.object.BytesRead))
	if err != nil {
		level.Warn(r.logger).Log("msg", "object size is less than previous known size. object will be tailed from begining", "object", r.object.Key)
		r.positions.Remove(fmt.Sprintf("s3object-%s", r.object.Key))
		r.object.BytesRead = 0
		return bufReader, nil
	}

	// skip the bytes which we have already read
	_, err = bufReader.Discard(len(availableBytes))
	if err != nil {
		return nil, err
	}

	level.Info(r.logger).Log("msg", "previously known object. object will be tailed from the previous known position", "object", r.object.Key, "position", r.object.BytesRead)
	return bufReader, nil
}

func (r *objectReader) setObjectSize(bufReader *bufio.Reader) error {
	// read the byte so that we can get the size of the underlying buffer
	_, err := bufReader.ReadByte()
	if err != nil {
		return err
	}

	// set object size
	r.object.Size = 1 + int64(bufReader.Buffered())

	// unread the byte
	if err = bufReader.UnreadByte(); err != nil {
		return err
	}

	return nil
}
func (r *objectReader) getReader(reader io.ReadCloser) (*bufio.Reader, error) {
	// identify the file type
	buf := [3]byte{}
	n, err := io.ReadAtLeast(reader, buf[:], len(buf))
	if err != nil {
		return nil, err
	}

	rd := io.MultiReader(bytes.NewReader(buf[:n]), reader)
	if isGzip(buf) {
		r, err := gzip.NewReader(rd)
		if err != nil {
			return nil, err
		}
		rd = r
	}

	bufReader := bufio.NewReader(rd)
	return bufReader, nil
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

	close(r.quit)
	<-r.done
	ObjectsActive.Add(-1.)

	// un-export metrics related to the file
	objectReadLines.DeleteLabelValues(r.object.Key, r.storeName)
	objectReadBytes.DeleteLabelValues(r.object.Key, r.storeName)
	objectTotalBytes.DeleteLabelValues(r.object.Key, r.storeName)
	objectLogLengthHistogram.DeleteLabelValues(r.object.Key, r.storeName)

	level.Info(r.logger).Log("msg", "stopped reading object", "object", r.object.Key)
}
