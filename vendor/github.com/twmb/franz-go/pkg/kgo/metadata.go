package kgo

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/twmb/franz-go/pkg/kerr"
)

type metawait struct {
	mu         sync.Mutex
	c          *sync.Cond
	lastUpdate time.Time
}

func (m *metawait) init() { m.c = sync.NewCond(&m.mu) }
func (m *metawait) signal() {
	m.mu.Lock()
	m.lastUpdate = time.Now()
	m.mu.Unlock()
	m.c.Broadcast()
}

// ForceMetadataRefresh triggers the client to update the metadata that is
// currently used for producing & consuming.
//
// Internally, the client already properly triggers metadata updates whenever a
// partition is discovered to be out of date (leader moved, epoch is old, etc).
// However, when partitions are added to a topic through a CreatePartitions
// request, it may take up to MetadataMaxAge for the new partitions to be
// discovered. In this case, you may want to forcefully refresh metadata
// manually to discover these new partitions sooner.
func (cl *Client) ForceMetadataRefresh() {
	cl.triggerUpdateMetadataNow("from user ForceMetadataRefresh")
}

// PartitionLeader returns the given topic partition's leader, leader epoch and
// load error. This returns -1, -1, nil if the partition has not been loaded.
func (cl *Client) PartitionLeader(topic string, partition int32) (leader, leaderEpoch int32, err error) {
	if partition < 0 {
		return -1, -1, errors.New("invalid negative partition")
	}

	var t *topicPartitions

	m := cl.producer.topics.load()
	if len(m) > 0 {
		t = m[topic]
	}
	if t == nil {
		if cl.consumer.g != nil {
			if m = cl.consumer.g.tps.load(); len(m) > 0 {
				t = m[topic]
			}
		} else if cl.consumer.d != nil {
			if m = cl.consumer.d.tps.load(); len(m) > 0 {
				t = m[topic]
			}
		}
		if t == nil {
			return -1, -1, nil
		}
	}

	tv := t.load()
	if len(tv.partitions) <= int(partition) {
		return -1, -1, tv.loadErr
	}
	p := tv.partitions[partition]
	return p.leader, p.leaderEpoch, p.loadErr
}

// waitmeta returns immediately if metadata was updated within the last second,
// otherwise this waits for up to wait for a metadata update to complete.
func (cl *Client) waitmeta(ctx context.Context, wait time.Duration, why string) {
	now := time.Now()

	cl.metawait.mu.Lock()
	if now.Sub(cl.metawait.lastUpdate) < cl.cfg.metadataMinAge {
		cl.metawait.mu.Unlock()
		return
	}
	cl.metawait.mu.Unlock()

	cl.triggerUpdateMetadataNow(why)

	quit := false
	done := make(chan struct{})
	timeout := time.NewTimer(wait)
	defer timeout.Stop()

	go func() {
		defer close(done)
		cl.metawait.mu.Lock()
		defer cl.metawait.mu.Unlock()

		for !quit {
			if now.Sub(cl.metawait.lastUpdate) < cl.cfg.metadataMinAge {
				return
			}
			cl.metawait.c.Wait()
		}
	}()

	select {
	case <-done:
		return
	case <-timeout.C:
	case <-ctx.Done():
	case <-cl.ctx.Done():
	}

	cl.metawait.mu.Lock()
	quit = true
	cl.metawait.mu.Unlock()
	cl.metawait.c.Broadcast()
}

func (cl *Client) triggerUpdateMetadata(must bool, why string) bool {
	if !must {
		cl.metawait.mu.Lock()
		defer cl.metawait.mu.Unlock()
		if time.Since(cl.metawait.lastUpdate) < cl.cfg.metadataMinAge {
			return false
		}
	}

	select {
	case cl.updateMetadataCh <- why:
	default:
	}
	return true
}

func (cl *Client) triggerUpdateMetadataNow(why string) {
	select {
	case cl.updateMetadataNowCh <- why:
	default:
	}
}

