package kgo

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

func (g *groupConsumer) should848() bool {
	if wantBeta := g.cl.ctx.Value("opt_in_kafka_next_gen_balancer_beta"); wantBeta == nil { // !!! TODO REMOVE ONCE BROKER IMPROVES
		return false
	}
	if g.cl.cfg.disableNextGenBalancer {
		return false
	}
	// We pin to v1, introduced in Kafka 4, which fully stabilizes KIP-848.
	if !g.cl.supportsKIP848v1() {
		return false
	}
	switch g.cfg.balancers[0].(type) {
	case *stickyBalancer:
	case *rangeBalancer:
	default:
		return false
	}
	return true
}

func (g *groupConsumer) manage848() {
	var serverAssignor string
	switch g.cfg.balancers[0].(type) {
	case *stickyBalancer:
		serverAssignor = "uniform"
	case *rangeBalancer:
		serverAssignor = "range"
	}

	// fallbackToClassic is set when manage848 hands off to the classic
	// manage() goroutine, which takes ownership of closing manageDone.
	var fallbackToClassic bool
	defer func() {
		if !fallbackToClassic {
			close(g.manageDone)
		}
	}()
	var known848Support bool
	optInKnown := func() {
		if known848Support {
			return
		}
		known848Support = true
		g.cfg.logger.Log(LogLevelInfo, "beginning to manage the next-gen group lifecycle", "group", g.cfg.group)
		g.cooperative.Store(true) // next gen is always cooperative
	}

	g.mu.Lock()
	g848 := &g848{g: g, serverAssignor: serverAssignor}
	g.g848 = g848
	g.mu.Unlock()

	// v1+ requires the client to generate their own memberID.
	// On v0, the server provides the ID and we join twice.
	// We pin to v1+.
	g.memberGen.store(newStringUUID(), 0) // 0 joins the group

	var consecutiveErrors int
outer:
	for {
		initialHb, err := g848.initialJoin()

		// Even if Kafka replies that the API is available, if we use it
		// and the broker is not configured to support it, we receive
		// UnsupportedVersion. On the first loop
		if !known848Support {
			if err != nil {
				var ke *kerr.Error
				if errors.As(err, &ke) {
					if ke.Code == kerr.UnsupportedVersion.Code {
						// It's okay to update is848 here. This is used while leaving
						// and while heartbeating. We have not yet entered heartbeating,
						// and if the user is concurrently leaving, the lack of a memberID
						// means both 848 and old group mgmt leaves return early.
						g.mu.Lock()
						g.is848 = false
						g.mu.Unlock()
						g.cfg.logger.Log(LogLevelInfo, "falling back to standard consumer group management due to lack of broker support", "group", g.cfg.group)
						fallbackToClassic = true
						go g.manage()
						return
					}
					optInKnown() // A kerr that is NOT UnsupportVersion means this is supported.
				}
				// For non-kerr errors, we fall into normal logic below and retry.
			} else {
				optInKnown()
			}
		}

		for err == nil {
			consecutiveErrors = 0
			var nowAssigned map[string][]int32

			// In heartbeating, if we lose or gain partitions, we need to
			// exit the old heartbeat and re-enter setupAssignedAndHeartbeat.
			// Otherwise, we heartbeat exactly the same as the old.
			//
			// This results in a few more heartbeats than necessary
			// when things are changing, but keeps all the old
			// logic that handles all edge conditions.
			//
			// setupAssignedAndHeartbeat starts prerevoke (for
			// lost partitions) and heartbeating concurrently.
			// While prerevoking, the heartbeat sends keepalive
			// (Topics=nil) so the server does not see partitions
			// released before the user commits their offsets in
			// any OnPartitionsRevoked callback.
			// The prerevoke goroutine clears prerevoking when
			// revocation and offset commits are complete, and
			// subsequent heartbeats resume sending full requests.
			_, err = g.setupAssignedAndHeartbeat(initialHb, func() (time.Duration, error) {
				req := g848.mkreq()
				if g848.prerevoking.Load() || (reflect.DeepEqual(g848.lastSubscribedTopics, req.SubscribedTopicNames) && reflect.DeepEqual(g848.lastTopics, req.Topics)) {
					req.InstanceID = nil
					req.RackID = nil
					req.RebalanceTimeoutMillis = -1
					req.ServerAssignor = nil
					req.SubscribedTopicRegex = nil
					req.SubscribedTopicNames = nil
					req.Topics = nil
				}
				resp, err := req.RequestWith(g.ctx, g.cl)
				if err != nil {
					return g.cfg.heartbeatInterval, err
				}

				err = errCodeMessage(resp.ErrorCode, resp.ErrorMessage)
				sleep := time.Duration(resp.HeartbeatIntervalMillis) * time.Millisecond
				if err != nil {
					return sleep, err
				}
				hbAssigned := g848.handleResp(req, resp)
				if hbAssigned != nil {
					err = kerr.RebalanceInProgress
					nowAssigned = hbAssigned
				}
				return sleep, err
			})

			switch {
			case errors.Is(err, kerr.RebalanceInProgress):
				err = nil

			case err != nil && isRetryableBrokerErr(err):
				g.cfg.logger.Log(LogLevelInfo, "consumer group heartbeat hit retriable broker error, retrying",
					"group", g.cfg.group,
					"err", err,
				)
				err = nil

			case errors.Is(err, kerr.UnknownMemberID):
				g.memberGen.store(newStringUUID(), 0)

			case errors.Is(err, kerr.FencedMemberEpoch),
				errors.Is(err, kerr.GroupMaxSizeReached),
				errors.Is(err, kerr.UnsupportedAssignor):
				lvl := LogLevelInfo
				if errors.Is(err, kerr.GroupMaxSizeReached) {
					lvl = LogLevelWarn
				} else if errors.Is(err, kerr.UnsupportedAssignor) {
					lvl = LogLevelError
				}
				member, gen := g.memberGen.load()
				g.cfg.logger.Log(lvl, "consumer group heartbeat error, abandoning assignment and rejoining",
					"group", g.cfg.group,
					"member_id", member,
					"generation", gen,
					"err", err,
				)
				g.nowAssigned.store(nil)
				continue outer
			}

			if nowAssigned != nil {
				member, gen := g.memberGen.load()
				g.cfg.logger.Log(LogLevelInfo, "consumer group heartbeat detected an updated assignment; exited heartbeat loop to assign & reentering",
					"group", g.cfg.group,
					"member_id", member,
					"generation", gen,
					"now_assigned", nowAssigned,
				)
				g.nowAssigned.store(nowAssigned)
			}
		}

		// The errors we have to handle are:
		// * UnknownMemberID: abandon partitions, rejoin
		// * FencedMemberEpoch: abandon partitions, rejoin
		// * UnreleasedInstanceID: fatal error, do not rejoin
		// * General error: fatal error, do not rejoin
		//
		// In the latter two cases, we fall into rejoining anyway
		// because it is both non-problematic (we will keep failing
		// with the same error) and because it will cause the user
		// to repeatedly get error logs.
		//
		// Note that manageFailWait calls nowAssigned.store(nil),
		// meaning our initialJoin should *always* have an empty
		// Topics in the request.
		consecutiveErrors++
		ctxCanceled := g.manageFailWait(consecutiveErrors, err)
		if ctxCanceled {
			return
		}
	}
}

