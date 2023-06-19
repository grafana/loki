// Copyright 2019-2022 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package server

import (
	"fmt"
	"math/rand"
	"sort"
	"sync"
	"time"
)

// TODO(dlc) - This is a fairly simplistic approach but should do for now.
type memStore struct {
	mu          sync.RWMutex
	cfg         StreamConfig
	state       StreamState
	msgs        map[uint64]*StoreMsg
	fss         map[string]*SimpleState
	maxp        int64
	scb         StorageUpdateHandler
	ageChk      *time.Timer
	consumers   int
	receivedAny bool
}

func newMemStore(cfg *StreamConfig) (*memStore, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config required")
	}
	if cfg.Storage != MemoryStorage {
		return nil, fmt.Errorf("memStore requires memory storage type in config")
	}
	ms := &memStore{
		msgs: make(map[uint64]*StoreMsg),
		fss:  make(map[string]*SimpleState),
		maxp: cfg.MaxMsgsPer,
		cfg:  *cfg,
	}

	return ms, nil
}

func (ms *memStore) UpdateConfig(cfg *StreamConfig) error {
	if cfg == nil {
		return fmt.Errorf("config required")
	}
	if cfg.Storage != MemoryStorage {
		return fmt.Errorf("memStore requires memory storage type in config")
	}

	ms.mu.Lock()
	ms.cfg = *cfg
	// Limits checks and enforcement.
	ms.enforceMsgLimit()
	ms.enforceBytesLimit()
	// Do age timers.
	if ms.ageChk == nil && ms.cfg.MaxAge != 0 {
		ms.startAgeChk()
	}
	if ms.ageChk != nil && ms.cfg.MaxAge == 0 {
		ms.ageChk.Stop()
		ms.ageChk = nil
	}
	// Make sure to update MaxMsgsPer
	maxp := ms.maxp
	ms.maxp = cfg.MaxMsgsPer
	// If the value is smaller we need to enforce that.
	if ms.maxp != 0 && ms.maxp < maxp {
		lm := uint64(ms.maxp)
		for _, ss := range ms.fss {
			if ss.Msgs > lm {
				ms.enforcePerSubjectLimit(ss)
			}
		}
	}
	ms.mu.Unlock()

	if cfg.MaxAge != 0 {
		ms.expireMsgs()
	}
	return nil
}

// Stores a raw message with expected sequence number and timestamp.
// Lock should be held.
func (ms *memStore) storeRawMsg(subj string, hdr, msg []byte, seq uint64, ts int64) error {
	if ms.msgs == nil {
		return ErrStoreClosed
	}

	// Tracking by subject.
	var ss *SimpleState
	var asl bool
	if len(subj) > 0 {
		if ss = ms.fss[subj]; ss != nil {
			asl = ms.maxp > 0 && ss.Msgs >= uint64(ms.maxp)
		}
	}

	// Check if we are discarding new messages when we reach the limit.
	if ms.cfg.Discard == DiscardNew {
		if asl && ms.cfg.DiscardNewPer {
			return ErrMaxMsgsPerSubject
		}
		if ms.cfg.MaxMsgs > 0 && ms.state.Msgs >= uint64(ms.cfg.MaxMsgs) {
			// If we are tracking max messages per subject and are at the limit we will replace, so this is ok.
			if !asl {
				return ErrMaxMsgs
			}
		}
		if ms.cfg.MaxBytes > 0 && ms.state.Bytes+uint64(len(msg)+len(hdr)) >= uint64(ms.cfg.MaxBytes) {
			if !asl {
				return ErrMaxBytes
			}
			// If we are here we are at a subject maximum, need to determine if dropping last message gives us enough room.
			sm, ok := ms.msgs[ss.First]
			if !ok || memStoreMsgSize(sm.subj, sm.hdr, sm.msg) < uint64(len(msg)+len(hdr)) {
				return ErrMaxBytes
			}
		}
	}

	if seq != ms.state.LastSeq+1 {
		if seq > 0 {
			return ErrSequenceMismatch
		}
		seq = ms.state.LastSeq + 1
	}

	// Adjust first if needed.
	now := time.Unix(0, ts).UTC()
	if ms.state.Msgs == 0 {
		ms.state.FirstSeq = seq
		ms.state.FirstTime = now
	}

	// Make copies
	// TODO(dlc) - Maybe be smarter here.
	if len(msg) > 0 {
		msg = copyBytes(msg)
	}
	if len(hdr) > 0 {
		hdr = copyBytes(hdr)
	}

	// FIXME(dlc) - Could pool at this level?
	sm := &StoreMsg{subj, nil, nil, make([]byte, 0, len(hdr)+len(msg)), seq, ts}
	sm.buf = append(sm.buf, hdr...)
	sm.buf = append(sm.buf, msg...)
	if len(hdr) > 0 {
		sm.hdr = sm.buf[:len(hdr)]
	}
	sm.msg = sm.buf[len(hdr):]
	ms.msgs[seq] = sm
	ms.state.Msgs++
	ms.state.Bytes += memStoreMsgSize(subj, hdr, msg)
	ms.state.LastSeq = seq
	ms.state.LastTime = now

	// Track per subject.
	if len(subj) > 0 {
		if ss != nil {
			ss.Msgs++
			ss.Last = seq
			// Check per subject limits.
			if ms.maxp > 0 && ss.Msgs > uint64(ms.maxp) {
				ms.enforcePerSubjectLimit(ss)
			}
		} else {
			ms.fss[subj] = &SimpleState{Msgs: 1, First: seq, Last: seq}
		}
	}

	// Limits checks and enforcement.
	ms.enforceMsgLimit()
	ms.enforceBytesLimit()

	// Check if we have and need the age expiration timer running.
	if ms.ageChk == nil && ms.cfg.MaxAge != 0 {
		ms.startAgeChk()
	}
	return nil
}

