package kfake

import (
	"hash/fnv"
	"math"
	"math/rand"
)

// TODO
//
// * Convert pids to struct, add heap of last use, add index to pidseqs, and
// remove pidseqs as they exhaust max # of pids configured.
//
// * Wrap epochs

type (
	pids map[int64]*pidMap

	pidMap struct {
		id    int64
		epoch int16
		tps   tps[pidseqs]
	}

	pid struct {
		id    int64
		epoch int16
	}

	pidseqs struct {
		seqs [5]int32
		at   uint8
	}
)

func (pids *pids) get(id int64, epoch int16, t string, p int32) (*pidseqs, int16) {
	if *pids == nil {
		return nil, 0
	}
	pm := (*pids)[id]
	if pm == nil {
		return nil, 0
	}
	return pm.tps.mkpDefault(t, p), pm.epoch
}

func (pids *pids) create(txnalID *string) pid {
	if *pids == nil {
		*pids = make(map[int64]*pidMap)
	}
	var id int64
	if txnalID != nil {
		hasher := fnv.New64()
		hasher.Write([]byte(*txnalID))
		id = int64(hasher.Sum64()) & math.MaxInt64
	} else {
		for {
			id = int64(rand.Uint64()) & math.MaxInt64
			if _, exists := (*pids)[id]; !exists {
				break
			}
		}
	}
	pm, exists := (*pids)[id]
	if exists {
		pm.epoch++
		return pid{id, pm.epoch}
	}
	pm = &pidMap{id: id}
	(*pids)[id] = pm
	return pid{id, 0}
}

func (seqs *pidseqs) pushAndValidate(firstSeq, numRecs int32) (ok, dup bool) {
	// If there is no pid, we do not do duplicate detection.
	if seqs == nil {
		return true, false
	}
	var (
		seq    = firstSeq
		seq64  = int64(seq)
		next64 = (seq64 + int64(numRecs)) % math.MaxInt32
		next   = int32(next64)
	)
	for i := 0; i < 5; i++ {
		if seqs.seqs[i] == seq && seqs.seqs[(i+1)%5] == next {
			return true, true
		}
	}
	if seqs.seqs[seqs.at] != seq {
		return false, false
	}
	seqs.at = (seqs.at + 1) % 5
	seqs.seqs[seqs.at] = next
	return true, false
}
