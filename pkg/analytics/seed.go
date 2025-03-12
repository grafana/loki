package analytics

import (
	"fmt"
	"time"

	jsoniter "github.com/json-iterator/go"
	prom "github.com/prometheus/prometheus/web/api/v1"

	"github.com/grafana/dskit/kv/memberlist"
)

// ClusterSeed is the seed for the usage stats.
// A unique ID is generated for each cluster.
type ClusterSeed struct {
	UID                    string    `json:"UID"`
	CreatedAt              time.Time `json:"created_at"`
	prom.PrometheusVersion `json:"version"`
}

// Merge implements the memberlist.Mergeable interface.
// It allow to merge the content of two different seeds.
func (c *ClusterSeed) Merge(mergeable memberlist.Mergeable, _ bool) (change memberlist.Mergeable, err error) {
	if mergeable == nil {
		return nil, nil
	}
	other, ok := mergeable.(*ClusterSeed)
	if !ok {
		return nil, fmt.Errorf("expected *usagestats.ClusterSeed, got %T", mergeable)
	}
	if other == nil {
		return nil, nil
	}
	// if we already have (c) the oldest key, then should not request change.
	if c.CreatedAt.Before(other.CreatedAt) {
		return nil, nil
	}
	if c.CreatedAt == other.CreatedAt {
		// if we have the exact same creation date but the key is different
		// we take the smallest UID using string alphabetical comparison to ensure stability.
		if c.UID > other.UID {
			*c = *other
			return other, nil
		}
		return nil, nil
	}
	// if our seed is not the oldest, then we should request a change.
	*c = *other
	return other, nil
}

// MergeContent tells if the content of the two seeds are the same.
func (c *ClusterSeed) MergeContent() []string {
	return []string{c.UID}
}

// RemoveTombstones is not required for usagestats
func (c *ClusterSeed) RemoveTombstones(_ time.Time) (total, removed int) {
	return 0, 0
}

func (c *ClusterSeed) Clone() memberlist.Mergeable {
	clone := *c
	return &clone
}

var JSONCodec = jsonCodec{}

type jsonCodec struct{}

// todo we need to use the default codec for the rest of the code
// currently crashing because the in-memory kvstore use a singleton.
func (jsonCodec) Decode(data []byte) (interface{}, error) {
	var seed ClusterSeed
	if err := jsoniter.ConfigFastest.Unmarshal(data, &seed); err != nil {
		return nil, err
	}
	return &seed, nil
}

func (jsonCodec) Encode(obj interface{}) ([]byte, error) {
	return jsoniter.ConfigFastest.Marshal(obj)
}
func (jsonCodec) CodecID() string { return "usagestats.jsonCodec" }