// StoreRawMsg stores a raw message with expected sequence number and timestamp.
func (ms *memStore) StoreRawMsg(subj string, hdr, msg []byte, seq uint64, ts int64) error {
	ms.mu.Lock()
	err := ms.storeRawMsg(subj, hdr, msg, seq, ts)
	cb := ms.scb
	// Check if first message timestamp requires expiry
	// sooner than initial replica expiry timer set to MaxAge when initializing.
	if !ms.receivedAny && ms.cfg.MaxAge != 0 && ts > 0 {
		ms.receivedAny = true
		// Calculate duration when the next expireMsgs should be called.
		ms.resetAgeChk(int64(time.Millisecond) * 50)
	}
	ms.mu.Unlock()

	if err == nil && cb != nil {
		cb(1, int64(memStoreMsgSize(subj, hdr, msg)), seq, subj)
	}

	return err
}

// Store stores a message.
func (ms *memStore) StoreMsg(subj string, hdr, msg []byte) (uint64, int64, error) {
	ms.mu.Lock()
	seq, ts := ms.state.LastSeq+1, time.Now().UnixNano()
	err := ms.storeRawMsg(subj, hdr, msg, seq, ts)
	cb := ms.scb
	ms.mu.Unlock()

	if err != nil {
		seq, ts = 0, 0
	} else if cb != nil {
		cb(1, int64(memStoreMsgSize(subj, hdr, msg)), seq, subj)
	}

	return seq, ts, err
}

// SkipMsg will use the next sequence number but not store anything.
func (ms *memStore) SkipMsg() uint64 {
	// Grab time.
	now := time.Now().UTC()

	ms.mu.Lock()
	seq := ms.state.LastSeq + 1
	ms.state.LastSeq = seq
	ms.state.LastTime = now
	if ms.state.Msgs == 0 {
		ms.state.FirstSeq = seq
		ms.state.FirstTime = now
	}
	ms.updateFirstSeq(seq)
	ms.mu.Unlock()
	return seq
}

// RegisterStorageUpdates registers a callback for updates to storage changes.
// It will present number of messages and bytes as a signed integer and an
// optional sequence number of the message if a single.
func (ms *memStore) RegisterStorageUpdates(cb StorageUpdateHandler) {
	ms.mu.Lock()
	ms.scb = cb
	ms.mu.Unlock()
}

// GetSeqFromTime looks for the first sequence number that has the message
// with >= timestamp.
// FIXME(dlc) - inefficient.
func (ms *memStore) GetSeqFromTime(t time.Time) uint64 {
	ts := t.UnixNano()
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	if len(ms.msgs) == 0 {
		return ms.state.LastSeq + 1
	}
	if ts <= ms.msgs[ms.state.FirstSeq].ts {
		return ms.state.FirstSeq
	}
	last := ms.msgs[ms.state.LastSeq].ts
	if ts == last {
		return ms.state.LastSeq
	}
	if ts > last {
		return ms.state.LastSeq + 1
	}
	index := sort.Search(len(ms.msgs), func(i int) bool {
		return ms.msgs[uint64(i)+ms.state.FirstSeq].ts >= ts
	})
	return uint64(index) + ms.state.FirstSeq
}

// FilteredState will return the SimpleState associated with the filtered subject and a proposed starting sequence.
func (ms *memStore) FilteredState(sseq uint64, subj string) SimpleState {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	return ms.filteredStateLocked(sseq, subj, false)
}

