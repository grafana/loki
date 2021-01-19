package gcplog

import (
	"context"

	"cloud.google.com/go/pubsub"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/relabel"

	"github.com/grafana/loki/pkg/promtail/api"
	"github.com/grafana/loki/pkg/promtail/scrapeconfig"
	"github.com/grafana/loki/pkg/promtail/targets/target"
)

var (
	gcplogEntries = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "promtail",
		Name:      "gcplog_target_entries_total",
		Help:      "Help number of successful entries sent to the gcplog target",
	}, []string{"project"})

	gcplogErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "promtail",
		Name:      "gcplog_target_parsing_errors_total",
		Help:      "Total number of parsing errors while receiving gcplog messages",
	}, []string{"project"})
)

// GcplogTarget represents the target specific to GCP project.
// It collects logs from GCP and push it to Loki.
// nolint:golint
type GcplogTarget struct {
	logger        log.Logger
	handler       api.EntryHandler
	config        *scrapeconfig.GcplogTargetConfig
	relabelConfig []*relabel.Config
	jobName       string

	// lifecycle management
	ctx    context.Context
	cancel context.CancelFunc

	// pubsub
	ps   *pubsub.Client
	msgs chan *pubsub.Message
}

// NewGcplogTarget returns the new instannce of GcplogTarget for
// the given `project-id`. It scraps logs from the GCP project
// and push it Loki via given `api.EntryHandler.`
// It starts the `run` loop to consume log entries that can be
// stopped via `target.Stop()`
// nolint:golint,govet
func NewGcplogTarget(
	logger log.Logger,
	handler api.EntryHandler,
	relabel []*relabel.Config,
	jobName string,
	config *scrapeconfig.GcplogTargetConfig,
) (*GcplogTarget, error) {

	ctx, cancel := context.WithCancel(context.Background())

	ps, err := pubsub.NewClient(ctx, config.ProjectID)
	if err != nil {
		return nil, err
	}

	target := newGcplogTarget(logger, handler, relabel, jobName, config, ps, ctx, cancel)

	go func() {
		_ = target.run()
	}()

	return target, nil
}

// nolint: golint
func newGcplogTarget(
	logger log.Logger,
	handler api.EntryHandler,
	relabel []*relabel.Config,
	jobName string,
	config *scrapeconfig.GcplogTargetConfig,
	pubsubClient *pubsub.Client,
	ctx context.Context,
	cancel func(),
) *GcplogTarget {

	return &GcplogTarget{
		logger:        logger,
		handler:       handler,
		relabelConfig: relabel,
		config:        config,
		jobName:       jobName,
		ctx:           ctx,
		cancel:        cancel,
		ps:            pubsubClient,
		msgs:          make(chan *pubsub.Message),
	}
}

func (t *GcplogTarget) run() error {
	send := t.handler.Chan()

	sub := t.ps.SubscriptionInProject(t.config.Subscription, t.config.ProjectID)
	go func() {
		// NOTE(kavi): `cancel` the context as exiting from this goroutine should stop main `run` loop
		// It makesense as no more messages will be received.
		defer t.cancel()

		err := sub.Receive(t.ctx, func(ctx context.Context, m *pubsub.Message) {
			t.msgs <- m
		})
		if err != nil {
			// TODO(kavi): Add proper error propagation maybe?
			level.Error(t.logger).Log("error", err)
			gcplogErrors.WithLabelValues(t.config.ProjectID).Inc()
		}
	}()

	for {
		select {
		case <-t.ctx.Done():
			return t.ctx.Err()
		case m := <-t.msgs:
			entry, err := format(m, t.config.Labels, t.config.UseIncomingTimestamp)
			if err != nil {
				level.Error(t.logger).Log("event", "error formating log entry", "cause", err)
				m.Ack()
				break
			}
			send <- entry
			m.Ack() // Ack only after log is sent.
			gcplogEntries.WithLabelValues(t.config.ProjectID).Inc()
		}
	}
}

// sent implements target.Target.
func (t *GcplogTarget) Type() target.TargetType {
	return target.GcplogTargetType
}

func (t *GcplogTarget) Ready() bool {
	return t.ctx.Err() == nil
}

func (t *GcplogTarget) DiscoveredLabels() model.LabelSet {
	return nil
}

func (t *GcplogTarget) Labels() model.LabelSet {
	return t.config.Labels
}

func (t *GcplogTarget) Details() interface{} {
	return nil
}

func (t *GcplogTarget) Stop() error {
	t.handler.Stop()
	t.cancel()
	return nil
}
