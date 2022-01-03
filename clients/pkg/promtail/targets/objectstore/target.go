package objectstore

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/clients/pkg/promtail/api"
	"github.com/grafana/loki/clients/pkg/promtail/positions"
	"github.com/grafana/loki/clients/pkg/promtail/targets/target"
	"github.com/grafana/loki/pkg/storage/chunk"
)

// Target describestarget
type Target struct {
	metrics   *Metrics
	storeName string
	logger    log.Logger
	handler   api.EntryHandler
	positions positions.Positions
	labels    model.LabelSet
	watch     map[string]*objectReader

	quit chan struct{}
	done chan struct{}

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	objectClient chunk.ObjectClient
	client       Client
	prefix       string
	timeout      int64
	reset_cursor bool
}

// NewTarget creates a new Target.
func NewTarget(metrics *Metrics, logger log.Logger,
	handler api.EntryHandler,
	positions positions.Positions,
	objectClient chunk.ObjectClient,
	client Client,
	labels model.LabelSet,
	timeout int64,
	reset_cursor bool,
	storename string) (*Target, error) {

	ctx, cancel := context.WithCancel(context.Background())
	t := &Target{
		metrics:      metrics,
		logger:       logger,
		handler:      handler,
		positions:    positions,
		quit:         make(chan struct{}),
		done:         make(chan struct{}),
		objectClient: objectClient,
		client:       client,
		watch:        map[string]*objectReader{},
		ctx:          ctx,
		cancel:       cancel,
		labels:       labels,
		storeName:    storename,
		timeout:      timeout,
		reset_cursor: reset_cursor,
	}
	go t.run()
	return t, nil
}

func (t *Target) run() {
	t.wg.Add(1)
	defer func() {
		for _, v := range t.watch {
			level.Info(t.logger).Log("msg", "updating object last position", "object", v.object.Key)
			v.markPositionAndSize()
			level.Info(t.logger).Log("msg", "stopping object reader", "object", v.object.Key)
			v.stop()
		}
		t.wg.Done()
	}()

	for t.ctx.Err() == nil {
		// ReceiveMessage blocks for timeout seconds.
		objects, err := t.client.ReceiveMessage(t.timeout)
		if err != nil {
			level.Error(t.logger).Log("msg", "error in receiving objects", "error", err)
		}

		// delete non active object readers from watching
		for key, object := range t.watch {
			if !object.active.Load() {
				delete(t.watch, key)
			}
		}

		for _, object := range objects {
			t.handleObject(object)
		}

	}
}

func (t *Target) handleObject(object messageObject) {
	// check if the object's position is already saved
	modifiedAt, pos, err := t.getPosition(object.Object.Key)
	if err != nil {
		level.Error(t.logger).Log("msg", "error in getting position for object", "error", err, "object", object.Object.Key)
	}
	level.Debug(t.logger).Log("msg", "object position details", "object", object.Object.Key, "modified_at", modifiedAt, "position", pos)

	v, ok := t.watch[object.Object.Key]
	if ok {
		readerActive := v.active.Load()
		// if the object is still being read and we receive the same object we should
		// not create one more reader.
		if readerActive && modifiedAt == object.Object.ModifiedAt.UnixNano() {
			level.Debug(t.logger).Log("msg", "received same object. object is still being read. not creating new reader", "object", object.Object.Key, "received_modified_at", object.Object.ModifiedAt.UnixNano(), "saved_modified_at", modifiedAt)
			return
		}
		if readerActive {
			level.Info(t.logger).Log("msg", "stopping active object reader", "object", v.object.Key)
			v.stop()
		}
	}

	if t.reset_cursor {
		level.Debug(t.logger).Log("msg", "reset_cursor set to true. reading object from begining", "object", object.Object.Key, "modified_at", modifiedAt, "position", pos)
	}

	objectReader := newObjectReader(t.metrics, t.storeName, object.Object, object.Acknowledge, t.objectClient, t.logger, t.labels, t.positions, t.handler, pos)
	t.watch[object.Object.Key] = objectReader
}

func (t *Target) getPosition(key string) (int64, int64, error) {
	positionString := t.positions.GetString(fmt.Sprintf("s3object-%s", key))
	if positionString == "" {
		return 0, 0, nil
	}

	position := strings.Split(positionString, ":")
	if len(position) != 2 {
		return 0, 0, errors.New(fmt.Sprintf("position value is wrong, expected 2 values, found %d", len(position)))
	}

	modifiedAt, err := strconv.ParseInt(position[0], 10, 64)
	if err != nil {
		return 0, 0, err
	}

	pos, err := strconv.ParseInt(position[1], 10, 64)
	if err != nil {
		return 0, 0, err
	}

	return modifiedAt, pos, nil
}

// Ready if at least one object is being read.
func (t *Target) Ready() bool {
	return len(t.watch) > 0
}

// Stop the target.
func (t *Target) Stop() {
	t.cancel()
	t.wg.Wait()
	t.handler.Stop()
}

// Type returns TargetType.
func (t *Target) Type() target.TargetType {
	return target.ObjectTargetType
}

// DiscoveredLabels returns the set of labels discovered by the object target, which
// is always nil.
func (t *Target) DiscoveredLabels() model.LabelSet {
	return nil
}

// Labels returns the set of labels that statically apply to all log entries
// produced by the Target.
func (t *Target) Labels() model.LabelSet {
	return t.labels
}

// Details of the target.
func (t *Target) Details() interface{} {
	objects := map[string]int64{}
	for objectName := range t.watch {
		_, objects[objectName], _ = t.getPosition(objectName)
	}
	return objects
}