func (ms *memStore) filteredStateLocked(sseq uint64, filter string, lastPerSubject bool) SimpleState {
	var ss SimpleState

	if sseq < ms.state.FirstSeq {
		sseq = ms.state.FirstSeq
	}

	// If past the end no results.
	if sseq > ms.state.LastSeq {
		return ss
	}

	isAll := filter == _EMPTY_ || filter == fwcs

	// First check if we can optimize this part.
	// This means we want all and the starting sequence was before this block.
	if isAll && sseq <= ms.state.FirstSeq {
		total := ms.state.Msgs
		if lastPerSubject {
			total = uint64(len(ms.fss))
		}
		return SimpleState{
			Msgs:  total,
			First: ms.state.FirstSeq,
			Last:  ms.state.LastSeq,
		}
	}

	tsa := [32]string{}
	fsa := [32]string{}
	fts := tokenizeSubjectIntoSlice(fsa[:0], filter)
	wc := subjectHasWildcard(filter)

	// 1. See if we match any subs from fss.
	// 2. If we match and the sseq is past ss.Last then we can use meta only.
	// 3. If we match and we need to do a partial, break and clear any totals and do a full scan like num pending.

	isMatch := func(subj string) bool {
		if isAll {
			return true
		}
		if !wc {
			return subj == filter
		}
		tts := tokenizeSubjectIntoSlice(tsa[:0], subj)
		return isSubsetMatchTokenized(tts, fts)
	}

	update := func(fss *SimpleState) {
		msgs, first, last := fss.Msgs, fss.First, fss.Last
		if lastPerSubject {
			msgs, first = 1, last
		}
		ss.Msgs += msgs
		if ss.First == 0 || first < ss.First {
			ss.First = first
		}
		if last > ss.Last {
			ss.Last = last
		}
	}

	var havePartial bool
	// We will track start and end sequences as we go.
	for subj, fss := range ms.fss {
		if isMatch(subj) {
			if sseq <= fss.First {
				update(fss)
			} else if sseq <= fss.Last {
				// We matched but its a partial.
				havePartial = true
				// Don't break here, we will update to keep tracking last.
				update(fss)
			}
		}
	}

	// If we did not encounter any partials we can return here.
	if !havePartial {
		return ss
	}

	// If we are here we need to scan the msgs.
	// Capture first and last sequences for scan and then clear what we had.
	first, last := ss.First, ss.Last
	// To track if we decide to exclude and we need to calculate first.
	var needScanFirst bool
	if first < sseq {
		first = sseq
		needScanFirst = true
	}

	// Now we want to check if it is better to scan inclusive and recalculate that way
	// or leave and scan exclusive and adjust our totals.
	// ss.Last is always correct here.
	toScan, toExclude := last-first, first-ms.state.FirstSeq+ms.state.LastSeq-ss.Last
	var seen map[string]bool
	if lastPerSubject {
		seen = make(map[string]bool)
	}
	if toScan < toExclude {
		ss.Msgs, ss.First = 0, 0
		for seq := first; seq <= last; seq++ {
			if sm, ok := ms.msgs[seq]; ok && !seen[sm.subj] && isMatch(sm.subj) {
				ss.Msgs++
				if ss.First == 0 {
					ss.First = seq
				}
				if seen != nil {
					seen[sm.subj] = true
				}
			}
		}
	} else {
		// We will adjust from the totals above by scanning what we need to exclude.
		ss.First = first
		var adjust uint64
		for seq := ms.state.FirstSeq; seq < first; seq++ {
			if sm, ok := ms.msgs[seq]; ok && !seen[sm.subj] && isMatch(sm.subj) {
				adjust++
				if seen != nil {
					seen[sm.subj] = true
				}
			}
		}
		// Now do range at end.
		for seq := last + 1; seq < ms.state.LastSeq; seq++ {
			if sm, ok := ms.msgs[seq]; ok && !seen[sm.subj] && isMatch(sm.subj) {
				adjust++
				if seen != nil {
					seen[sm.subj] = true
				}
			}
		}
		ss.Msgs -= adjust
		if needScanFirst {
			for seq := first; seq < last; seq++ {
				if sm, ok := ms.msgs[seq]; ok && isMatch(sm.subj) {
					ss.First = seq
					break
				}
			}
		}
	}

	return ss
}

// SubjectsState returns a map of SimpleState for all matching subjects.
func (ms *memStore) SubjectsState(subject string) map[string]SimpleState {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	if len(ms.fss) == 0 {
		return nil
	}

	fss := make(map[string]SimpleState)
	for subj, ss := range ms.fss {
		if subject == _EMPTY_ || subject == fwcs || subjectIsSubsetMatch(subj, subject) {
			oss := fss[subj]
			if oss.First == 0 { // New
				fss[subj] = *ss
			} else {
				// Merge here.
				oss.Last, oss.Msgs = ss.Last, oss.Msgs+ss.Msgs
				fss[subj] = oss
			}
		}
	}
	return fss
}

// SubjectsTotal return message totals per subject.
func (ms *memStore) SubjectsTotals(filterSubject string) map[string]uint64 {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	if len(ms.fss) == 0 {
		return nil
	}

	tsa := [32]string{}
	fsa := [32]string{}
	fts := tokenizeSubjectIntoSlice(fsa[:0], filterSubject)
	isAll := filterSubject == _EMPTY_ || filterSubject == fwcs

	fst := make(map[string]uint64)
	for subj, ss := range ms.fss {
		if isAll {
			fst[subj] = ss.Msgs
		} else {
			if tts := tokenizeSubjectIntoSlice(tsa[:0], subj); isSubsetMatchTokenized(tts, fts) {
				fst[subj] = ss.Msgs
			}
		}
	}
	return fst
}

// NumPending will return the number of pending messages matching the filter subject starting at sequence.
func (ms *memStore) NumPending(sseq uint64, filter string, lastPerSubject bool) (total, validThrough uint64) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	ss := ms.filteredStateLocked(sseq, filter, lastPerSubject)
	return ss.Msgs, ms.state.LastSeq
}

// Will check the msg limit for this tracked subject.
// Lock should be held.
func (ms *memStore) enforcePerSubjectLimit(ss *SimpleState) {
	if ms.maxp <= 0 {
		return
	}
	for nmsgs := ss.Msgs; nmsgs > uint64(ms.maxp); nmsgs = ss.Msgs {
		if !ms.removeMsg(ss.First, false) {
			break
		}
	}
}

// Will check the msg limit and drop firstSeq msg if needed.
// Lock should be held.
func (ms *memStore) enforceMsgLimit() {
	if ms.cfg.MaxMsgs <= 0 || ms.state.Msgs <= uint64(ms.cfg.MaxMsgs) {
		return
	}
	for nmsgs := ms.state.Msgs; nmsgs > uint64(ms.cfg.MaxMsgs); nmsgs = ms.state.Msgs {
		ms.deleteFirstMsgOrPanic()
	}
}

// Will check the bytes limit and drop msgs if needed.
// Lock should be held.
func (ms *memStore) enforceBytesLimit() {
	if ms.cfg.MaxBytes <= 0 || ms.state.Bytes <= uint64(ms.cfg.MaxBytes) {
		return
	}
	for bs := ms.state.Bytes; bs > uint64(ms.cfg.MaxBytes); bs = ms.state.Bytes {
		ms.deleteFirstMsgOrPanic()
	}
}

