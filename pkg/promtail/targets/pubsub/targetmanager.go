package pubsub

import (
	"context"
	"fmt"
	"time"

	"cloud.google.com/go/pubsub"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/promtail/api"
	"github.com/grafana/loki/pkg/promtail/scrapeconfig"
	"github.com/grafana/loki/pkg/promtail/targets/target"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/relabel"
)

var (
// TODO(kavi): metrics for pubsub target
)

type PubsubTargetManager struct {
	logger  log.Logger
	targets map[string]*PubsubTarget
}

func NewPubsubTargetManager(
	logger log.Logger,
	client api.EntryHandler,
	scrape []scrapeconfig.Config,
) (*PubsubTargetManager, error) {
	tm := &PubsubTargetManager{
		logger:  logger,
		targets: make(map[string]*PubsubTarget),
	}

	for _, cf := range scrape {
		t, err := NewPubsubTarget(logger, client, cf.RelabelConfigs, cf.JobName, cf.PubsubConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create pubsub target: %w", err)
		}
		tm.targets[cf.JobName] = t
	}

	return tm, nil
}

func (tm *PubsubTargetManager) Ready() bool {
	for _, t := range tm.targets {
		if t.Ready() {
			return true
		}
	}
	return false
}

func (tm *PubsubTargetManager) Stop() {
	for name, t := range tm.targets {
		if err := t.Stop(); err != nil {
			level.Error(t.logger).Log("event", "failed to stop pubsub target", "name", name, "cause", err)
		}
	}
}

func (tm *PubsubTargetManager) ActiveTargets() map[string][]target.Target {
	// TODO(kavi): if someway to check if specific topic is active and store the state on the target struct?
	return tm.AllTargets()
}

func (tm *PubsubTargetManager) AllTargets() map[string][]target.Target {
	res := make(map[string][]target.Target, len(tm.targets))
	for k, v := range tm.targets {
		res[k] = []target.Target{v}
	}
	return res
}

type PubsubTarget struct {
	logger        log.Logger
	handler       api.EntryHandler
	config        *scrapeconfig.PubsubTargetConfig
	relabelConfig []*relabel.Config
	jobName       string

	// lifecycle management
	ctx    context.Context
	cancel context.CancelFunc
}

func NewPubsubTarget(
	logger log.Logger,
	handler api.EntryHandler,
	relabel []*relabel.Config,
	jobName string,
	config *scrapeconfig.PubsubTargetConfig,
) (*PubsubTarget, error) {

	ctx, cancel := context.WithCancel(context.Background())

	pt := &PubsubTarget{
		logger:        logger,
		handler:       handler,
		relabelConfig: relabel,
		config:        config,
		jobName:       jobName,
		ctx:           ctx,
		cancel:        cancel,
	}

	go pt.run()

	return pt, nil
}

func (t *PubsubTarget) run() error {
	pclient, err := pubsub.NewClient(t.ctx, t.config.ProjectID)
	if err != nil {
		return fmt.Errorf("failed to create pubsub client: %w", err)
	}

	send := t.handler.Chan()

	sub := pclient.SubscriptionInProject(t.config.Subscription, t.config.ProjectID)

	labels := make(model.LabelSet)

	// TODO(kavi): take labelset from the config
	labels[model.LabelName("source")] = model.LabelValue("cloudtail")

	for {
		level.Info(t.logger).Log("event", "listening for new message")
		select {
		case <-t.ctx.Done():
			return t.ctx.Err()
		default:
			// TODO(kavi): add proper formatter
			sub.Receive(t.ctx, func(ctx context.Context, m *pubsub.Message) {
				send <- api.Entry{
					Labels: labels,
					Entry: logproto.Entry{
						Timestamp: time.Now(),
						Line:      "testing",
					},
				}
			})
		}
	}
}

// Type implements target.Target.
func (t *PubsubTarget) Type() target.TargetType {
	return target.PubsubTargetType
}

func (t *PubsubTarget) Ready() bool {
	// TODO(kavi): anyway to ping topic to see no connection issue?
	return true
}

func (t *PubsubTarget) DiscoveredLabels() model.LabelSet {
	// TODO(kavi): should be discoverable by labels?
	return nil
}

func (t *PubsubTarget) Labels() model.LabelSet {
	return t.config.Labels
}

func (t *PubsubTarget) Details() interface{} {
	return nil
}

func (t *PubsubTarget) Stop() error {
	t.handler.Stop()
	t.cancel()
	return nil
}