func (g *groupConsumer) leave848(ctx context.Context) {
	memberID := g.memberGen.memberID()
	g.cfg.logger.Log(LogLevelInfo, "leaving next-gen group",
		"group", g.cfg.group,
		"member_id", memberID,
		"instance_id", g.cfg.instanceID,
	)
	// If we error when leaving, there is not much
	// we can do. We may as well just return.
	req := kmsg.NewPtrConsumerGroupHeartbeatRequest()
	req.Group = g.cfg.group
	req.MemberID = memberID
	req.MemberEpoch = -1
	if g.cfg.instanceID != nil {
		req.MemberEpoch = -2
	}

	resp, err := req.RequestWith(ctx, g.cl)
	if err != nil {
		g.leaveErr = err
		return
	}
	g.leaveErr = errCodeMessage(resp.ErrorCode, resp.ErrorMessage)
}

type g848 struct {
	g *groupConsumer

	serverAssignor string

	lastSubscribedTopics []string
	lastTopics           []kmsg.ConsumerGroupHeartbeatRequestTopic

	// prerevoking is true while prerevoke is running: the
	// assignment has changed and lost partitions are being
	// revoked and their offsets committed. While true, the
	// heartbeat closure sends keepalive (Topics=nil) so the
	// server does not see partitions as released before offsets
	// are committed. Cleared by the prerevoke goroutine.
	prerevoking atomic.Bool
}