// Will start the age check timer.
// Lock should be held.
func (ms *memStore) startAgeChk() {
	if ms.ageChk == nil && ms.cfg.MaxAge != 0 {
		ms.ageChk = time.AfterFunc(ms.cfg.MaxAge, ms.expireMsgs)
	}
}

// Lock should be held.
func (ms *memStore) resetAgeChk(delta int64) {
	if ms.cfg.MaxAge == 0 {
		return
	}

	fireIn := ms.cfg.MaxAge
	if delta > 0 && time.Duration(delta) < fireIn {
		fireIn = time.Duration(delta)
	}
	if ms.ageChk != nil {
		ms.ageChk.Reset(fireIn)
	} else {
		ms.ageChk = time.AfterFunc(fireIn, ms.expireMsgs)
	}
}

// Will expire msgs that are too old.
func (ms *memStore) expireMsgs() {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	now := time.Now().UnixNano()
	minAge := now - int64(ms.cfg.MaxAge)
	for {
		if sm, ok := ms.msgs[ms.state.FirstSeq]; ok && sm.ts <= minAge {
			ms.deleteFirstMsgOrPanic()
			// Recalculate in case we are expiring a bunch.
			now = time.Now().UnixNano()
			minAge = now - int64(ms.cfg.MaxAge)

		} else {
			if len(ms.msgs) == 0 {
				if ms.ageChk != nil {
					ms.ageChk.Stop()
					ms.ageChk = nil
				}
			} else {
				var fireIn time.Duration
				if sm == nil {
					fireIn = ms.cfg.MaxAge
				} else {
					fireIn = time.Duration(sm.ts - minAge)
				}
				if ms.ageChk != nil {
					ms.ageChk.Reset(fireIn)
				} else {
					ms.ageChk = time.AfterFunc(fireIn, ms.expireMsgs)
				}
			}
			return
		}
	}
}

// PurgeEx will remove messages based on subject filters, sequence and number of messages to keep.
// Will return the number of purged messages.
func (ms *memStore) PurgeEx(subject string, sequence, keep uint64) (purged uint64, err error) {
	if sequence > 1 && keep > 0 {
		return 0, ErrPurgeArgMismatch
	}

	if subject == _EMPTY_ || subject == fwcs {
		if keep == 0 && (sequence == 0 || sequence == 1) {
			return ms.Purge()
		}
		if sequence > 1 {
			return ms.Compact(sequence)
		} else if keep > 0 {
			ms.mu.RLock()
			msgs, lseq := ms.state.Msgs, ms.state.LastSeq
			ms.mu.RUnlock()
			if keep >= msgs {
				return 0, nil
			}
			return ms.Compact(lseq - keep + 1)
		}
		return 0, nil

	}
	eq := compareFn(subject)
	if ss := ms.FilteredState(1, subject); ss.Msgs > 0 {
		if keep > 0 {
			if keep >= ss.Msgs {
				return 0, nil
			}
			ss.Msgs -= keep
		}
		last := ss.Last
		if sequence > 1 {
			last = sequence - 1
		}
		ms.mu.Lock()
		for seq := ss.First; seq <= last; seq++ {
			if sm, ok := ms.msgs[seq]; ok && eq(sm.subj, subject) {
				if ok := ms.removeMsg(sm.seq, false); ok {
					purged++
					if purged >= ss.Msgs {
						break
					}
				}
			}
		}
		ms.mu.Unlock()
	}
	return purged, nil
}

// Purge will remove all messages from this store.
// Will return the number of purged messages.
func (ms *memStore) Purge() (uint64, error) {
	ms.mu.Lock()
	purged := uint64(len(ms.msgs))
	cb := ms.scb
	bytes := int64(ms.state.Bytes)
	ms.state.FirstSeq = ms.state.LastSeq + 1
	ms.state.FirstTime = time.Time{}
	ms.state.Bytes = 0
	ms.state.Msgs = 0
	ms.msgs = make(map[uint64]*StoreMsg)
	ms.fss = make(map[string]*SimpleState)
	ms.mu.Unlock()

	if cb != nil {
		cb(-int64(purged), -bytes, 0, _EMPTY_)
	}

	return purged, nil
}

// Compact will remove all messages from this store up to
// but not including the seq parameter.
// Will return the number of purged messages.
func (ms *memStore) Compact(seq uint64) (uint64, error) {
	if seq == 0 {
		return ms.Purge()
	}

	var purged, bytes uint64

	ms.mu.Lock()
	cb := ms.scb
	if seq <= ms.state.LastSeq {
		sm, ok := ms.msgs[seq]
		if !ok {
			ms.mu.Unlock()
			return 0, ErrStoreMsgNotFound
		}
		ms.state.FirstSeq = seq
		ms.state.FirstTime = time.Unix(0, sm.ts).UTC()

		for seq := seq - 1; seq > 0; seq-- {
			if sm := ms.msgs[seq]; sm != nil {
				bytes += memStoreMsgSize(sm.subj, sm.hdr, sm.msg)
				purged++
				delete(ms.msgs, seq)
				ms.removeSeqPerSubject(sm.subj, seq)
			}
		}
		ms.state.Msgs -= purged
		ms.state.Bytes -= bytes
	} else {
		// We are compacting past the end of our range. Do purge and set sequences correctly
		// such that the next message placed will have seq.
		purged = uint64(len(ms.msgs))
		bytes = ms.state.Bytes
		ms.state.Bytes = 0
		ms.state.Msgs = 0
		ms.state.FirstSeq = seq
		ms.state.FirstTime = time.Time{}
		ms.state.LastSeq = seq - 1
		ms.msgs = make(map[uint64]*StoreMsg)
	}
	ms.mu.Unlock()

	if cb != nil {
		cb(-int64(purged), -int64(bytes), 0, _EMPTY_)
	}

	return purged, nil
}