func (cl *Client) blockingMetadataFn(fn func()) {
	var wg sync.WaitGroup
	wg.Add(1)
	waitfn := func() {
		defer wg.Done()
		fn()
	}
	select {
	case cl.blockingMetadataFnCh <- waitfn:
		wg.Wait()
	case <-cl.ctx.Done():
	}
}

// updateMetadataLoop updates metadata whenever the update ticker ticks,
// or whenever deliberately triggered.
func (cl *Client) updateMetadataLoop() {
	defer close(cl.metadone)
	var consecutiveErrors int
	var lastAt time.Time

	ticker := time.NewTicker(cl.cfg.metadataMaxAge)
	defer ticker.Stop()
loop:
	for {
		var now bool
		select {
		case <-cl.ctx.Done():
			return
		case <-ticker.C:
			// We do not log on the standard update case.
		case why := <-cl.updateMetadataCh:
			cl.cfg.logger.Log(LogLevelInfo, "metadata update triggered", "why", why)
		case why := <-cl.updateMetadataNowCh:
			cl.cfg.logger.Log(LogLevelInfo, "immediate metadata update triggered", "why", why)
			now = true
		case fn := <-cl.blockingMetadataFnCh:
			fn()
			continue loop
		}

		var nowTries int
	start:
		nowTries++
		if !now {
			if wait := cl.cfg.metadataMinAge - time.Since(lastAt); wait > 0 {
				timer := time.NewTimer(wait)
			prewait:
				select {
				case <-cl.ctx.Done():
					timer.Stop()
					return
				case why := <-cl.updateMetadataNowCh:
					timer.Stop()
					cl.cfg.logger.Log(LogLevelInfo, "immediate metadata update triggered, bypassing normal wait", "why", why)
				case <-timer.C:
				case fn := <-cl.blockingMetadataFnCh:
					fn()
					goto prewait
				}
			}
		}

		// Even with an "update now", we sleep just a bit to allow some
		// potential pile on now triggers.
		time.Sleep(time.Until(lastAt.Add(10 * time.Millisecond)))

		// Drain any refires that occurred during our waiting.
	out:
		for {
			select {
			case <-cl.updateMetadataCh:
			case <-cl.updateMetadataNowCh:
			case fn := <-cl.blockingMetadataFnCh:
				fn()
			default:
				break out
			}
		}

		retryWhy, err := cl.updateMetadata()
		if retryWhy != nil || err != nil {
			// If err is non-nil, the metadata request failed
			// itself and already retried 3x; we do not loop more.
			//
			// If err is nil, the a topic or partition had a load
			// error and is perhaps still being created. We retry a
			// few more times to give Kafka a chance to figure
			// things out. By default this will put us at 2s of
			// looping+waiting (250ms per wait, 8x), and if things
			// still fail we will fall into the slower update below
			// which waits (default) 5s between tries.
			if now && err == nil && nowTries < 8 {
				wait := 250 * time.Millisecond
				if cl.cfg.metadataMinAge < wait {
					wait = cl.cfg.metadataMinAge
				}
				cl.cfg.logger.Log(LogLevelDebug, "immediate metadata update had inner errors, re-updating",
					"errors", retryWhy.reason(""),
					"update_after", wait,
				)
				timer := time.NewTimer(wait)
			quickbackoff:
				select {
				case <-cl.ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				case fn := <-cl.blockingMetadataFnCh:
					fn()
					goto quickbackoff
				}
				goto start
			}
			if err != nil {
				cl.triggerUpdateMetadata(true, fmt.Sprintf("re-updating metadata due to err: %s", err))
			} else {
				cl.triggerUpdateMetadata(true, retryWhy.reason("re-updating due to inner errors"))
			}
		}
		if err == nil {
			cl.metawait.signal()
			cl.consumer.doOnMetadataUpdate()
			lastAt = time.Now()
			consecutiveErrors = 0
			continue
		}

		consecutiveErrors++
		after := time.NewTimer(cl.cfg.retryBackoff(consecutiveErrors))
	backoff:
		select {
		case <-cl.ctx.Done():
			after.Stop()
			return
		case <-after.C:
		case fn := <-cl.blockingMetadataFnCh:
			fn()
			goto backoff
		}
	}
}

