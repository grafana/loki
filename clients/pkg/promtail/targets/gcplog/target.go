package gcplog

import (
	"context"
	"sync"

	"cloud.google.com/go/pubsub"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/relabel"

	"github.com/grafana/loki/clients/pkg/promtail/api"
	"github.com/grafana/loki/clients/pkg/promtail/scrapeconfig"
	"github.com/grafana/loki/clients/pkg/promtail/targets/target"
)

// GcplogTarget represents the target specific to GCP project.
// It collects logs from GCP and push it to Loki.
// nolint:revive
type GcplogTarget struct {
	metrics       *Metrics
	logger        log.Logger
	handler       api.EntryHandler
	config        *scrapeconfig.GcplogTargetConfig
	relabelConfig []*relabel.Config
	jobName       string

	// lifecycle management
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// pubsub
	ps   *pubsub.Client
	msgs chan *pubsub.Message
}

// NewGcplogTarget returns the new instannce of GcplogTarget for
// the given `project-id`. It scraps logs from the GCP project
// and push it Loki via given `api.EntryHandler.`
// It starts the `run` loop to consume log entries that can be
// stopped via `target.Stop()`
// nolint:revive,govet
func NewGcplogTarget(
	metrics *Metrics,
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

	target := newGcplogTarget(metrics, logger, handler, relabel, jobName, config, ps, ctx, cancel)

	go func() {
		_ = target.run()
	}()

	return target, nil
}

// nolint:revive
func newGcplogTarget(
	metrics *Metrics,
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
		metrics:       metrics,
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
	t.wg.Add(1)
	defer t.wg.Done()

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
			level.Error(t.logger).Log("msg", "failed to receive pubsub messages", "error", err)
			t.metrics.gcplogErrors.WithLabelValues(t.config.ProjectID).Inc()
			t.metrics.gcplogTargetLastSuccessScrape.WithLabelValues(t.config.ProjectID, t.config.Subscription).SetToCurrentTime()
		}
	}()

	for {
		select {
		case <-t.ctx.Done():
			return t.ctx.Err()
		case m := <-t.msgs:
			entry, err := format(m, t.config.Labels, t.config.UseIncomingTimestamp, t.relabelConfig)
			if err != nil {
				level.Error(t.logger).Log("event", "error formating log entry", "cause", err)
				m.Ack()
				break
			}
			send <- entry
			m.Ack() // Ack only after log is sent.
			t.metrics.gcplogEntries.WithLabelValues(t.config.ProjectID).Inc()
		}
	}
}

// sent implements target.Target.
func (t *GcplogTarget) Type() target.TargetType {
	return target.GcplogTargetType
}

func (t *GcplogTarget) Ready() bool {
	// Return true just like all other targets.
	// Rationale is gcplog scraping shouldn't stop because of some transient timeout errors.
	// This transient failure can cause promtail readyness probe to fail which may prevent pod from starting.
	// We have metrics now to track if scraping failed (`gcplog_target_last_success_scrape`).
	return true
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
	t.cancel()
	t.wg.Wait()
	t.handler.Stop()
	return nil
}
