package ruler

import (
	"context"
	"errors"

	"github.com/cortexproject/cortex/pkg/cortexpb"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/prometheus/pkg/exemplar"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/rules"
	"github.com/prometheus/prometheus/storage"

	"github.com/grafana/loki/pkg/util"
)

type RemoteWriteAppendable struct {
	groupAppender map[string]*RemoteWriteAppender

	userID    string
	cfg       Config
	overrides RulesLimits
	logger    log.Logger

	metrics *remoteWriteMetrics
}

func newRemoteWriteAppendable(cfg Config, overrides RulesLimits, logger log.Logger, userID string, metrics *remoteWriteMetrics) *RemoteWriteAppendable {
	return &RemoteWriteAppendable{
		logger:        logger,
		userID:        userID,
		cfg:           cfg,
		overrides:     overrides,
		groupAppender: make(map[string]*RemoteWriteAppender),
		metrics:       metrics,
	}
}

type RemoteWriteAppender struct {
	logger       log.Logger
	ctx          context.Context
	remoteWriter RemoteWriter
	userID       string
	groupKey     string

	queue   *util.EvictingQueue
	metrics *remoteWriteMetrics
}

func (a *RemoteWriteAppendable) Appender(ctx context.Context) storage.Appender {
	groupKey := retrieveGroupKeyFromContext(ctx)

	capacity := a.overrides.RulerRemoteWriteQueueCapacity(a.userID)

	// create or retrieve an appender associated with this groupKey (unique ID for rule group)
	appender, found := a.groupAppender[groupKey]
	if found {
		err := appender.WithQueueCapacity(capacity)
		if err != nil {
			level.Warn(a.logger).Log("msg", "attempting to set capacity failed", "err", err)
		}

		return appender
	}

	client, err := NewRemoteWriter(a.cfg, a.userID)
	if err != nil {
		level.Error(a.logger).Log("msg", "error creating remote-write client; setting appender as noop", "err", err, "tenant", a.userID)
		return &NoopAppender{}
	}

	queue, err := util.NewEvictingQueue(capacity, a.onEvict(a.userID, groupKey))
	if err != nil {
		level.Error(a.logger).Log("msg", "queue creation error; setting appender as noop", "err", err, "tenant", a.userID)
		return &NoopAppender{}
	}

	appender = &RemoteWriteAppender{
		ctx:          ctx,
		logger:       a.logger,
		remoteWriter: client,
		groupKey:     groupKey,
		userID:       a.userID,

		queue:   queue,
		metrics: a.metrics,
	}

	// only track reference if groupKey was retrieved
	if groupKey == "" {
		level.Warn(a.logger).Log("msg", "blank group key passed via context; creating new appender")
		return appender
	}

	a.groupAppender[groupKey] = appender
	return appender
}

func (a *RemoteWriteAppendable) onEvict(userID, groupKey string) func() {
	return func() {
		a.metrics.samplesEvicted.WithLabelValues(userID, groupKey).Inc()
	}
}

func (a *RemoteWriteAppender) Append(_ uint64, l labels.Labels, t int64, v float64) (uint64, error) {
	a.queue.Append(TimeSeriesEntry{
		Labels: l,
		Sample: cortexpb.Sample{
			Value:       v,
			TimestampMs: t,
		},
	})

	a.metrics.samplesQueued.WithLabelValues(a.userID, a.groupKey).Set(float64(a.queue.Length()))
	a.metrics.samplesQueuedTotal.WithLabelValues(a.userID, a.groupKey).Inc()

	return 0, nil
}

func (a *RemoteWriteAppender) AppendExemplar(_ uint64, _ labels.Labels, _ exemplar.Exemplar) (uint64, error) {
	return 0, errors.New("exemplars are unsupported")
}

func (a *RemoteWriteAppender) Commit() error {
	if a.queue.Length() <= 0 {
		return nil
	}

	if a.remoteWriter == nil {
		level.Warn(a.logger).Log("msg", "no remote_write client defined, skipping commit")
		return nil
	}

	level.Debug(a.logger).Log("msg", "writing samples to remote_write target", "target", a.remoteWriter.Endpoint(), "count", a.queue.Length())

	req, err := a.remoteWriter.PrepareRequest(a.queue)
	if err != nil {
		level.Error(a.logger).Log("msg", "could not prepare remote-write request", "err", err)
		a.metrics.remoteWriteErrors.WithLabelValues(a.userID, a.groupKey).Inc()
		return err
	}

	err = a.remoteWriter.Store(a.ctx, req)
	if err != nil {
		level.Error(a.logger).Log("msg", "could not store recording rule samples", "err", err)
		a.metrics.remoteWriteErrors.WithLabelValues(a.userID, a.groupKey).Inc()
		return err
	}

	// Clear the queue on a successful response
	a.queue.Clear()

	a.metrics.samplesQueued.WithLabelValues(a.userID, a.groupKey).Set(0)

	return nil
}

func (a *RemoteWriteAppender) Rollback() error {
	a.queue.Clear()

	return nil
}

func (a *RemoteWriteAppender) WithQueueCapacity(capacity int) error {
	if err := a.queue.SetCapacity(capacity); err != nil {
		return err
	}

	a.metrics.samplesQueueCapacity.WithLabelValues(a.userID, a.groupKey).Set(float64(capacity))
	return nil
}

func retrieveGroupKeyFromContext(ctx context.Context) string {
	data, found := ctx.Value(promql.QueryOrigin{}).(map[string]interface{})
	if !found {
		return ""
	}

	ruleGroup, found := data["ruleGroup"].(map[string]string)
	if !found {
		return ""
	}

	file, found := ruleGroup["file"]
	if !found {
		return ""
	}

	name, found := ruleGroup["name"]
	if !found {
		return ""
	}

	return rules.GroupKey(file, name)
}
