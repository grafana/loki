package objectstore

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/promtail/api"
	"github.com/grafana/loki/pkg/promtail/positions"
	"github.com/grafana/loki/pkg/promtail/scrapeconfig"
	"github.com/grafana/loki/pkg/promtail/targets/target"
)

var (
	objectReadBytes = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "promtail",
		Name:      "object_read_bytes_total",
		Help:      "Number of bytes read.",
	}, []string{"key", "store"})
	objectTotalBytes = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "promtail",
		Name:      "object_file_bytes_total",
		Help:      "Number of bytes total.",
	}, []string{"key", "store"})
	objectReadLines = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "promtail",
		Name:      "object_read_lines_total",
		Help:      "Number of lines read.",
	}, []string{"key", "store"})
	ObjectsActive = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "promtail",
		Name:      "object_files_active_total",
		Help:      "Number of active files.",
	})
	objectLogLengthHistogram = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "promtail",
		Name:      "object_log_entries_bytes",
		Help:      "the total count of bytes",
		Buckets:   prometheus.ExponentialBuckets(16, 2, 8),
	}, []string{"key", "store"})
)

// ObjectTarget describes Object target
type ObjectTarget struct {
	storeName    string
	logger       log.Logger
	handler      api.EntryHandler
	positions    positions.Positions
	labels       model.LabelSet
	watch        map[string]*objectReader
	quit         chan struct{}
	done         chan struct{}
	objectClient chunk.ObjectClient
	prefix       string
	syncPeriod   time.Duration
}

// NewObjectTarget creates a new ObjectTarget.
func NewObjectTarget(logger log.Logger,
	handler api.EntryHandler,
	positions positions.Positions,
	jobName string,
	objectClient chunk.ObjectClient,
	config *scrapeconfig.Config) (*ObjectTarget, error) {

	t := &ObjectTarget{
		logger:       logger,
		handler:      handler,
		positions:    positions,
		quit:         make(chan struct{}),
		done:         make(chan struct{}),
		objectClient: objectClient,
		watch:        map[string]*objectReader{},
	}

	// add prefix label
	if config.S3Config != nil {
		t.labels = config.S3Config.Labels.Merge(model.LabelSet{
			model.LabelName("s3_prefix"): model.LabelValue(config.S3Config.Prefix),
		})
		t.prefix = config.S3Config.Prefix
		t.syncPeriod = config.S3Config.SyncPeriod
		t.storeName = "s3"
	}

	go t.run()
	return t, nil
}

func (t *ObjectTarget) run() {
	defer func() {
		for _, v := range t.watch {
			level.Info(t.logger).Log("msg", "updating object last position", "object", v.object.Key)
			v.markPositionAndSize()
			level.Info(t.logger).Log("msg", "stopping object reader", "object", v.object.Key)
			v.stop()
		}
		close(t.done)
	}()

	ticker := time.NewTicker(t.syncPeriod)
	for {
		select {
		case <-ticker.C:
			objects, _, err := t.objectClient.List(context.Background(), t.prefix)
			if err != nil {
				level.Error(t.logger).Log("msg", "failed to list objects", "error", err)
				continue
			}
			t.handleObjects(objects)
		case <-t.quit:
			return
		}
	}
}

func (t *ObjectTarget) handleObjects(objects []chunk.StorageObject) {
	for _, object := range objects {
		if v, ok := t.watch[object.Key]; ok {
			v.readerMtx.RLock()
			readerActive := v.active
			v.readerMtx.RUnlock()
			if readerActive {
				continue
			}
			level.Info(t.logger).Log("msg", "stopping object reader", "object", v.object.Key)
			v.stop()
			delete(t.watch, object.Key)
			continue
		}

		// check if the object's position is already saved
		modifiedAt, pos, size, err := t.getPosition(object.Key)
		if err != nil {
			level.Error(t.logger).Log("msg", "error in getting position for object", "error", err, "object", object.Key)
			continue
		}

		// skip any object which is already read completely
		if modifiedAt == object.ModifiedAt.UnixNano() && pos > 0 && pos == size {
			level.Debug(t.logger).Log("msg", "object completely read, skipping object read", "object", object.Key)
			continue
		}

		objectReader, err := newObjectReader(t.storeName, object, t.objectClient, t.logger, t.labels, t.positions, t.handler, pos)
		if err != nil {
			level.Error(t.logger).Log("msg", "error in initialzing object reader", "error", err)
			continue
		}
		t.watch[object.Key] = objectReader
	}
}

func (t *ObjectTarget) getPosition(key string) (int64, int64, int64, error) {
	positionString := t.positions.GetString(fmt.Sprintf("s3object-%s", key))
	if positionString == "" {
		return 0, 0, 0, nil
	}
	position := strings.Split(positionString, ":")
	if len(position) != 3 {
		return 0, 0, 0, errors.New(fmt.Sprintf("position value is wrong, expected 3 values, found %d", len(position)))
	}
	modifiedAt, err := strconv.ParseInt(position[0], 10, 64)
	pos, err := strconv.ParseInt(position[1], 10, 64)
	size, err := strconv.ParseInt(position[2], 10, 64)
	if err != nil {
		return 0, 0, 0, err
	}
	return modifiedAt, pos, size, nil
}

// Ready if at least one object is being read.
func (t *ObjectTarget) Ready() bool {
	return len(t.watch) > 0
}

// Stop the target.
func (t *ObjectTarget) Stop() {
	close(t.quit)
	<-t.done
}

// Type returns ObjectTargetType.
func (t *ObjectTarget) Type() target.TargetType {
	return target.ObjectTargetType
}

// DiscoveredLabels returns the set of labels discovered by the object target, which
// is always nil.
func (t *ObjectTarget) DiscoveredLabels() model.LabelSet {
	return nil
}

// Labels returns the set of labels that statically apply to all log entries
// produced by the ObjectTarget.
func (t *ObjectTarget) Labels() model.LabelSet {
	return t.labels
}

// Details of the target.
func (t *ObjectTarget) Details() interface{} {
	objects := map[string]int64{}
	for objectName := range t.watch {
		_, objects[objectName], _, _ = t.getPosition(objectName)
	}
	return objects
}