// Will completely reset our store.
func (ms *memStore) reset() error {

	ms.mu.Lock()
	var purged, bytes uint64
	cb := ms.scb
	if cb != nil {
		for _, sm := range ms.msgs {
			purged++
			bytes += memStoreMsgSize(sm.subj, sm.hdr, sm.msg)
		}
	}

	// Reset
	ms.state.FirstSeq = 0
	ms.state.FirstTime = time.Time{}
	ms.state.LastSeq = 0
	ms.state.LastTime = time.Now().UTC()
	// Update msgs and bytes.
	ms.state.Msgs = 0
	ms.state.Bytes = 0
	// Reset msgs and fss.
	ms.msgs = make(map[uint64]*StoreMsg)
	ms.fss = make(map[string]*SimpleState)

	ms.mu.Unlock()

	if cb != nil {
		cb(-int64(purged), -int64(bytes), 0, _EMPTY_)
	}

	return nil
}

// Truncate will truncate a stream store up to seq. Sequence needs to be valid.
func (ms *memStore) Truncate(seq uint64) error {
	// Check for request to reset.
	if seq == 0 {
		return ms.reset()
	}

	var purged, bytes uint64

	ms.mu.Lock()
	lsm, ok := ms.msgs[seq]
	if !ok {
		ms.mu.Unlock()
		return ErrInvalidSequence
	}

	for i := ms.state.LastSeq; i > seq; i-- {
		if sm := ms.msgs[i]; sm != nil {
			purged++
			bytes += memStoreMsgSize(sm.subj, sm.hdr, sm.msg)
			delete(ms.msgs, i)
			ms.removeSeqPerSubject(sm.subj, i)
		}
	}
	// Reset last.
	ms.state.LastSeq = lsm.seq
	ms.state.LastTime = time.Unix(0, lsm.ts).UTC()
	// Update msgs and bytes.
	ms.state.Msgs -= purged
	ms.state.Bytes -= bytes

	cb := ms.scb
	ms.mu.Unlock()

	if cb != nil {
		cb(-int64(purged), -int64(bytes), 0, _EMPTY_)
	}

	return nil
}

func (ms *memStore) deleteFirstMsgOrPanic() {
	if !ms.deleteFirstMsg() {
		panic("jetstream memstore has inconsistent state, can't find first seq msg")
	}
}

func (ms *memStore) deleteFirstMsg() bool {
	return ms.removeMsg(ms.state.FirstSeq, false)
}

// LoadMsg will lookup the message by sequence number and return it if found.
func (ms *memStore) LoadMsg(seq uint64, smp *StoreMsg) (*StoreMsg, error) {
	ms.mu.RLock()
	sm, ok := ms.msgs[seq]
	last := ms.state.LastSeq
	ms.mu.RUnlock()

	if !ok || sm == nil {
		var err = ErrStoreEOF
		if seq <= last {
			err = ErrStoreMsgNotFound
		}
		return nil, err
	}

	if smp == nil {
		smp = new(StoreMsg)
	}
	sm.copy(smp)
	return smp, nil
}

// LoadLastMsg will return the last message we have that matches a given subject.
// The subject can be a wildcard.
func (ms *memStore) LoadLastMsg(subject string, smp *StoreMsg) (*StoreMsg, error) {
	var sm *StoreMsg
	var ok bool

	ms.mu.RLock()
	defer ms.mu.RUnlock()

	if subject == _EMPTY_ || subject == fwcs {
		sm, ok = ms.msgs[ms.state.LastSeq]
	} else if ss := ms.filteredStateLocked(1, subject, true); ss.Msgs > 0 {
		sm, ok = ms.msgs[ss.Last]
	}
	if !ok || sm == nil {
		return nil, ErrStoreMsgNotFound
	}

	if smp == nil {
		smp = new(StoreMsg)
	}
	sm.copy(smp)
	return smp, nil
}

// LoadNextMsg will find the next message matching the filter subject starting at the start sequence.
// The filter subject can be a wildcard.
func (ms *memStore) LoadNextMsg(filter string, wc bool, start uint64, smp *StoreMsg) (*StoreMsg, uint64, error) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	if start < ms.state.FirstSeq {
		start = ms.state.FirstSeq
	}

	// If past the end no results.
	if start > ms.state.LastSeq {
		return nil, ms.state.LastSeq, ErrStoreEOF
	}

	isAll := filter == _EMPTY_ || filter == fwcs

	// Skip scan of mb.fss is number of messages in the block are less than
	// 1/2 the number of subjects in mb.fss. Or we have a wc and lots of fss entries.
	const linearScanMaxFSS = 256
	doLinearScan := isAll || 2*int(ms.state.LastSeq-start) < len(ms.fss) || (wc && len(ms.fss) > linearScanMaxFSS)

	// Initial setup.
	fseq, lseq := start, ms.state.LastSeq

	if !doLinearScan {
		subs := []string{filter}
		if wc || isAll {
			subs = subs[:0]
			for fsubj := range ms.fss {
				if isAll || subjectIsSubsetMatch(fsubj, filter) {
					subs = append(subs, fsubj)
				}
			}
		}
		fseq, lseq = ms.state.LastSeq, uint64(0)
		for _, subj := range subs {
			ss := ms.fss[subj]
			if ss == nil {
				continue
			}
			if ss.First < fseq {
				fseq = ss.First
			}
			if ss.Last > lseq {
				lseq = ss.Last
			}
		}
		if fseq < start {
			fseq = start
		}
	}

	eq := subjectsEqual
	if wc {
		eq = subjectIsSubsetMatch
	}

	for nseq := fseq; nseq <= lseq; nseq++ {
		if sm, ok := ms.msgs[nseq]; ok && (isAll || eq(sm.subj, filter)) {
			if smp == nil {
				smp = new(StoreMsg)
			}
			sm.copy(smp)
			return smp, nseq, nil
		}
	}
	return nil, ms.state.LastSeq, ErrStoreEOF
}

