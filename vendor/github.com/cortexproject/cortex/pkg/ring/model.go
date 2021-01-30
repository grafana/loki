package ring

import (
	"container/heap"
	"fmt"
	"sort"
	"time"

	"github.com/gogo/protobuf/proto"

	"github.com/cortexproject/cortex/pkg/ring/kv/codec"
	"github.com/cortexproject/cortex/pkg/ring/kv/memberlist"
)

// ByAddr is a sortable list of IngesterDesc.
type ByAddr []IngesterDesc

func (ts ByAddr) Len() int           { return len(ts) }
func (ts ByAddr) Swap(i, j int)      { ts[i], ts[j] = ts[j], ts[i] }
func (ts ByAddr) Less(i, j int) bool { return ts[i].Addr < ts[j].Addr }

// ProtoDescFactory makes new Descs
func ProtoDescFactory() proto.Message {
	return NewDesc()
}

// GetCodec returns the codec used to encode and decode data being put by ring.
func GetCodec() codec.Codec {
	return codec.NewProtoCodec("ringDesc", ProtoDescFactory)
}

// NewDesc returns an empty ring.Desc
func NewDesc() *Desc {
	return &Desc{
		Ingesters: map[string]IngesterDesc{},
	}
}

// AddIngester adds the given ingester to the ring. Ingester will only use supplied tokens,
// any other tokens are removed.
func (d *Desc) AddIngester(id, addr, zone string, tokens []uint32, state IngesterState, registeredAt time.Time) IngesterDesc {
	if d.Ingesters == nil {
		d.Ingesters = map[string]IngesterDesc{}
	}

	registeredTimestamp := int64(0)
	if !registeredAt.IsZero() {
		registeredTimestamp = registeredAt.Unix()
	}

	ingester := IngesterDesc{
		Addr:                addr,
		Timestamp:           time.Now().Unix(),
		RegisteredTimestamp: registeredTimestamp,
		State:               state,
		Tokens:              tokens,
		Zone:                zone,
	}

	d.Ingesters[id] = ingester
	return ingester
}

// RemoveIngester removes the given ingester and all its tokens.
func (d *Desc) RemoveIngester(id string) {
	delete(d.Ingesters, id)
}

// ClaimTokens transfers all the tokens from one ingester to another,
// returning the claimed token.
// This method assumes that Ring is in the correct state, 'to' ingester has no tokens anywhere.
// Tokens list must be sorted properly. If all of this is true, everything will be fine.
func (d *Desc) ClaimTokens(from, to string) Tokens {
	var result Tokens

	if fromDesc, found := d.Ingesters[from]; found {
		result = fromDesc.Tokens
		fromDesc.Tokens = nil
		d.Ingesters[from] = fromDesc
	}

	ing := d.Ingesters[to]
	ing.Tokens = result
	d.Ingesters[to] = ing

	return result
}

// FindIngestersByState returns the list of ingesters in the given state
func (d *Desc) FindIngestersByState(state IngesterState) []IngesterDesc {
	var result []IngesterDesc
	for _, ing := range d.Ingesters {
		if ing.State == state {
			result = append(result, ing)
		}
	}
	return result
}

// Ready returns no error when all ingesters are active and healthy.
func (d *Desc) Ready(now time.Time, heartbeatTimeout time.Duration) error {
	numTokens := 0
	for id, ingester := range d.Ingesters {
		if now.Sub(time.Unix(ingester.Timestamp, 0)) > heartbeatTimeout {
			return fmt.Errorf("instance %s past heartbeat timeout", id)
		} else if ingester.State != ACTIVE {
			return fmt.Errorf("instance %s in state %v", id, ingester.State)
		}
		numTokens += len(ingester.Tokens)
	}

	if numTokens == 0 {
		return fmt.Errorf("no tokens in ring")
	}
	return nil
}

// TokensFor return all ring tokens and tokens for the input provided ID.
// Returned tokens are guaranteed to be sorted.
func (d *Desc) TokensFor(id string) (myTokens, allTokens Tokens) {
	allTokens = d.GetTokens()
	myTokens = d.Ingesters[id].Tokens
	return
}

// GetRegisteredAt returns the timestamp when the instance has been registered to the ring
// or a zero value if unknown.
func (i *IngesterDesc) GetRegisteredAt() time.Time {
	if i == nil || i.RegisteredTimestamp == 0 {
		return time.Time{}
	}

	return time.Unix(i.RegisteredTimestamp, 0)
}

