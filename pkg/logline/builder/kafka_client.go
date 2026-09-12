package builder

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl/plain"
)

// createKafkaClient builds the consumer-group Kafka client.
//
// Key design decisions:
//
//   - kgo.ConsumerGroup + kgo.ConsumeTopics: partitions are assigned by the
//     group coordinator instead of derived from the pod's StatefulSet ordinal.
//     Scaling the replica count is safe — partition ownership rebalances
//     automatically.
//
//   - kgo.InstanceID(cfg.InstanceID): static membership. A pod that restarts
//     within cfg.KafkaSessionTimeout rejoins with the same identity and keeps
//     its partitions, so rolling deploys and crash-restart loops do not cause
//     rebalances.
//
//   - kgo.Balancers(newCooperativeActiveStickyBalancer(...)): partition-ring-
//     aware balancer that balances ACTIVE partitions (per the partition ring)
//     evenly across members using cooperative-sticky semantics, while still
//     assigning inactive partitions round-robin so they are monitored and
//     can activate quickly. Cooperative-sticky on the active set means only
//     the partitions that must move are revoked; the rest stay put, avoiding
//     stop-the-world rebalances.
//
//   - kgo.DisableAutoCommit: offsets are committed by the flush goroutine
//     *after* the corresponding .lidx files are uploaded. This is what
//     enforces at-least-once. kgo's own auto-commit would commit positions
//     of records that have only been fetched, not yet uploaded.
//
//   - kgo.ConsumeResetOffset(AtStart): for partitions with no committed
//     offset yet, start at the earliest available record rather than the
//     end. Without this, a fresh consumer group would silently skip every
//     record produced before the first member joined.
func (s *Service) createKafkaClient() (*kgo.Client, error) {
	address := s.cfg.Kafka.ReaderConfig.Address
	if address == "" {
		address = s.cfg.Kafka.Address
	}

	seedBrokers := strings.Split(address, ",")
	for i := range seedBrokers {
		seedBrokers[i] = strings.TrimSpace(seedBrokers[i])
	}

	clientID := s.cfg.Kafka.ReaderConfig.ClientID
	if clientID == "" {
		clientID = s.cfg.Kafka.ClientID
	}
	if clientID == "" {
		clientID = "logline-index-builder"
	}

	opts := []kgo.Opt{
		kgo.WithLogger(newKgoLogger(log.With(s.logger, "component", "kgo"))),
		kgo.SeedBrokers(seedBrokers...),
		kgo.ClientID(clientID),
		kgo.DialTimeout(s.cfg.Kafka.DialTimeout),
		kgo.MetadataMinAge(10 * time.Second),
		kgo.MetadataMaxAge(10 * time.Second),
		kgo.FetchMinBytes(1 * 1024 * 1024),
		kgo.FetchMaxBytes(100 * 1024 * 1024),
		kgo.FetchMaxPartitionBytes(50 * 1024 * 1024),
		kgo.FetchMaxWait(1 * time.Second),

		// Consumer-group membership.
		kgo.ConsumerGroup(s.cfg.Kafka.ConsumerGroup),
		kgo.ConsumeTopics(s.cfg.Kafka.Topic),
		kgo.SessionTimeout(s.cfg.KafkaSessionTimeout),
		kgo.DisableAutoCommit(),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
		kgo.OnPartitionsAssigned(s.onPartitionsAssigned),
		kgo.OnPartitionsRevoked(s.onPartitionsLostOrRevoked),
		kgo.OnPartitionsLost(s.onPartitionsLostOrRevoked),
	}

	balancer := newCooperativeActiveStickyBalancer(s.partitionRing, log.With(s.logger, "component", "active-sticky-balancer"))
	opts = append(opts, kgo.Balancers(balancer))

	// Static membership is what keeps pod restarts from triggering rebalances.
	// kfake rejects InstanceID, so unit tests opt out via the unexported flag.
	if !s.cfg.disableStaticMembership {
		opts = append(opts, kgo.InstanceID(s.cfg.InstanceID))
	}

	if s.cfg.Kafka.SASLUsername != "" && s.cfg.Kafka.SASLPassword.String() != "" {
		level.Info(s.logger).Log("msg", "enabling SASL PLAIN authentication", "username", s.cfg.Kafka.SASLUsername)
		opts = append(opts, kgo.SASL(plain.Plain(func(_ context.Context) (plain.Auth, error) {
			return plain.Auth{
				User: s.cfg.Kafka.SASLUsername,
				Pass: s.cfg.Kafka.SASLPassword.String(),
			}, nil
		})))
	}

	client, err := kgo.NewClient(opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kafka client: %w", err)
	}

	level.Info(s.logger).Log(
		"msg", "Kafka consumer-group client initialized",
		"brokers", strings.Join(seedBrokers, ","),
		"topic", s.cfg.Kafka.Topic,
		"consumer_group", s.cfg.Kafka.ConsumerGroup,
		"instance_id", s.cfg.InstanceID,
		"session_timeout", s.cfg.KafkaSessionTimeout,
	)
	return client, nil
}

// kgoLogger adapts go-kit/log to franz-go's kgo.Logger interface so that
// internal kafka client errors (e.g. retryable fetch failures) are visible.
type kgoLogger struct {
	logger log.Logger
}

func newKgoLogger(logger log.Logger) *kgoLogger {
	return &kgoLogger{logger: logger}
}

func (l *kgoLogger) Level() kgo.LogLevel { return kgo.LogLevelWarn }

func (l *kgoLogger) Log(lvl kgo.LogLevel, msg string, keyvals ...any) {
	merged := make([]any, 0, 2+len(keyvals))
	merged = append(merged, "msg", msg)
	merged = append(merged, keyvals...)
	switch lvl {
	case kgo.LogLevelError:
		level.Error(l.logger).Log(merged...)
	case kgo.LogLevelWarn:
		level.Warn(l.logger).Log(merged...)
	case kgo.LogLevelInfo:
		level.Info(l.logger).Log(merged...)
	case kgo.LogLevelDebug:
		level.Debug(l.logger).Log(merged...)
	}
}
