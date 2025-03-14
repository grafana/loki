package index

import (
	"fmt"
	"sort"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

// BitPrefixInvertedIndex is another inverted index implementation
// that uses the bit prefix sharding algorithm in tsdb/index/shard.go
// instead of a modulo approach.
// This is the standard for TSDB compatibility because
// the same series must resolve to the same shard (for each period config),
// whether it's resolved on the ingester or via the store.
type BitPrefixInvertedIndex struct {
	totalShards uint32
	shards      []*indexShard
}

func ValidateBitPrefixShardFactor(factor uint32) error {
	if requiredBits := index.NewShard(0, factor).RequiredBits(); 1<<requiredBits != factor {
		return fmt.Errorf("Incompatible inverted index shard factor on ingester: It must be a power of two, got %d", factor)
	}
	return nil
}

func NewBitPrefixWithShards(totalShards uint32) (*BitPrefixInvertedIndex, error) {
	if err := ValidateBitPrefixShardFactor(totalShards); err != nil {
		return nil, err
	}

	shards := make([]*indexShard, totalShards)
	for i := uint32(0); i < totalShards; i++ {
		shards[i] = &indexShard{
			idx:   map[string]indexEntry{},
			shard: i,
		}
	}
	return &BitPrefixInvertedIndex{
		totalShards: totalShards,
		shards:      shards,
	}, nil
}

func (ii *BitPrefixInvertedIndex) getShards(shard *logql.Shard) ([]*indexShard, bool) {
	if shard == nil {
		return ii.shards, false
	}

	// When comparing a higher shard factor to a lower inverted index shard factor
	// we must filter resulting fingerprints as the the lower shard factor in the
	// inverted index is a superset of the requested factor.
	//
	// For instance, the 3_of_4 shard factor maps to the bit prefix 0b11.
	// If the inverted index only has a factor of 2, we'll need to check the 0b1
	// prefixed shard (which contains the 0b10 and 0b11 prefixes).
	// Conversely, if the requested shard is 1_of_2, but the index has a factor of 4,
	// we can _exactly_ match ob1 => (ob10, ob11) and know all fingerprints in those
	// resulting shards have the requested ob1 prefix (don't need to filter).
	// NB(owen-d): this only applies when using the old power-of-two shards,
	// which are superseded by the new bounded sharding strategy.
	filter := true

	switch shard.Variant() {
	case logql.PowerOfTwoVersion:
		if int(shard.PowerOfTwo.Of) <= len(ii.shards) {
			filter = false
		}
	}

	minFp, maxFp := shard.GetFromThrough()

	// Determine how many bits we need to take from
	// the requested shard's min/max fingerprint values
	// in order to calculate the indices for the inverted index's
	// shard factor.
	requiredBits := index.NewShard(0, uint32(len(ii.shards))).RequiredBits()
	lowerIdx := int(minFp >> (64 - requiredBits))
	upperIdx := int(maxFp >> (64 - requiredBits))

	// If the upper bound's shard doesn't align exactly
	// with the maximum fingerprint, we must also
	// check the subsequent shard.
	// This happens in two cases:
	// 1) When requesting the last shard of any factor.
	// This accounts for zero indexing in our sharding logic
	// to successfully request `shards[start:len(shards)]`
	// 2) When requesting the _first_ shard of a larger factor
	// than the index uses. In this case, the required_bits are not
	// enough and the requested end prefix gets trimmed.
	// If confused, comment out this line and see which tests fail.
	if (upperIdx << (64 - requiredBits)) != int(maxFp) {
		upperIdx++
	}

	return ii.shards[lowerIdx:upperIdx], filter
}

func (ii *BitPrefixInvertedIndex) shardForFP(fp model.Fingerprint) int {
	localShard := index.NewShard(0, uint32(len(ii.shards)))
	return int(fp >> (64 - localShard.RequiredBits()))
}

func (ii *BitPrefixInvertedIndex) validateShard(shard *logql.Shard) error {
	if shard == nil {
		return nil
	}

	switch shard.Variant() {
	case logql.PowerOfTwoVersion:
		return shard.PowerOfTwo.Validate()
	}
	return nil

}

// Add a fingerprint under the specified labels.
// NOTE: memory for `labels` is unsafe; anything retained beyond the
// life of this function must be copied
func (ii *BitPrefixInvertedIndex) Add(labels []logproto.LabelAdapter, fp model.Fingerprint) labels.Labels {
	// add() returns 'interned' values so the original labels are not retained
	return ii.shards[ii.shardForFP(fp)].add(labels, fp)
}

// Lookup all fingerprints for the provided matchers.
func (ii *BitPrefixInvertedIndex) Lookup(matchers []*labels.Matcher, shard *logql.Shard) ([]model.Fingerprint, error) {
	if err := ii.validateShard(shard); err != nil {
		return nil, err
	}

	var result []model.Fingerprint
	shards, filter := ii.getShards(shard)

	// if no matcher is specified, all fingerprints would be returned
	if len(matchers) == 0 {
		for i := range shards {
			fps := shards[i].allFPs()
			result = append(result, fps...)
		}
	} else {
		for i := range shards {
			fps := shards[i].lookup(matchers)
			result = append(result, fps...)
		}
	}

	// Because bit prefix order is also ascending order,
	// the merged fingerprints from ascending shards are also in order.
	if filter {
		minFP, maxFP := shard.GetFromThrough()
		minIdx := sort.Search(len(result), func(i int) bool {
			return result[i] >= minFP
		})

		maxIdx := sort.Search(len(result), func(i int) bool {
			return result[i] >= maxFP
		})

		result = result[minIdx:maxIdx]
	}

	return result, nil
}

// LabelNames returns all label names.
func (ii *BitPrefixInvertedIndex) LabelNames(shard *logql.Shard) ([]string, error) {
	if err := ii.validateShard(shard); err != nil {
		return nil, err
	}

	var extractor func(unlockIndex) []string
	shards, filter := ii.getShards(shard)

	// If we need to check shard inclusion, we have to do it the expensive way :(
	// Therefore it's more performant to request shard factors lower or equal to the
	// inverted index factor
	if filter {

		extractor = func(x unlockIndex) (results []string) {

		outer:
			for name, entry := range x {
				for _, valEntry := range entry.fps {
					for _, fp := range valEntry.fps {
						if shard.Match(fp) {
							results = append(results, name)
							continue outer
						}
					}
				}
			}

			return results
		}
	}

	results := make([][]string, 0, len(shards))
	for i := range shards {
		shardResult := shards[i].labelNames(extractor)
		results = append(results, shardResult)
	}

	return mergeStringSlices(results), nil
}

// LabelValues returns the values for the given label.
func (ii *BitPrefixInvertedIndex) LabelValues(name string, shard *logql.Shard) ([]string, error) {
	if err := ii.validateShard(shard); err != nil {
		return nil, err
	}

	var extractor func(indexEntry) []string
	shards, filter := ii.getShards(shard)
	if filter {

		extractor = func(x indexEntry) []string {
			results := make([]string, 0, len(x.fps))

		outer:
			for val, valEntry := range x.fps {
				for _, fp := range valEntry.fps {
					if shard.Match(fp) {
						results = append(results, val)
						continue outer
					}
				}
			}
			return results
		}
	}
	results := make([][]string, 0, len(shards))

	for i := range shards {
		shardResult := shards[i].labelValues(name, extractor)
		results = append(results, shardResult)
	}

	return mergeStringSlices(results), nil
}

// Delete a fingerprint with the given label pairs.
func (ii *BitPrefixInvertedIndex) Delete(labels labels.Labels, fp model.Fingerprint) {
	localShard := index.NewShard(0, uint32(len(ii.shards)))
	idx := int(fp >> (64 - localShard.RequiredBits()))
	ii.shards[idx].delete(labels, fp)
}
