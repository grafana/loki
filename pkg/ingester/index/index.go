// originally from https://github.com/cortexproject/cortex/blob/868898a2921c662dcd4f90683e8b95c927a8edd8/pkg/ingester/index/index.go
// but modified to support sharding queries.
package index

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"unsafe"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/storage/stores/series"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

const DefaultIndexShards = 32

var ErrInvalidShardQuery = errors.New("incompatible index shard query")

type Interface interface {
	Add(labels []logproto.LabelAdapter, fp model.Fingerprint) labels.Labels
	Lookup(matchers []*labels.Matcher, shard *logql.Shard) ([]model.Fingerprint, error)
	LabelNames(shard *logql.Shard) ([]string, error)
	LabelValues(name string, shard *logql.Shard) ([]string, error)
	Delete(labels labels.Labels, fp model.Fingerprint)
}

// InvertedIndex implements a in-memory inverted index from label pairs to fingerprints.
// It is sharded to reduce lock contention on writes.
type InvertedIndex struct {
	totalShards uint32
	shards      []*indexShard
}

func NewWithShards(totalShards uint32) *InvertedIndex {
	shards := make([]*indexShard, totalShards)
	for i := uint32(0); i < totalShards; i++ {
		shards[i] = &indexShard{
			idx:   map[string]indexEntry{},
			shard: i,
		}
	}
	return &InvertedIndex{
		totalShards: totalShards,
		shards:      shards,
	}
}

func (ii *InvertedIndex) getShards(shard *index.ShardAnnotation) []*indexShard {
	if shard == nil {
		return ii.shards
	}

	totalRequested := ii.totalShards / shard.Of
	result := make([]*indexShard, totalRequested)
	var j int
	for i := uint32(0); i < totalRequested; i++ {
		subShard := ((shard.Shard) + (i * shard.Of))
		result[j] = ii.shards[subShard]
		j++
	}
	return result
}

func (ii *InvertedIndex) validateShard(shard *logql.Shard) (*index.ShardAnnotation, error) {
	if shard == nil {
		return nil, nil
	}

	s := shard.PowerOfTwo
	if s == nil {
		return nil, errors.New("inverted index only supports shard annotations with `PowerOfTwo`")
	}

	if ii.totalShards%s.Of != 0 || s.Of > ii.totalShards {
		return nil, fmt.Errorf("%w index_shard:%d query_shard:%v", ErrInvalidShardQuery, ii.totalShards, s)
	}
	return s, nil
}

// Add a fingerprint under the specified labels.
// NOTE: memory for `labels` is unsafe; anything retained beyond the
// life of this function must be copied
func (ii *InvertedIndex) Add(labels []logproto.LabelAdapter, fp model.Fingerprint) labels.Labels {
	shardIndex := labelsSeriesIDHash(logproto.FromLabelAdaptersToLabels(labels))
	shard := ii.shards[shardIndex%ii.totalShards]
	return shard.add(labels, fp) // add() returns 'interned' values so the original labels are not retained
}

var (
	bufferPool = sync.Pool{
		New: func() interface{} {
			return bytes.NewBuffer(make([]byte, 0, 1000))
		},
	}
	base64Pool = sync.Pool{
		New: func() interface{} {
			return bytes.NewBuffer(make([]byte, 0, base64.RawStdEncoding.EncodedLen(sha256.Size)))
		},
	}
)

func labelsSeriesIDHash(ls labels.Labels) uint32 {
	b64 := base64Pool.Get().(*bytes.Buffer)
	defer func() {
		base64Pool.Put(b64)
	}()
	buf := b64.Bytes()[:b64.Cap()]
	labelsSeriesID(ls, buf)
	return binary.BigEndian.Uint32(buf)
}

func labelsSeriesID(ls labels.Labels, dest []byte) {
	buf := bufferPool.Get().(*bytes.Buffer)
	defer func() {
		buf.Reset()
		bufferPool.Put(buf)
	}()
	labelsString(buf, ls)
	h := sha256.Sum256(buf.Bytes())
	dest = dest[:base64.RawStdEncoding.EncodedLen(len(h))]
	base64.RawStdEncoding.Encode(dest, h[:])
}

// Backwards-compatible with model.Metric.String()
func labelsString(b *bytes.Buffer, ls labels.Labels) {
	// metrics name is used in the store for computing shards.
	// see chunk/schema_util.go for more details. `labelsString()`
	b.WriteString("logs")
	b.WriteByte('{')
	i := 0
	for _, l := range ls {
		if l.Name == labels.MetricName {
			continue
		}
		if i > 0 {
			b.WriteByte(',')
			b.WriteByte(' ')
		}
		b.WriteString(l.Name)
		b.WriteByte('=')
		var buf [1000]byte
		b.Write(strconv.AppendQuote(buf[:0], l.Value))
		i++
	}
	b.WriteByte('}')
}