var errMissingTopic = errors.New("topic_missing")

// Updates all producer and consumer partition data, returning whether a new
// update needs scheduling or if an error occurred.
//
// The producer and consumer use different topic maps and underlying
// topicPartitionsData pointers, but we update those underlying pointers
// equally.
func (cl *Client) updateMetadata() (retryWhy multiUpdateWhy, err error) {
	var (
		tpsProducerLoad = cl.producer.topics.load()
		tpsConsumer     *topicsPartitions
		groupExternal   *groupExternal
		all             = cl.cfg.regex
		reqTopics       []string
	)
	c := &cl.consumer
	switch {
	case c.g != nil:
		tpsConsumer = c.g.tps
		groupExternal = c.g.loadExternal()
	case c.d != nil:
		tpsConsumer = c.d.tps
	}

	if !all {
		reqTopicsSet := make(map[string]struct{})
		for _, m := range []map[string]*topicPartitions{
			tpsProducerLoad,
			tpsConsumer.load(),
		} {
			for topic := range m {
				reqTopicsSet[topic] = struct{}{}
			}
		}
		groupExternal.eachTopic(func(t string) {
			reqTopicsSet[t] = struct{}{}
		})
		reqTopics = make([]string, 0, len(reqTopicsSet))
		for topic := range reqTopicsSet {
			reqTopics = append(reqTopics, topic)
		}
	}

	latest, err := cl.fetchTopicMetadata(all, reqTopics)
	if err != nil {
		cl.bumpMetadataFailForTopics( // bump load failures for all topics
			tpsProducerLoad,
			err,
		)
		return nil, err
	}
	groupExternal.updateLatest(latest)

	// If we are consuming with regex and fetched all topics, the metadata
	// may have returned topics the consumer is not yet tracking. We ensure
	// that we will store the topics at the end of our metadata update.
	tpsConsumerLoad := tpsConsumer.load()
	if all {
		allTopics := make([]string, 0, len(latest))
		for topic := range latest {
			allTopics = append(allTopics, topic)
		}

		// We filter out topics will not match any of our regex's.
		// This ensures that the `tps` field does not contain topics
		// we will never use (the client works with misc. topics in
		// there, but it's better to avoid it -- and allows us to use
		// `tps` in GetConsumeTopics).
		allTopics = c.filterMetadataAllTopics(allTopics)

		tpsConsumerLoad = tpsConsumer.ensureTopics(allTopics)
		defer tpsConsumer.storeData(tpsConsumerLoad)

		// For regex consuming, if a topic is not returned in the
		// response and for at least missingTopicDelete from when we
		// first discovered it, we assume the topic has been deleted
		// and purge it. We allow for missingTopicDelete because (in
		// testing locally) Kafka can originally broadcast a newly
		// created topic exists and then fail to broadcast that info
		// again for a while.
		var purgeTopics []string
		for topic, tps := range tpsConsumerLoad {
			if _, ok := latest[topic]; !ok {
				if td := tps.load(); td.when != 0 && time.Since(time.Unix(td.when, 0)) > cl.cfg.missingTopicDelete {
					purgeTopics = append(purgeTopics, td.topic)
				} else {
					retryWhy.add(topic, -1, errMissingTopic)
				}
			}
		}
		if len(purgeTopics) > 0 {
			// We have to `go` because Purge issues a blocking
			// metadata fn; this will wait for our current
			// execution to finish then purge.
			cl.cfg.logger.Log(LogLevelInfo, "regex consumer purging topics that were previously consumed because they are missing in a metadata response, we are assuming they are deleted", "topics", purgeTopics)
			go cl.PurgeTopicsFromClient(purgeTopics...)
		}
	}

	css := &consumerSessionStopper{cl: cl}
	defer css.maybeRestart()

	var missingProduceTopics []*topicPartitions
	for _, m := range []struct {
		priors    map[string]*topicPartitions
		isProduce bool
	}{
		{tpsProducerLoad, true},
		{tpsConsumerLoad, false},
	} {
		for topic, priorParts := range m.priors {
			newParts, exists := latest[topic]
			if !exists {
				if m.isProduce {
					missingProduceTopics = append(missingProduceTopics, priorParts)
				}
				continue
			}
			cl.mergeTopicPartitions(
				topic,
				priorParts,
				newParts,
				m.isProduce,
				css,
				&retryWhy,
			)
		}
	}

	// For all produce topics that were missing, we want to bump their
	// retries that a failure happened. However, if we are regex consuming,
	// then it is possible in a rare scenario for the broker to not return
	// a topic that actually does exist and that we previously received a
	// metadata response for. This is handled above for consuming, we now
	// handle it the same way for consuming.
	if len(missingProduceTopics) > 0 {
		var bumpFail []string
		for _, tps := range missingProduceTopics {
			if all {
				if td := tps.load(); td.when != 0 && time.Since(time.Unix(td.when, 0)) > cl.cfg.missingTopicDelete {
					bumpFail = append(bumpFail, td.topic)
				} else {
					retryWhy.add(td.topic, -1, errMissingTopic)
				}
			} else {
				bumpFail = append(bumpFail, tps.load().topic)
			}
		}
		if len(bumpFail) > 0 {
			cl.bumpMetadataFailForTopics(
				tpsProducerLoad,
				fmt.Errorf("metadata request did not return topics: %v", bumpFail),
				bumpFail...,
			)
		}
	}

	return retryWhy, nil
}