// RemoveMsg will remove the message from this store.
// Will return the number of bytes removed.
func (ms *memStore) RemoveMsg(seq uint64) (bool, error) {
	ms.mu.Lock()
	removed := ms.removeMsg(seq, false)
	ms.mu.Unlock()
	return removed, nil
}

// EraseMsg will remove the message and rewrite its contents.
func (ms *memStore) EraseMsg(seq uint64) (bool, error) {
	ms.mu.Lock()
	removed := ms.removeMsg(seq, true)
	ms.mu.Unlock()
	return removed, nil
}

// Performs logic to update first sequence number.
// Lock should be held.
func (ms *memStore) updateFirstSeq(seq uint64) {
	if seq != ms.state.FirstSeq {
		// Interior delete.
		return
	}
	var nsm *StoreMsg
	var ok bool
	for nseq := ms.state.FirstSeq + 1; nseq <= ms.state.LastSeq; nseq++ {
		if nsm, ok = ms.msgs[nseq]; ok {
			break
		}
	}
	if nsm != nil {
		ms.state.FirstSeq = nsm.seq
		ms.state.FirstTime = time.Unix(0, nsm.ts).UTC()
	} else {
		// Like purge.
		ms.state.FirstSeq = ms.state.LastSeq + 1
		ms.state.FirstTime = time.Time{}
	}
}

// Remove a seq from the fss and select new first.
// Lock should be held.
func (ms *memStore) removeSeqPerSubject(subj string, seq uint64) {
	ss := ms.fss[subj]
	if ss == nil {
		return
	}
	if ss.Msgs == 1 {
		delete(ms.fss, subj)
		return
	}
	ss.Msgs--
	if seq != ss.First {
		return
	}
	// If we know we only have 1 msg left don't need to search for next first.
	if ss.Msgs == 1 {
		ss.First = ss.Last
		return
	}
	// TODO(dlc) - Might want to optimize this longer term.
	for tseq := seq + 1; tseq <= ss.Last; tseq++ {
		if sm := ms.msgs[tseq]; sm != nil && sm.subj == subj {
			ss.First = tseq
			break
		}
	}
}

// Removes the message referenced by seq.
// Lock should he held.
func (ms *memStore) removeMsg(seq uint64, secure bool) bool {
	var ss uint64
	sm, ok := ms.msgs[seq]
	if !ok {
		return false
	}

	ss = memStoreMsgSize(sm.subj, sm.hdr, sm.msg)

	delete(ms.msgs, seq)
	ms.state.Msgs--
	ms.state.Bytes -= ss
	ms.updateFirstSeq(seq)

	if secure {
		if len(sm.hdr) > 0 {
			sm.hdr = make([]byte, len(sm.hdr))
			rand.Read(sm.hdr)
		}
		if len(sm.msg) > 0 {
			sm.msg = make([]byte, len(sm.msg))
			rand.Read(sm.msg)
		}
		sm.seq, sm.ts = 0, 0
	}

	// Remove any per subject tracking.
	ms.removeSeqPerSubject(sm.subj, seq)

	if ms.scb != nil {
		// We do not want to hold any locks here.
		ms.mu.Unlock()
		delta := int64(ss)
		ms.scb(-1, -delta, seq, sm.subj)
		ms.mu.Lock()
	}

	return ok
}

// Type returns the type of the underlying store.
func (ms *memStore) Type() StorageType {
	return MemoryStorage
}

// FastState will fill in state with only the following.
// Msgs, Bytes, First and Last Sequence and Time and NumDeleted.
func (ms *memStore) FastState(state *StreamState) {
	ms.mu.RLock()
	state.Msgs = ms.state.Msgs
	state.Bytes = ms.state.Bytes
	state.FirstSeq = ms.state.FirstSeq
	state.FirstTime = ms.state.FirstTime
	state.LastSeq = ms.state.LastSeq
	state.LastTime = ms.state.LastTime
	if state.LastSeq > state.FirstSeq {
		state.NumDeleted = int((state.LastSeq - state.FirstSeq + 1) - state.Msgs)
		if state.NumDeleted < 0 {
			state.NumDeleted = 0
		}
	}
	state.Consumers = ms.consumers
	state.NumSubjects = len(ms.fss)
	ms.mu.RUnlock()
}