func (i *IngesterDesc) IsHealthy(op Operation, heartbeatTimeout time.Duration, now time.Time) bool {
	healthy := op.IsInstanceInStateHealthy(i.State)

	return healthy && now.Unix()-i.Timestamp <= heartbeatTimeout.Milliseconds()/1000
}

// Merge merges other ring into this one. Returns sub-ring that represents the change,
// and can be sent out to other clients.
//
// This merge function depends on the timestamp of the ingester. For each ingester,
// it will choose more recent state from the two rings, and put that into this ring.
// There is one exception: we accept LEFT state even if Timestamp hasn't changed.
//
// localCAS flag tells the merge that it can use incoming ring as a full state, and detect
// missing ingesters based on it. Ingesters from incoming ring will cause ingester
// to be marked as LEFT and gossiped about.
//
// If multiple ingesters end up owning the same tokens, Merge will do token conflict resolution
// (see resolveConflicts).
//
// This method is part of memberlist.Mergeable interface, and is only used by gossiping ring.
func (d *Desc) Merge(mergeable memberlist.Mergeable, localCAS bool) (memberlist.Mergeable, error) {
	return d.mergeWithTime(mergeable, localCAS, time.Now())
}

func (d *Desc) mergeWithTime(mergeable memberlist.Mergeable, localCAS bool, now time.Time) (memberlist.Mergeable, error) {
	if mergeable == nil {
		return nil, nil
	}

	other, ok := mergeable.(*Desc)
	if !ok {
		// This method only deals with non-nil rings.
		return nil, fmt.Errorf("expected *ring.Desc, got %T", mergeable)
	}

	if other == nil {
		return nil, nil
	}

	thisIngesterMap := buildNormalizedIngestersMap(d)
	otherIngesterMap := buildNormalizedIngestersMap(other)

	var updated []string

	for name, oing := range otherIngesterMap {
		ting := thisIngesterMap[name]
		// ting.Timestamp will be 0, if there was no such ingester in our version
		if oing.Timestamp > ting.Timestamp {
			oing.Tokens = append([]uint32(nil), oing.Tokens...) // make a copy of tokens
			thisIngesterMap[name] = oing
			updated = append(updated, name)
		} else if oing.Timestamp == ting.Timestamp && ting.State != LEFT && oing.State == LEFT {
			// we accept LEFT even if timestamp hasn't changed
			thisIngesterMap[name] = oing // has no tokens already
			updated = append(updated, name)
		}
	}

	if localCAS {
		// This breaks commutativity! But we only do it locally, not when gossiping with others.
		for name, ting := range thisIngesterMap {
			if _, ok := otherIngesterMap[name]; !ok && ting.State != LEFT {
				// missing, let's mark our ingester as LEFT
				ting.State = LEFT
				ting.Tokens = nil
				// We are deleting entry "now", and should not keep old timestamp, because there may already be pending
				// message in the gossip network with newer timestamp (but still older than "now").
				// Such message would "resurrect" this deleted entry.
				ting.Timestamp = now.Unix()
				thisIngesterMap[name] = ting

				updated = append(updated, name)
			}
		}
	}

	// No updated ingesters
	if len(updated) == 0 {
		return nil, nil
	}

	// resolveConflicts allocates lot of memory, so if we can avoid it, do that.
	if conflictingTokensExist(thisIngesterMap) {
		resolveConflicts(thisIngesterMap)
	}

	// Let's build a "change" for returning
	out := NewDesc()
	for _, u := range updated {
		ing := thisIngesterMap[u]
		out.Ingesters[u] = ing
	}

	d.Ingesters = thisIngesterMap

	return out, nil
}

// MergeContent describes content of this Mergeable.
// Ring simply returns list of ingesters that it includes.
func (d *Desc) MergeContent() []string {
	result := []string(nil)
	for k := range d.Ingesters {
		result = append(result, k)
	}
	return result
}