// We use a special structure to repesent metadata before we *actually* convert
// it to topicPartitionsData. This helps avoid any pointer reuse problems
// because we want to keep the client's producer and consumer maps completely
// independent.  If we just returned map[string]*topicPartitionsData, we could
// end up in some really weird pointer reuse scenario that ultimately results
// in a bug.
//
// See #190 for more details, as well as the commit message introducing this.
type metadataTopic struct {
	loadErr    error
	isInternal bool
	topic      string
	partitions []metadataPartition
}

func (mt *metadataTopic) newPartitions(cl *Client, isProduce bool) *topicPartitionsData {
	n := len(mt.partitions)
	ps := &topicPartitionsData{
		loadErr:            mt.loadErr,
		isInternal:         mt.isInternal,
		partitions:         make([]*topicPartition, 0, n),
		writablePartitions: make([]*topicPartition, 0, n),
		topic:              mt.topic,
		when:               time.Now().Unix(),
	}
	for i := range mt.partitions {
		p := mt.partitions[i].newPartition(cl, isProduce)
		ps.partitions = append(ps.partitions, p)
		if p.loadErr == nil {
			ps.writablePartitions = append(ps.writablePartitions, p)
		}
	}
	return ps
}

type metadataPartition struct {
	topic       string
	topicID     [16]byte
	partition   int32
	loadErr     int16
	leader      int32
	leaderEpoch int32
	sns         sinkAndSource
}

func (mp metadataPartition) newPartition(cl *Client, isProduce bool) *topicPartition {
	td := topicPartitionData{
		leader:      mp.leader,
		leaderEpoch: mp.leaderEpoch,
	}
	p := &topicPartition{
		loadErr:            kerr.ErrorForCode(mp.loadErr),
		topicPartitionData: td,
	}
	if isProduce {
		p.records = &recBuf{
			cl:                  cl,
			topic:               mp.topic,
			partition:           mp.partition,
			maxRecordBatchBytes: cl.maxRecordBatchBytesForTopic(mp.topic),
			recBufsIdx:          -1,
			failing:             mp.loadErr != 0,
			sink:                mp.sns.sink,
			topicPartitionData:  td,
			lastAckedOffset:     -1,
		}
	} else {
		p.cursor = &cursor{
			topic:              mp.topic,
			topicID:            mp.topicID,
			partition:          mp.partition,
			keepControl:        cl.cfg.keepControl,
			cursorsIdx:         -1,
			source:             mp.sns.source,
			topicPartitionData: td,
			cursorOffset: cursorOffset{
				offset:            -1, // required to not consume until needed
				lastConsumedEpoch: -1, // required sentinel
			},
		}
	}
	return p
}