// Lookup all fingerprints for the provided matchers.
func (ii *InvertedIndex) Lookup(matchers []*labels.Matcher, s *logql.Shard) ([]model.Fingerprint, error) {
	shard, err := ii.validateShard(s)
	if err != nil {
		return nil, err
	}

	var result []model.Fingerprint
	shards := ii.getShards(shard)

	// if no matcher is specified, all fingerprints would be returned
	if len(matchers) == 0 {
		for i := range shards {
			fps := shards[i].allFPs()
			result = append(result, fps...)
		}
		return result, nil
	}

	for i := range shards {
		fps := shards[i].lookup(matchers)
		result = append(result, fps...)
	}
	return result, nil
}

// LabelNames returns all label names.
func (ii *InvertedIndex) LabelNames(s *logql.Shard) ([]string, error) {
	shard, err := ii.validateShard(s)
	if err != nil {
		return nil, err
	}
	shards := ii.getShards(shard)
	results := make([][]string, 0, len(shards))
	for i := range shards {
		shardResult := shards[i].labelNames(nil)
		results = append(results, shardResult)
	}

	return mergeStringSlices(results), nil
}

// LabelValues returns the values for the given label.
func (ii *InvertedIndex) LabelValues(name string, s *logql.Shard) ([]string, error) {
	shard, err := ii.validateShard(s)
	if err != nil {
		return nil, err
	}
	shards := ii.getShards(shard)
	results := make([][]string, 0, len(shards))

	for i := range shards {
		shardResult := shards[i].labelValues(name, nil)
		results = append(results, shardResult)
	}

	return mergeStringSlices(results), nil
}

// Delete a fingerprint with the given label pairs.
func (ii *InvertedIndex) Delete(labels labels.Labels, fp model.Fingerprint) {
	shard := ii.shards[labelsSeriesIDHash(labels)%ii.totalShards]
	shard.delete(labels, fp)
}

// NB slice entries are sorted in fp order.
type indexEntry struct {
	name string
	fps  map[string]indexValueEntry
}

type indexValueEntry struct {
	value string
	fps   []model.Fingerprint
}

type unlockIndex map[string]indexEntry

// This is the prevalent value for Intel and AMD CPUs as-at 2018.
const cacheLineSize = 64

// Roughly
// map[labelName] => map[labelValue] => []fingerprint
type indexShard struct {
	shard uint32
	mtx   sync.RWMutex
	idx   unlockIndex
	//nolint:structcheck,unused
	pad [cacheLineSize - unsafe.Sizeof(sync.Mutex{}) - unsafe.Sizeof(unlockIndex{})]byte
}

func copyString(s string) string {
	return string([]byte(s))
}

// add metric to the index; return all the name/value pairs as a fresh
// sorted slice, referencing 'interned' strings from the index so that
// no references are retained to the memory of `metric`.
func (shard *indexShard) add(metric []logproto.LabelAdapter, fp model.Fingerprint) labels.Labels {
	shard.mtx.Lock()
	defer shard.mtx.Unlock()

	internedLabels := make(labels.Labels, len(metric))

	for i, pair := range metric {
		values, ok := shard.idx[pair.Name]
		if !ok {
			values = indexEntry{
				name: copyString(pair.Name),
				fps:  map[string]indexValueEntry{},
			}
			shard.idx[values.name] = values
		}
		fingerprints, ok := values.fps[pair.Value]
		if !ok {
			fingerprints = indexValueEntry{
				value: copyString(pair.Value),
			}
		}
		// Insert into the right position to keep fingerprints sorted
		j := sort.Search(len(fingerprints.fps), func(i int) bool {
			return fingerprints.fps[i] >= fp
		})
		fingerprints.fps = append(fingerprints.fps, 0)
		copy(fingerprints.fps[j+1:], fingerprints.fps[j:])
		fingerprints.fps[j] = fp
		values.fps[fingerprints.value] = fingerprints
		internedLabels[i] = labels.Label{Name: values.name, Value: fingerprints.value}
	}
	sort.Sort(internedLabels)
	return internedLabels
}