// v1+ requires the end user to generate their own MemberID, with the
// recommendation being v4 uuid base64 encoded so it can be put in URLs. We
// roughly do that (no version nor variant bits). crypto/rand does not fail
// and, in future Go versions, will panic on internal errors.
func newStringUUID() string {
	var uuid [16]byte
	io.ReadFull(rand.Reader, uuid[:]) // even more random than adding version & variant bits is having full randomness
	return base64.URLEncoding.EncodeToString(uuid[:])
}

func (g *g848) initialJoin() (time.Duration, error) {
	g.g.memberGen.storeGeneration(0)
	g.lastSubscribedTopics = nil
	g.lastTopics = nil
	g.prerevoking.Store(false)
	req := g.mkreq()
	resp, err := req.RequestWith(g.g.ctx, g.g.cl)
	if err == nil {
		err = errCodeMessage(resp.ErrorCode, resp.ErrorMessage)
	}
	if err != nil {
		return 0, err
	}
	nowAssigned := g.handleResp(req, resp)
	member, gen := g.g.memberGen.load()
	g.g.cfg.logger.Log(LogLevelInfo, "consumer group initial heartbeat received assignment",
		"group", g.g.cfg.group,
		"member_id", member,
		"generation", gen,
		"now_assigned", nowAssigned,
	)

	// We always keep nowAssigned: the field is always nil here, so we
	// either re-store nil or we save an update.
	if g.g.nowAssigned.read() != nil {
		panic("nowAssigned is not nil in our initial join, invalid invariant!")
	}
	g.g.nowAssigned.store(nowAssigned)
	return time.Duration(resp.HeartbeatIntervalMillis) * time.Millisecond, nil
}

func (g *g848) handleResp(req *kmsg.ConsumerGroupHeartbeatRequest, resp *kmsg.ConsumerGroupHeartbeatResponse) map[string][]int32 {
	if resp.MemberID != nil {
		g.g.memberGen.store(*resp.MemberID, resp.MemberEpoch)
		g.g.cl.cfg.logger.Log(LogLevelDebug, "storing member and epoch", "member", *resp.MemberID, "epoch", resp.MemberEpoch)
	} else {
		g.g.memberGen.storeGeneration(resp.MemberEpoch)
		g.g.cl.cfg.logger.Log(LogLevelDebug, "storing epoch", "epoch", resp.MemberEpoch)
	}

	id2t := g.g.cl.id2tMap()
	newAssigned := make(map[string][]int32)

	// Only update the last-sent fields when Topics was actually
	// included in the request. When the request was a keepalive
	// (Topics=nil), we preserve the previous values so the next
	// comparison still matches and produces another keepalive.
	// Without this guard, storing nil after a keepalive causes
	// DeepEqual(nil, []) to fail on the next heartbeat, re-sending
	// Topics=[] - creating an alternating full/keepalive pattern.
	// The server clears pendingRevocations when Topics does not
	// contain a pending partition, so the alternating stale full
	// heartbeats can cause premature revocation clearing and dual
	// assignment.
	if req.Topics != nil {
		g.lastSubscribedTopics = req.SubscribedTopicNames
		g.lastTopics = req.Topics
	}

	if resp.Assignment == nil {
		return nil
	}

	for _, t := range resp.Assignment.Topics {
		name := id2t[t.TopicID]
		if name == "" {
			// If we do not recognize the topic ID, we do not keep it for
			// assignment yet, but we immediately trigger a metadata update
			// to hopefully discover this topic by the time we are assigned
			// the topic again.
			g.g.cl.triggerUpdateMetadataNow(fmt.Sprintf("consumer group heartbeat returned topic ID %s that we do not recognize", topicID(t.TopicID)))
			continue
		}
		slices.Sort(t.Partitions)
		newAssigned[name] = t.Partitions
	}

	current := g.g.nowAssigned.read()
	if !mapi32sDeepEq(current, newAssigned) {
		return newAssigned
	}
	return nil
}

