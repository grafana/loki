package kafka

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/IBM/sarama"
	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/clients/pkg/promtail/targets/target"
)

type DiscovererFn func(sarama.ConsumerGroupSession, sarama.ConsumerGroupClaim) (RunnableTarget, error)

func (d DiscovererFn) NewTarget(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) (RunnableTarget, error) {
	return d(session, claim)
}

type fakeTarget struct {
	ctx context.Context
	lbs model.LabelSet
}

func (f *fakeTarget) run()                             { <-f.ctx.Done() }
func (f *fakeTarget) Type() target.TargetType          { return "" }
func (f *fakeTarget) DiscoveredLabels() model.LabelSet { return nil }
func (f *fakeTarget) Labels() model.LabelSet           { return f.lbs }
func (f *fakeTarget) Ready() bool                      { return true }
func (f *fakeTarget) Details() interface{}             { return nil }

func Test_ComsumerConsume(t *testing.T) {
	var (
		group       = &testConsumerGroupHandler{mu: &sync.Mutex{}}
		session     = &testSession{}
		ctx, cancel = context.WithCancel(context.Background())
		c           = &consumer{
			logger:        log.NewNopLogger(),
			ctx:           context.Background(),
			cancel:        func() {},
			ConsumerGroup: group,
			discoverer: DiscovererFn(func(_ sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) (RunnableTarget, error) {
				if claim.Topic() != "dropped" {
					return &fakeTarget{
						ctx: ctx,
						lbs: model.LabelSet{"topic": model.LabelValue(claim.Topic())},
					}, nil
				}
				return &fakeTarget{
					ctx: ctx,
				}, nil
			}),
		}
	)

	c.start(ctx, []string{"foo"})
	require.Eventually(t, group.consuming.Load, 5*time.Second, 100*time.Microsecond)
	require.NoError(t, group.handler.Setup(session))
	go func() {
		err := group.handler.ConsumeClaim(session, newTestClaim("foo", 1, 2))
		require.NoError(t, err)
	}()
	go func() {
		err := group.handler.ConsumeClaim(session, newTestClaim("bar", 1, 2))
		require.NoError(t, err)
	}()
	go func() {
		err := group.handler.ConsumeClaim(session, newTestClaim("dropped", 1, 2))
		require.NoError(t, err)
	}()
	require.Eventually(t, func() bool {
		return len(c.getActiveTargets()) == 2
	}, 2*time.Second, 100*time.Millisecond)
	require.Eventually(t, func() bool {
		return len(c.getDroppedTargets()) == 1
	}, 2*time.Second, 100*time.Millisecond)
	err := group.handler.Cleanup(session)
	require.NoError(t, err)
	cancel()
	c.stop()
}

func Test_ComsumerRetry(_ *testing.T) {
	var (
		group = &testConsumerGroupHandler{
			mu:        &sync.Mutex{},
			returnErr: errors.New("foo"),
		}
		ctx, cancel = context.WithCancel(context.Background())
		c           = &consumer{
			logger:        log.NewNopLogger(),
			ctx:           context.Background(),
			cancel:        func() {},
			ConsumerGroup: group,
		}
	)
	defer cancel()
	c.start(ctx, []string{"foo"})
	<-time.After(2 * time.Second)
	c.stop()
}