// buildNormalizedIngestersMap will do the following:
// - sorts tokens and removes duplicates (only within single ingester)
// - it doesn't modify input ring
func buildNormalizedIngestersMap(inputRing *Desc) map[string]IngesterDesc {
	out := map[string]IngesterDesc{}

	// Make sure LEFT ingesters have no tokens
	for n, ing := range inputRing.Ingesters {
		if ing.State == LEFT {
			ing.Tokens = nil
		}
		out[n] = ing
	}

	// Sort tokens, and remove duplicates
	for name, ing := range out {
		if len(ing.Tokens) == 0 {
			continue
		}

		if !sort.IsSorted(Tokens(ing.Tokens)) {
			sort.Sort(Tokens(ing.Tokens))
		}

		// tokens are sorted now, we can easily remove duplicates.
		prev := ing.Tokens[0]
		for ix := 1; ix < len(ing.Tokens); {
			if ing.Tokens[ix] == prev {
				ing.Tokens = append(ing.Tokens[:ix], ing.Tokens[ix+1:]...)
			} else {
				prev = ing.Tokens[ix]
				ix++
			}
		}

		// write updated value back to map
		out[name] = ing
	}

	return out
}

func conflictingTokensExist(normalizedIngesters map[string]IngesterDesc) bool {
	count := 0
	for _, ing := range normalizedIngesters {
		count += len(ing.Tokens)
	}

	tokensMap := make(map[uint32]bool, count)
	for _, ing := range normalizedIngesters {
		for _, t := range ing.Tokens {
			if tokensMap[t] {
				return true
			}
			tokensMap[t] = true
		}
	}
	return false
}

// This function resolves token conflicts, if there are any.
//
// We deal with two possibilities:
// 1) if one node is LEAVING or LEFT and the other node is not, LEVING/LEFT one loses the token
// 2) otherwise node names are compared, and node with "lower" name wins the token
//
// Modifies ingesters map with updated tokens.
func resolveConflicts(normalizedIngesters map[string]IngesterDesc) {
	size := 0
	for _, ing := range normalizedIngesters {
		size += len(ing.Tokens)
	}
	tokens := make([]uint32, 0, size)
	tokenToIngester := make(map[uint32]string, size)

	for ingKey, ing := range normalizedIngesters {
		if ing.State == LEFT {
			// LEFT ingesters don't use tokens anymore
			continue
		}

		for _, token := range ing.Tokens {
			prevKey, found := tokenToIngester[token]
			if !found {
				tokens = append(tokens, token)
				tokenToIngester[token] = ingKey
			} else {
				// there is already ingester for this token, let's do conflict resolution
				prevIng := normalizedIngesters[prevKey]

				winnerKey := ingKey
				switch {
				case ing.State == LEAVING && prevIng.State != LEAVING:
					winnerKey = prevKey
				case prevIng.State == LEAVING && ing.State != LEAVING:
					winnerKey = ingKey
				case ingKey < prevKey:
					winnerKey = ingKey
				case prevKey < ingKey:
					winnerKey = prevKey
				}

				tokenToIngester[token] = winnerKey
			}
		}
	}

	sort.Sort(Tokens(tokens))

	// let's store the resolved result back
	newTokenLists := map[string][]uint32{}
	for key := range normalizedIngesters {
		// make sure that all ingesters start with empty list
		// especially ones that will no longer have any tokens
		newTokenLists[key] = nil
	}

	// build list of tokens for each ingester
	for _, token := range tokens {
		key := tokenToIngester[token]
		newTokenLists[key] = append(newTokenLists[key], token)
	}

	// write tokens back
	for key, tokens := range newTokenLists {
		ing := normalizedIngesters[key]
		ing.Tokens = tokens
		normalizedIngesters[key] = ing
	}
}

// RemoveTombstones removes LEFT ingesters older than given time limit. If time limit is zero, remove all LEFT ingesters.
func (d *Desc) RemoveTombstones(limit time.Time) {
	removed := 0
	for n, ing := range d.Ingesters {
		if ing.State == LEFT && (limit.IsZero() || time.Unix(ing.Timestamp, 0).Before(limit)) {
			// remove it
			delete(d.Ingesters, n)
			removed++
		}
	}
}

func (d *Desc) getTokensInfo() map[uint32]instanceInfo {
	out := map[uint32]instanceInfo{}

	for instanceID, instance := range d.Ingesters {
		info := instanceInfo{
			InstanceID: instanceID,
			Zone:       instance.Zone,
		}

		for _, token := range instance.Tokens {
			out[token] = info
		}
	}

	return out
}