// fetchTopicMetadata fetches metadata for all reqTopics and returns new
// topicPartitionsData for each topic.
func (cl *Client) fetchTopicMetadata(all bool, reqTopics []string) (map[string]*metadataTopic, error) {
	_, meta, err := cl.fetchMetadataForTopics(cl.ctx, all, reqTopics)
	if err != nil {
		return nil, err
	}

	// Since we've fetched the metadata for some topics we can optimistically cache it
	// for mapped metadata too. This may reduce the number of Metadata requests issued
	// by the client.
	cl.storeCachedMappedMetadata(meta, nil)

	topics := make(map[string]*metadataTopic, len(meta.Topics))

	// Even if metadata returns a leader epoch, we do not use it unless we
	// can validate it per OffsetForLeaderEpoch. Some brokers may have an
	// odd set of support.
	useLeaderEpoch := cl.supportsOffsetForLeaderEpoch()

	for i := range meta.Topics {
		topicMeta := &meta.Topics[i]
		if topicMeta.Topic == nil {
			cl.cfg.logger.Log(LogLevelWarn, "metadata response contained nil topic name even though we did not request with topic IDs, skipping")
			continue
		}
		topic := *topicMeta.Topic

		mt := &metadataTopic{
			loadErr:    kerr.ErrorForCode(topicMeta.ErrorCode),
			isInternal: topicMeta.IsInternal,
			topic:      topic,
			partitions: make([]metadataPartition, 0, len(topicMeta.Partitions)),
		}

		topics[topic] = mt

		if mt.loadErr != nil {
			continue
		}

		// This 249 limit is in Kafka itself, we copy it here to rely on it while producing.
		if len(topic) > 249 {
			mt.loadErr = fmt.Errorf("invalid long topic name of (len %d) greater than max allowed 249", len(topic))
			continue
		}

		// Kafka partitions are strictly increasing from 0. We enforce
		// that here; if any partition is missing, we consider this
		// topic a load failure.
		sort.Slice(topicMeta.Partitions, func(i, j int) bool {
			return topicMeta.Partitions[i].Partition < topicMeta.Partitions[j].Partition
		})
		for i := range topicMeta.Partitions {
			if got := topicMeta.Partitions[i].Partition; got != int32(i) {
				mt.loadErr = fmt.Errorf("kafka did not reply with a comprensive set of partitions for a topic; we expected partition %d but saw %d", i, got)
				break
			}
		}

		if mt.loadErr != nil {
			continue
		}

		for i := range topicMeta.Partitions {
			partMeta := &topicMeta.Partitions[i]
			leaderEpoch := partMeta.LeaderEpoch
			if meta.Version < 7 || !useLeaderEpoch {
				leaderEpoch = -1
			}
			mp := metadataPartition{
				topic:       topic,
				topicID:     topicMeta.TopicID,
				partition:   partMeta.Partition,
				loadErr:     partMeta.ErrorCode,
				leader:      partMeta.Leader,
				leaderEpoch: leaderEpoch,
			}
			if mp.loadErr != 0 {
				mp.leader = unknownSeedID(0) // ensure every records & cursor can use a sink or source
			}
			cl.sinksAndSourcesMu.Lock()
			sns, exists := cl.sinksAndSources[mp.leader]
			if !exists {
				sns = sinkAndSource{
					sink:   cl.newSink(mp.leader),
					source: cl.newSource(mp.leader),
				}
				cl.sinksAndSources[mp.leader] = sns
			}
			for _, replica := range partMeta.Replicas {
				if replica < 0 {
					continue
				}
				if _, exists = cl.sinksAndSources[replica]; !exists {
					cl.sinksAndSources[replica] = sinkAndSource{
						sink:   cl.newSink(replica),
						source: cl.newSource(replica),
					}
				}
			}
			cl.sinksAndSourcesMu.Unlock()
			mp.sns = sns
			mt.partitions = append(mt.partitions, mp)
		}
	}

	return topics, nil
}