func (shard *indexShard) lookup(matchers []*labels.Matcher) []model.Fingerprint {
	// index slice values must only be accessed under lock, so all
	// code paths must take a copy before returning
	shard.mtx.RLock()
	defer shard.mtx.RUnlock()

	// per-shard intersection is initially nil, which is a special case
	// meaning "everything" when passed to intersect()
	// loop invariant: result is sorted
	var result []model.Fingerprint
	for _, matcher := range matchers {
		values, ok := shard.idx[matcher.Name]
		if !ok {
			return nil
		}
		var toIntersect model.Fingerprints
		if matcher.Type == labels.MatchEqual {
			fps := values.fps[matcher.Value]
			toIntersect = append(toIntersect, fps.fps...) // deliberate copy
		} else if matcher.Type == labels.MatchRegexp && len(series.FindSetMatches(matcher.Value)) > 0 {
			// The lookup is of the form `=~"a|b|c|d"`
			set := series.FindSetMatches(matcher.Value)
			for _, value := range set {
				toIntersect = append(toIntersect, values.fps[value].fps...)
			}
			sort.Sort(toIntersect)
		} else {
			// accumulate the matching fingerprints (which are all distinct)
			// then sort to maintain the invariant
			for value, fps := range values.fps {
				if matcher.Matches(value) {
					toIntersect = append(toIntersect, fps.fps...)
				}
			}
			sort.Sort(toIntersect)
		}
		result = intersect(result, toIntersect)
		if len(result) == 0 {
			return nil
		}
	}

	return result
}

func (shard *indexShard) allFPs() model.Fingerprints {
	shard.mtx.RLock()
	defer shard.mtx.RUnlock()

	var fps model.Fingerprints
	for _, ie := range shard.idx {
		for _, ive := range ie.fps {
			fps = append(fps, ive.fps...)
		}
	}
	if len(fps) == 0 {
		return nil
	}

	var result model.Fingerprints
	m := map[model.Fingerprint]struct{}{}
	for _, fp := range fps {
		if _, ok := m[fp]; !ok {
			m[fp] = struct{}{}
			result = append(result, fp)
		}
	}
	return result
}

func (shard *indexShard) labelNames(extractor func(unlockIndex) []string) []string {
	shard.mtx.RLock()
	defer shard.mtx.RUnlock()

	results := make([]string, 0, len(shard.idx))
	if extractor != nil {
		results = append(results, extractor(shard.idx)...)
	} else {
		for name := range shard.idx {
			results = append(results, name)
		}
	}

	sort.Strings(results)
	return results
}

func (shard *indexShard) labelValues(
	name string,
	extractor func(indexEntry) []string,
) []string {
	shard.mtx.RLock()
	defer shard.mtx.RUnlock()

	values, ok := shard.idx[name]
	if !ok {
		return nil
	}

	if extractor == nil {
		results := make([]string, 0, len(values.fps))
		for val := range values.fps {
			results = append(results, val)
		}
		sort.Strings(results)
		return results
	}

	return extractor(values)
}

func (shard *indexShard) delete(labels labels.Labels, fp model.Fingerprint) {
	shard.mtx.Lock()
	defer shard.mtx.Unlock()

	for _, pair := range labels {
		name, value := pair.Name, pair.Value
		values, ok := shard.idx[name]
		if !ok {
			continue
		}
		fingerprints, ok := values.fps[value]
		if !ok {
			continue
		}

		j := sort.Search(len(fingerprints.fps), func(i int) bool {
			return fingerprints.fps[i] >= fp
		})

		// see if search didn't find fp which matches the condition which means we don't have to do anything.
		if j >= len(fingerprints.fps) || fingerprints.fps[j] != fp {
			continue
		}
		fingerprints.fps = fingerprints.fps[:j+copy(fingerprints.fps[j:], fingerprints.fps[j+1:])]

		if len(fingerprints.fps) == 0 {
			delete(values.fps, value)
		} else {
			values.fps[value] = fingerprints
		}

		if len(values.fps) == 0 {
			delete(shard.idx, name)
		} else {
			shard.idx[name] = values
		}
	}
}

// intersect two sorted lists of fingerprints.  Assumes there are no duplicate
// fingerprints within the input lists.
func intersect(a, b []model.Fingerprint) []model.Fingerprint {
	if a == nil {
		return b
	}
	result := []model.Fingerprint{}
	for i, j := 0, 0; i < len(a) && j < len(b); {
		if a[i] == b[j] {
			result = append(result, a[i])
		}
		if a[i] < b[j] {
			i++
		} else {
			j++
		}
	}
	return result
}

func mergeStringSlices(ss [][]string) []string {
	switch len(ss) {
	case 0:
		return nil
	case 1:
		return ss[0]
	case 2:
		return mergeTwoStringSlices(ss[0], ss[1])
	default:
		halfway := len(ss) / 2
		return mergeTwoStringSlices(
			mergeStringSlices(ss[:halfway]),
			mergeStringSlices(ss[halfway:]),
		)
	}
}

func mergeTwoStringSlices(a, b []string) []string {
	result := make([]string, 0, len(a)+len(b))
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		if a[i] < b[j] {
			result = append(result, a[i])
			i++
		} else if a[i] > b[j] {
			result = append(result, b[j])
			j++
		} else {
			result = append(result, a[i])
			i++
			j++
		}
	}
	result = append(result, a[i:]...)
	result = append(result, b[j:]...)
	return result
}