// GetTokens returns sorted list of tokens owned by all instances within the ring.
func (d *Desc) GetTokens() []uint32 {
	instances := make([][]uint32, 0, len(d.Ingesters))
	for _, instance := range d.Ingesters {
		instances = append(instances, instance.Tokens)
	}

	return MergeTokens(instances)
}

// getTokensByZone returns instances tokens grouped by zone. Tokens within each zone
// are guaranteed to be sorted.
func (d *Desc) getTokensByZone() map[string][]uint32 {
	zones := map[string][][]uint32{}
	for _, instance := range d.Ingesters {
		zones[instance.Zone] = append(zones[instance.Zone], instance.Tokens)
	}

	// Merge tokens per zone.
	return MergeTokensByZone(zones)
}

type CompareResult int

const (
	Equal                       CompareResult = iota // Both rings contain same exact instances.
	EqualButStatesAndTimestamps                      // Both rings contain the same instances with the same data except states and timestamps (may differ).
	Different                                        // Rings have different set of instances, or their information don't match.
)

// RingCompare compares this ring against another one and returns one of Equal, EqualButStatesAndTimestamps or Different.
func (d *Desc) RingCompare(o *Desc) CompareResult {
	if d == nil {
		if o == nil || len(o.Ingesters) == 0 {
			return Equal
		}
		return Different
	}
	if o == nil {
		if len(d.Ingesters) == 0 {
			return Equal
		}
		return Different
	}

	if len(d.Ingesters) != len(o.Ingesters) {
		return Different
	}

	equalStatesAndTimestamps := true

	for name, ing := range d.Ingesters {
		oing, ok := o.Ingesters[name]
		if !ok {
			return Different
		}

		if ing.Addr != oing.Addr {
			return Different
		}

		if ing.Zone != oing.Zone {
			return Different
		}

		if ing.RegisteredTimestamp != oing.RegisteredTimestamp {
			return Different
		}

		if len(ing.Tokens) != len(oing.Tokens) {
			return Different
		}

		for ix, t := range ing.Tokens {
			if oing.Tokens[ix] != t {
				return Different
			}
		}

		if ing.Timestamp != oing.Timestamp {
			equalStatesAndTimestamps = false
		}

		if ing.State != oing.State {
			equalStatesAndTimestamps = false
		}
	}

	if equalStatesAndTimestamps {
		return Equal
	}
	return EqualButStatesAndTimestamps
}

func GetOrCreateRingDesc(d interface{}) *Desc {
	if d == nil {
		return NewDesc()
	}
	return d.(*Desc)
}

// TokensHeap is an heap data structure used to merge multiple lists
// of sorted tokens into a single one.
type TokensHeap [][]uint32

func (h TokensHeap) Len() int {
	return len(h)
}

func (h TokensHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
}

func (h TokensHeap) Less(i, j int) bool {
	return h[i][0] < h[j][0]
}

func (h *TokensHeap) Push(x interface{}) {
	*h = append(*h, x.([]uint32))
}

func (h *TokensHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

// MergeTokens takes in input multiple lists of tokens and returns a single list
// containing all tokens merged and sorted. Each input single list is required
// to have tokens already sorted.
func MergeTokens(instances [][]uint32) []uint32 {
	numTokens := 0

	// Build the heap.
	h := make(TokensHeap, 0, len(instances))
	for _, tokens := range instances {
		if len(tokens) == 0 {
			continue
		}

		// We can safely append the input slice because elements inside are never shuffled.
		h = append(h, tokens)
		numTokens += len(tokens)
	}
	heap.Init(&h)

	out := make([]uint32, 0, numTokens)

	for h.Len() > 0 {
		// The minimum element in the tree is the root, at index 0.
		lowest := h[0]
		out = append(out, lowest[0])

		if len(lowest) > 1 {
			// Remove the first token from the lowest because we popped it
			// and then fix the heap to keep it sorted.
			h[0] = h[0][1:]
			heap.Fix(&h, 0)
		} else {
			heap.Remove(&h, 0)
		}
	}

	return out
}

// MergeTokensByZone is like MergeTokens but does it for each input zone.
func MergeTokensByZone(zones map[string][][]uint32) map[string][]uint32 {
	out := make(map[string][]uint32, len(zones))
	for zone, tokens := range zones {
		out[zone] = MergeTokens(tokens)
	}
	return out
}