// mergeTopicPartitions merges a new topicPartition into an old and returns
// whether the metadata update that caused this merge needs to be retried.
//
// Retries are necessary if the topic or any partition has a retryable error.
func (cl *Client) mergeTopicPartitions(
	topic string,
	l *topicPartitions,
	mt *metadataTopic,
	isProduce bool,
	css *consumerSessionStopper,
	retryWhy *multiUpdateWhy,
) {
	lv := *l.load() // copy so our field writes do not collide with reads

	r := mt.newPartitions(cl, isProduce)

	// Producers must store the update through a special function that
	// manages unknown topic waiting, whereas consumers can just simply
	// store the update.
	if isProduce {
		hadPartitions := len(lv.partitions) != 0
		defer func() { cl.storePartitionsUpdate(topic, l, &lv, hadPartitions) }()
	} else {
		defer l.v.Store(&lv)
	}

	lv.loadErr = r.loadErr
	lv.isInternal = r.isInternal
	lv.topic = r.topic
	if lv.when == 0 {
		lv.when = r.when
	}

	// If the load had an error for the entire topic, we set the load error
	// but keep our stale partition information. For anything being
	// produced, we bump the respective error or fail everything. There is
	// nothing to be done in a consumer.
	if r.loadErr != nil {
		if isProduce {
			for _, topicPartition := range lv.partitions {
				topicPartition.records.bumpRepeatedLoadErr(lv.loadErr)
			}
		} else if !kerr.IsRetriable(r.loadErr) || cl.cfg.keepRetryableFetchErrors {
			cl.consumer.addFakeReadyForDraining(topic, -1, r.loadErr, "metadata refresh has a load error on this entire topic")
		}
		retryWhy.add(topic, -1, r.loadErr)
		return
	}

	// Before the atomic update, we keep the latest partitions / writable
	// partitions. All updates happen in r's slices, and we keep the
	// results and store them in lv.
	defer func() {
		lv.partitions = r.partitions
		lv.writablePartitions = r.writablePartitions
	}()

	// We should have no deleted partitions, but there are two cases where
	// we could.
	//
	// 1) an admin added partitions, we saw, then we re-fetched metadata
	// from an out of date broker that did not have the new partitions
	//
	// 2) a topic was deleted and recreated with fewer partitions
	//
	// Both of these scenarios should be rare to non-existent, and we do
	// nothing if we encounter them.

	// Migrating topicPartitions is a little tricky because we have to
	// worry about underlying pointers that may currently be loaded.
	for part, oldTP := range lv.partitions {
		exists := part < len(r.partitions)
		if !exists {
			// This is the "deleted" case; see the comment above.
			//
			// We need to keep the partition around. For producing,
			// the partition could be loaded and a record could be
			// added to it after we bump the load error. For
			// consuming, the partition is part of a group or part
			// of what was loaded for direct consuming.
			//
			// We only clear a partition if it is purged from the
			// client (which can happen automatically for consumers
			// if the user opted into ConsumeRecreatedTopics).
			dup := *oldTP
			newTP := &dup
			newTP.loadErr = errMissingMetadataPartition

			r.partitions = append(r.partitions, newTP)

			cl.cfg.logger.Log(LogLevelDebug, "metadata update is missing partition in topic, we are keeping the partition around for safety -- use PurgeTopicsFromClient if you wish to remove the topic",
				"topic", topic,
				"partition", part,
			)
			if isProduce {
				oldTP.records.bumpRepeatedLoadErr(errMissingMetadataPartition)
			}
			retryWhy.add(topic, int32(part), errMissingMetadataPartition)
			continue
		}
		newTP := r.partitions[part]

		// Like above for the entire topic, an individual partition
		// can have a load error. Unlike for the topic, individual
		// partition errors are always retryable.
		//
		// If the load errored, we keep all old information minus the
		// load error itself (the new load will have no information).
		if newTP.loadErr != nil {
			err := newTP.loadErr
			*newTP = *oldTP
			newTP.loadErr = err
			if isProduce {
				newTP.records.bumpRepeatedLoadErr(newTP.loadErr)
			} else if !kerr.IsRetriable(newTP.loadErr) || cl.cfg.keepRetryableFetchErrors {
				cl.consumer.addFakeReadyForDraining(topic, int32(part), newTP.loadErr, "metadata refresh has a load error on this partition")
			}
			retryWhy.add(topic, int32(part), newTP.loadErr)
			continue
		}

		// If the new partition has an older leader epoch, then we
		// fetched from an out of date broker. We just keep the old
		// information.
		if newTP.leaderEpoch < oldTP.leaderEpoch {
			// If we repeatedly rewind, then perhaps the cluster
			// entered some bad state and lost forward progress.
			// We will log & allow the rewind to allow the client
			// to continue; other requests may encounter fenced
			// epoch errors (and respectively recover).
			//
			// Five is a pretty low amount of retries, but since
			// we iterate through known brokers, this basically
			// means we keep stale metadata if five brokers all
			// agree things rewound.
			const maxEpochRewinds = 5
			if oldTP.epochRewinds < maxEpochRewinds {
				cl.cfg.logger.Log(LogLevelDebug, "metadata leader epoch went backwards, ignoring update",
					"topic", topic,
					"partition", part,
					"old_leader_epoch", oldTP.leaderEpoch,
					"new_leader_epoch", newTP.leaderEpoch,
					"current_num_rewinds", oldTP.epochRewinds+1,
				)
				*newTP = *oldTP
				newTP.epochRewinds++
				retryWhy.add(topic, int32(part), errEpochRewind)
				continue
			}

			cl.cfg.logger.Log(LogLevelInfo, "metadata leader epoch went backwards repeatedly, we are now keeping the metadata to allow forward progress",
				"topic", topic,
				"partition", part,
				"old_leader_epoch", oldTP.leaderEpoch,
				"new_leader_epoch", newTP.leaderEpoch,
			)
		}

		if !isProduce {
			var noID [16]byte
			if newTP.cursor.topicID == noID && oldTP.cursor.topicID != noID {
				cl.cfg.logger.Log(LogLevelWarn, "metadata update is missing the topic ID when we previously had one, ignoring update",
					"topic", topic,
					"partition", part,
				)
				retryWhy.add(topic, int32(part), errMissingTopicID)
				continue
			}
		}

		// If the tp data is the same, we simply copy over the records
		// and cursor pointers.
		//
		// If the tp data equals the old, then the sink / source is the
		// same, because the sink/source is from the tp leader.
		if newTP.topicPartitionData == oldTP.topicPartitionData {
			cl.cfg.logger.Log(LogLevelDebug, "metadata refresh has identical topic partition data",
				"topic", topic,
				"partition", part,
				"leader", newTP.leader,
				"leader_epoch", newTP.leaderEpoch,
			)
			if isProduce {
				newTP.records = oldTP.records
				newTP.records.clearFailing() // always clear failing state for producing after meta update
			} else {
				newTP.cursor = oldTP.cursor // unlike records, there is no failing state for a cursor
			}
		} else {
			cl.cfg.logger.Log(LogLevelDebug, "metadata refresh topic partition data changed",
				"topic", topic,
				"partition", part,
				"new_leader", newTP.leader,
				"new_leader_epoch", newTP.leaderEpoch,
				"old_leader", oldTP.leader,
				"old_leader_epoch", oldTP.leaderEpoch,
			)
			if isProduce {
				oldTP.migrateProductionTo(newTP) // migration clears failing state
			} else {
				oldTP.migrateCursorTo(newTP, css)
			}
		}
	}

	// For any partitions **not currently in use**, we need to add them to
	// the sink or source. If they are in use, they could be getting
	// managed or moved by the sink or source itself, so we should not
	// check the index field (which may be concurrently modified).
	if len(lv.partitions) > len(r.partitions) {
		return
	}
	newPartitions := r.partitions[len(lv.partitions):]

	// Anything left with a negative recBufsIdx / cursorsIdx is a new topic
	// partition and must be added to the sink / source.
	for _, newTP := range newPartitions {
		if isProduce && newTP.records.recBufsIdx == -1 {
			newTP.records.sink.addRecBuf(newTP.records)
		} else if !isProduce && newTP.cursor.cursorsIdx == -1 {
			newTP.cursor.source.addCursor(newTP.cursor)
		}
	}
}