func (ms *memStore) State() StreamState {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	state := ms.state
	state.Consumers = ms.consumers
	state.NumSubjects = len(ms.fss)
	state.Deleted = nil

	// Calculate interior delete details.
	if numDeleted := int((state.LastSeq - state.FirstSeq + 1) - state.Msgs); numDeleted > 0 {
		state.Deleted = make([]uint64, 0, state.NumDeleted)
		// TODO(dlc) - Too Simplistic, once state is updated to allow runs etc redo.
		for seq := state.FirstSeq + 1; seq < ms.state.LastSeq; seq++ {
			if _, ok := ms.msgs[seq]; !ok {
				state.Deleted = append(state.Deleted, seq)
			}
		}
	}
	if len(state.Deleted) > 0 {
		state.NumDeleted = len(state.Deleted)
	}

	return state
}

func (ms *memStore) Utilization() (total, reported uint64, err error) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	return ms.state.Bytes, ms.state.Bytes, nil
}

func memStoreMsgSize(subj string, hdr, msg []byte) uint64 {
	return uint64(len(subj) + len(hdr) + len(msg) + 16) // 8*2 for seq + age
}

// Delete is same as Stop for memory store.
func (ms *memStore) Delete() error {
	ms.Purge()
	return ms.Stop()
}

func (ms *memStore) Stop() error {
	ms.mu.Lock()
	if ms.ageChk != nil {
		ms.ageChk.Stop()
		ms.ageChk = nil
	}
	ms.msgs = nil
	ms.mu.Unlock()
	return nil
}

func (ms *memStore) isClosed() bool {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	return ms.msgs == nil
}

type consumerMemStore struct {
	mu     sync.Mutex
	ms     StreamStore
	cfg    ConsumerConfig
	state  ConsumerState
	closed bool
}

func (ms *memStore) ConsumerStore(name string, cfg *ConsumerConfig) (ConsumerStore, error) {
	if ms == nil {
		return nil, fmt.Errorf("memstore is nil")
	}
	if ms.isClosed() {
		return nil, ErrStoreClosed
	}
	if cfg == nil || name == _EMPTY_ {
		return nil, fmt.Errorf("bad consumer config")
	}
	o := &consumerMemStore{ms: ms, cfg: *cfg}
	ms.AddConsumer(o)
	return o, nil
}

func (ms *memStore) AddConsumer(o ConsumerStore) error {
	ms.mu.Lock()
	ms.consumers++
	ms.mu.Unlock()
	return nil
}

func (ms *memStore) RemoveConsumer(o ConsumerStore) error {
	ms.mu.Lock()
	if ms.consumers > 0 {
		ms.consumers--
	}
	ms.mu.Unlock()
	return nil
}

func (ms *memStore) Snapshot(_ time.Duration, _, _ bool) (*SnapshotResult, error) {
	return nil, fmt.Errorf("no impl")
}

func (o *consumerMemStore) Update(state *ConsumerState) error {
	// Sanity checks.
	if state.AckFloor.Consumer > state.Delivered.Consumer {
		return fmt.Errorf("bad ack floor for consumer")
	}
	if state.AckFloor.Stream > state.Delivered.Stream {
		return fmt.Errorf("bad ack floor for stream")
	}

	// Copy to our state.
	var pending map[uint64]*Pending
	var redelivered map[uint64]uint64
	if len(state.Pending) > 0 {
		pending = make(map[uint64]*Pending, len(state.Pending))
		for seq, p := range state.Pending {
			pending[seq] = &Pending{p.Sequence, p.Timestamp}
		}
		for seq := range pending {
			if seq <= state.AckFloor.Stream || seq > state.Delivered.Stream {
				return fmt.Errorf("bad pending entry, sequence [%d] out of range", seq)
			}
		}
	}
	if len(state.Redelivered) > 0 {
		redelivered = make(map[uint64]uint64, len(state.Redelivered))
		for seq, dc := range state.Redelivered {
			redelivered[seq] = dc
		}
	}

	// Replace our state.
	o.mu.Lock()

	// Check to see if this is an outdated update.
	if state.Delivered.Consumer < o.state.Delivered.Consumer {
		o.mu.Unlock()
		return fmt.Errorf("old update ignored")
	}

	o.state.Delivered = state.Delivered
	o.state.AckFloor = state.AckFloor
	o.state.Pending = pending
	o.state.Redelivered = redelivered
	o.mu.Unlock()

	return nil
}

// SetStarting sets our starting stream sequence.
func (o *consumerMemStore) SetStarting(sseq uint64) error {
	o.mu.Lock()
	o.state.Delivered.Stream = sseq
	o.mu.Unlock()
	return nil
}

// HasState returns if this store has a recorded state.
func (o *consumerMemStore) HasState() bool {
	return false
}

func (o *consumerMemStore) UpdateDelivered(dseq, sseq, dc uint64, ts int64) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if dc != 1 && o.cfg.AckPolicy == AckNone {
		return ErrNoAckPolicy
	}

	if dseq <= o.state.AckFloor.Consumer {
		return nil
	}

	// See if we expect an ack for this.
	if o.cfg.AckPolicy != AckNone {
		// Need to create pending records here.
		if o.state.Pending == nil {
			o.state.Pending = make(map[uint64]*Pending)
		}
		var p *Pending
		// Check for an update to a message already delivered.
		if sseq <= o.state.Delivered.Stream {
			if p = o.state.Pending[sseq]; p != nil {
				p.Sequence, p.Timestamp = dseq, ts
			}
		} else {
			// Add to pending.
			o.state.Pending[sseq] = &Pending{dseq, ts}
		}
		// Update delivered as needed.
		if dseq > o.state.Delivered.Consumer {
			o.state.Delivered.Consumer = dseq
		}
		if sseq > o.state.Delivered.Stream {
			o.state.Delivered.Stream = sseq
		}

		if dc > 1 {
			if o.state.Redelivered == nil {
				o.state.Redelivered = make(map[uint64]uint64)
			}
			o.state.Redelivered[sseq] = dc - 1
		}
	} else {
		// For AckNone just update delivered and ackfloor at the same time.
		o.state.Delivered.Consumer = dseq
		o.state.Delivered.Stream = sseq
		o.state.AckFloor.Consumer = dseq
		o.state.AckFloor.Stream = sseq
	}

	return nil
}