func (g *g848) mkreq() *kmsg.ConsumerGroupHeartbeatRequest {
	req := kmsg.NewPtrConsumerGroupHeartbeatRequest()
	req.Group = g.g.cfg.group
	req.MemberID, req.MemberEpoch = g.g.memberGen.load()

	// Most fields in the request can be null if the field is equal to the
	// last time we sent the request. The first time we write, we include
	// all information. We always return all information here; the caller
	// of mkreq may strip fields as needed.
	//
	// Our initial set of subscribed topics is specified in our config.
	// For non-regex consuming, topics are directly created into g.tps.
	// As well, g.tps is added to or purged from in AddConsumeTopics or
	// PurgeConsumeTopics. We can always use g.tps for direct topics the
	// user wants to consume.
	//
	// For regex topics, they cannot add or remove after client creation.
	// We just use the initial config field.

	req.InstanceID = g.g.cfg.instanceID
	if g.g.cfg.rack != "" {
		req.RackID = &g.g.cfg.rack
	}
	req.RebalanceTimeoutMillis = int32(g.g.cfg.rebalanceTimeout.Milliseconds())
	req.ServerAssignor = &g.serverAssignor

	tps := g.g.tps.load()
	if g.g.cl.cfg.regex && len(g.g.cl.cfg.excludeTopics) > 0 {
		// KIP-848's SubscribedTopicRegex is include-only with no exclude
		// counterpart. When excludes are configured, fall back to sending
		// the already-resolved topic names from g.tps (which
		// filterMetadataAllTopics populates with excludes applied).
		// New topics are picked up on the next metadata refresh.
		subscribedTopics := make([]string, 0, len(tps))
		for t := range tps {
			subscribedTopics = append(subscribedTopics, t)
		}
		req.SubscribedTopicNames = subscribedTopics
	} else if g.g.cl.cfg.regex {
		topics := g.g.cl.cfg.topics
		patterns := make([]string, 0, len(topics))
		for topic := range topics {
			patterns = append(patterns, "(?:"+topic+")")
		}
		pattern := strings.Join(patterns, "|")
		req.SubscribedTopicRegex = &pattern
	} else {
		// SubscribedTopics must always exist when epoch == 0.
		// We specifically 'make' the slice to ensure it is non-nil.
		subscribedTopics := make([]string, 0, len(tps))
		for t := range tps {
			subscribedTopics = append(subscribedTopics, t)
		}
		req.SubscribedTopicNames = subscribedTopics
	}
	// Build Topics from our current assignment. The heartbeat closure
	// uses prerevoking to strip this to nil during prerevoke,
	// preventing the server from seeing released partitions before
	// offsets are committed.
	nowAssigned := g.g.nowAssigned.read()
	req.Topics = []kmsg.ConsumerGroupHeartbeatRequestTopic{} // ALWAYS initialize: len 0 is significantly different than nil (nil means same as last time)
	for t, ps := range nowAssigned {
		rt := kmsg.NewConsumerGroupHeartbeatRequestTopic()
		rt.Partitions = slices.Clone(ps)
		rt.TopicID = tps[t].load().id
		req.Topics = append(req.Topics, rt)
	}

	return req
}