var (
	errEpochRewind    = errors.New("epoch rewind")
	errMissingTopicID = errors.New("missing topic ID")
)

type multiUpdateWhy map[kerrOrString]map[string]map[int32]struct{}

type kerrOrString struct {
	k *kerr.Error
	s string
}

func (m *multiUpdateWhy) isOnly(err error) bool {
	if m == nil {
		return false
	}
	for e := range *m {
		if !errors.Is(err, e.k) {
			return false
		}
	}
	return true
}

func (m *multiUpdateWhy) add(t string, p int32, err error) {
	if err == nil {
		return
	}

	if *m == nil {
		*m = make(map[kerrOrString]map[string]map[int32]struct{})
	}
	var ks kerrOrString
	if ke := (*kerr.Error)(nil); errors.As(err, &ke) {
		ks = kerrOrString{k: ke}
	} else {
		ks = kerrOrString{s: err.Error()}
	}

	ts := (*m)[ks]
	if ts == nil {
		ts = make(map[string]map[int32]struct{})
		(*m)[ks] = ts
	}

	ps := ts[t]
	if ps == nil {
		ps = make(map[int32]struct{})
		ts[t] = ps
	}
	// -1 signals that the entire topic had an error.
	if p != -1 {
		ps[p] = struct{}{}
	}
}

