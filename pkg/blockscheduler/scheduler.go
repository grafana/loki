package blockscheduler

import (
	"context"
	"errors"
	"flag"
	"strconv"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/twmb/franz-go/pkg/kadm"

	"github.com/grafana/dskit/services"
)

type Config struct {
	Topic string

	ConsumerGroup string
	Interval      time.Duration
}

func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.DurationVar(&cfg.Interval, prefix+"interval", time.Minute, "Interval to use for computing jobs.")
	f.StringVar(&cfg.ConsumerGroup, prefix+"consumer-group", "block-scheduler", "Consumer group used by block scheduler to track the last consumed offset.")
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("block-scheduler.", f)
}

func (cfg *Config) Validate() error {
	if cfg.Interval.Seconds() <= 0 {
		return errors.New("interval must be a non-zero value.")
	}

	return nil
}

type BlockScheduler struct {
	services.Service

	cfg     Config
	metrics *Metrics
	logger  log.Logger

	admClient *kadm.Client
}

func New(cfg Config, admClient *kadm.Client, logger log.Logger, r prometheus.Registerer) (*BlockScheduler, error) {
	s := &BlockScheduler{
		cfg:       cfg,
		admClient: admClient,
		logger:    logger,
		metrics:   NewMetrics(r),
	}

	s.Service = services.NewBasicService(nil, s.running, nil)
	return s, nil
}

func (s *BlockScheduler) running(ctx context.Context) error {
	ticker := time.NewTicker(s.cfg.Interval)

	for {
		select {
		case <-ticker.C:
			if err := s.PublishLagMetrics(ctx); err != nil {
				level.Error(s.logger).Log("msg", "failed to publish lag metrics", "err", err)
			}
		case <-ctx.Done():
			return nil
		}
	}
}

func (s *BlockScheduler) PublishLagMetrics(ctx context.Context) error {
	lag, err := GetGroupLag(ctx, s.admClient, s.cfg.Topic, s.cfg.ConsumerGroup, -1)
	if err != nil {
		level.Error(s.logger).Log("msg", "failed to get group lag", "err", err)
		return err
	}

	for _, partition := range lag {
		for partition, offsets := range partition {
			s.metrics.lag.WithLabelValues(strconv.Itoa(int(partition))).Set(float64(offsets.Lag))
			s.metrics.committedOffset.WithLabelValues(strconv.Itoa(int(partition))).Set(float64(offsets.Commit.At))
		}
	}

	return nil
}