func (o *consumerMemStore) UpdateAcks(dseq, sseq uint64) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.cfg.AckPolicy == AckNone {
		return ErrNoAckPolicy
	}
	if len(o.state.Pending) == 0 || o.state.Pending[sseq] == nil {
		return ErrStoreMsgNotFound
	}

	// On restarts the old leader may get a replay from the raft logs that are old.
	if dseq <= o.state.AckFloor.Consumer {
		return nil
	}

	// Check for AckAll here.
	if o.cfg.AckPolicy == AckAll {
		sgap := sseq - o.state.AckFloor.Stream
		o.state.AckFloor.Consumer = dseq
		o.state.AckFloor.Stream = sseq
		for seq := sseq; seq > sseq-sgap; seq-- {
			delete(o.state.Pending, seq)
			if len(o.state.Redelivered) > 0 {
				delete(o.state.Redelivered, seq)
			}
		}
		return nil
	}

	// AckExplicit

	// First delete from our pending state.
	if p, ok := o.state.Pending[sseq]; ok {
		delete(o.state.Pending, sseq)
		dseq = p.Sequence // Use the original.
	}
	// Now remove from redelivered.
	if len(o.state.Redelivered) > 0 {
		delete(o.state.Redelivered, sseq)
	}

	if len(o.state.Pending) == 0 {
		o.state.AckFloor.Consumer = o.state.Delivered.Consumer
		o.state.AckFloor.Stream = o.state.Delivered.Stream
	} else if dseq == o.state.AckFloor.Consumer+1 {
		first := o.state.AckFloor.Consumer == 0
		o.state.AckFloor.Consumer = dseq
		o.state.AckFloor.Stream = sseq

		if !first && o.state.Delivered.Consumer > dseq {
			for ss := sseq + 1; ss < o.state.Delivered.Stream; ss++ {
				if p, ok := o.state.Pending[ss]; ok {
					if p.Sequence > 0 {
						o.state.AckFloor.Consumer = p.Sequence - 1
						o.state.AckFloor.Stream = ss - 1
					}
					break
				}
			}
		}
	}

	return nil
}

func (o *consumerMemStore) UpdateConfig(cfg *ConsumerConfig) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	// This is mostly unchecked here. We are assuming the upper layers have done sanity checking.
	o.cfg = *cfg
	return nil
}

func (o *consumerMemStore) Stop() error {
	o.mu.Lock()
	o.closed = true
	ms := o.ms
	o.mu.Unlock()
	ms.RemoveConsumer(o)
	return nil
}

func (o *consumerMemStore) Delete() error {
	return o.Stop()
}

func (o *consumerMemStore) StreamDelete() error {
	return o.Stop()
}

func (o *consumerMemStore) State() (*ConsumerState, error) {
	return o.stateWithCopy(true)
}

// This will not copy pending or redelivered, so should only be done under the
// consumer owner's lock.
func (o *consumerMemStore) BorrowState() (*ConsumerState, error) {
	return o.stateWithCopy(false)
}

func (o *consumerMemStore) stateWithCopy(doCopy bool) (*ConsumerState, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.closed {
		return nil, ErrStoreClosed
	}

	state := &ConsumerState{}

	state.Delivered = o.state.Delivered
	state.AckFloor = o.state.AckFloor
	if len(o.state.Pending) > 0 {
		if doCopy {
			state.Pending = o.copyPending()
		} else {
			state.Pending = o.state.Pending
		}
	}
	if len(o.state.Redelivered) > 0 {
		if doCopy {
			state.Redelivered = o.copyRedelivered()
		} else {
			state.Redelivered = o.state.Redelivered
		}
	}
	return state, nil
}

// EncodeState for this consumer store.
func (o *consumerMemStore) EncodedState() ([]byte, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.closed {
		return nil, ErrStoreClosed
	}

	return encodeConsumerState(&o.state), nil
}

func (o *consumerMemStore) copyPending() map[uint64]*Pending {
	pending := make(map[uint64]*Pending, len(o.state.Pending))
	for seq, p := range o.state.Pending {
		pending[seq] = &Pending{p.Sequence, p.Timestamp}
	}
	return pending
}

func (o *consumerMemStore) copyRedelivered() map[uint64]uint64 {
	redelivered := make(map[uint64]uint64, len(o.state.Redelivered))
	for seq, dc := range o.state.Redelivered {
		redelivered[seq] = dc
	}
	return redelivered
}

// Type returns the type of the underlying store.
func (o *consumerMemStore) Type() StorageType { return MemoryStorage }

// Templates
type templateMemStore struct{}

func newTemplateMemStore() *templateMemStore {
	return &templateMemStore{}
}

// No-ops for memstore.
func (ts *templateMemStore) Store(t *streamTemplate) error  { return nil }
func (ts *templateMemStore) Delete(t *streamTemplate) error { return nil }