// err{topic[1 2 3] topic2[4 5 6]} err2{...}
func (m multiUpdateWhy) reason(reason string) string {
	if len(m) == 0 {
		return ""
	}

	ksSorted := make([]kerrOrString, 0, len(m))
	for err := range m {
		ksSorted = append(ksSorted, err)
	}
	sort.Slice(ksSorted, func(i, j int) bool { // order by non-nil kerr's code, otherwise the string
		l, r := ksSorted[i], ksSorted[j]
		return l.k != nil && (r.k == nil || l.k.Code < r.k.Code) || r.k == nil && l.s < r.s
	})

	var errorStrings []string
	for _, ks := range ksSorted {
		ts := m[ks]
		tsSorted := make([]string, 0, len(ts))
		for t := range ts {
			tsSorted = append(tsSorted, t)
		}
		sort.Strings(tsSorted)

		var topicStrings []string
		for _, t := range tsSorted {
			ps := ts[t]
			if len(ps) == 0 {
				topicStrings = append(topicStrings, t)
			} else {
				psSorted := make([]int32, 0, len(ps))
				for p := range ps {
					psSorted = append(psSorted, p)
				}
				sort.Slice(psSorted, func(i, j int) bool { return psSorted[i] < psSorted[j] })
				topicStrings = append(topicStrings, fmt.Sprintf("%s%v", t, psSorted))
			}
		}

		if ks.k != nil {
			errorStrings = append(errorStrings, fmt.Sprintf("%s{%s}", ks.k.Message, strings.Join(topicStrings, " ")))
		} else {
			errorStrings = append(errorStrings, fmt.Sprintf("%s{%s}", ks.s, strings.Join(topicStrings, " ")))
		}
	}
	if reason == "" {
		return strings.Join(errorStrings, " ")
	}
	return reason + ": " + strings.Join(errorStrings, " ")
}
