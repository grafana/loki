package sarama

import (
	"context"
	"errors"
	"slices"
	"time"
)

// consumeCooperative keeps one session while its assignment changes
func (c *consumerGroup) consumeCooperative(ctx context.Context, topics []string, sess *consumerGroupSession) (err error) {
	defer func() {
		if releaseErr := sess.release(true); releaseErr != nil && err == nil {
			err = releaseErr
		}
		c.recordSessionCause(sess)
	}()

	for {
		select {
		case <-c.closed:
			return nil
		case <-ctx.Done():
			return nil
		case <-sess.ctx.Done():
			return nil
		case cause := <-sess.rejoin:
			c.lastSessionCause = cause
		}

		for {
			res, err := c.rejoinCooperative(ctx, topics, sess)
			if errors.Is(err, ErrClosedClient) {
				return ErrClosedConsumerGroup
			}
			if err != nil {
				return err
			}

			followUp, err := sess.reconcileAssignment(res)
			if err != nil {
				return err
			}
			if !followUp {
				break
			}
		}
	}
}

func (c *consumerGroup) rejoinCooperative(ctx context.Context, topics []string, sess *consumerGroupSession) (*rebalanceResult, error) {
	held := &heldAssignment{
		claims:       sess.Claims(),
		generationID: sess.GenerationID(),
	}

	sess.generationMu.Lock()
	defer sess.generationMu.Unlock()

	// This join covers any rebalance signaled before it. Drop a signal still
	// buffered, or the member would rejoin again for a rebalance it is
	// already part of.
	select {
	case <-sess.rejoin:
	default:
	}

	var res *rebalanceResult
	err := sess.offsets.transitionGeneration(func() (int32, string, error) {
		result, err := c.joinSync(ctx, topics, held, c.config.Consumer.Group.Rebalance.Retry.Max)
		if err != nil {
			return 0, "", err
		}

		res = result
		// joinSync rejoins under a new member id after UNKNOWN_MEMBER_ID
		// copy, as a pointer into res would keep its claim maps alive
		sess.memberID.Store(new(res.memberID))
		sess.generationID.Store(res.generationID)
		return res.generationID, res.memberID, nil
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

// reconcileAssignment reports whether released claims require a follow-up join
func (s *consumerGroupSession) reconcileAssignment(res *rebalanceResult) (bool, error) {
	owned := s.Claims()
	revoked := diffClaims(owned, res.claims)
	added := diffClaims(res.claims, owned)

	if len(revoked) > 0 {
		if err := s.revokeClaims(revoked); err != nil {
			return false, err
		}
		s.offsets.removePartitions(revoked)
	}

	for topic := range added {
		if err := s.parent.client.RefreshMetadata(topic); err != nil {
			return false, err
		}
	}
	if err := s.manageClaims(added); err != nil {
		return false, err
	}

	s.claims.Store(&res.claims)
	s.startClaims(added)
	s.watchPartitionNumbers(res)
	return len(revoked) > 0, nil
}

// revokeClaims waits up to the rebalance timeout for handlers to return
func (s *consumerGroupSession) revokeClaims(revoked map[string][]int32) error {
	var stopping []*runningClaim
	for topic, partitions := range revoked {
		for _, partition := range partitions {
			tp := topicPartitionAssignment{Topic: topic, Partition: partition}
			if claim := s.running[tp]; claim != nil {
				claim.cancel(errPartitionRevoked)
				stopping = append(stopping, claim)
				delete(s.running, tp)
			}
		}
	}

	timer := time.NewTimer(s.parent.config.Consumer.Group.Rebalance.Timeout)
	defer timer.Stop()

	for _, claim := range stopping {
		select {
		case <-claim.done:
		case <-timer.C:
			s.parent.handleError(ErrRebalanceTimedOut, "", -1)
			s.cancel(ErrRebalanceTimedOut)
			return ErrRebalanceTimedOut
		}
	}
	return nil
}

// diffClaims returns the claims in a that are absent from b.
func diffClaims(a, b map[string][]int32) map[string][]int32 {
	diff := make(map[string][]int32)
	for topic, partitions := range a {
		for _, partition := range partitions {
			if !slices.Contains(b[topic], partition) {
				diff[topic] = append(diff[topic], partition)
			}
		}
	}
	return diff
}

func (s *consumerGroupSession) triggerRebalance(cause error) {
	if s.parent.protocol == RebalanceProtocolEager {
		s.cancel(cause)
		return
	}

	select {
	case s.rejoin <- cause:
	default:
	}
}
