package kfake

import (
	"fmt"
	"slices"
	"sort"
	"sync"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
	"github.com/twmb/franz-go/pkg/kversion"
)

// ApiVersions: v0-5
//
// Behavior:
// * Returns all registered API keys and their version ranges
// * Advertises the kversion feature table for the cluster's version (KIP-584)
// * Auto-downgrades to v0 response on unknown version
// * v5+: REBOOTSTRAP_REQUIRED if the client names a cluster or node that is
//   not us (KIP-1242)
//
// Version notes:
// * v1: ThrottleMillis
// * v3: ClientSoftwareName, ClientSoftwareVersion, flexible versions
// * v3+: FinalizedFeatures, SupportedFeatures (KIP-584)
// * v5: ClusterID, NodeID (KIP-1242)

func init() { regKey(18, 0, 5) }

func (c *Cluster) handleApiVersions(creq *clientReq) (kmsg.Response, error) {
	req := creq.kreq.(*kmsg.ApiVersionsRequest)
	resp := req.ResponseKind().(*kmsg.ApiVersionsResponse)

	// A version above what we serve is answered the way a real broker
	// answers it (KIP-511): a v0 response carrying UNSUPPORTED_VERSION and
	// only our ApiVersions range, so that the client retries at that
	// version and still learns our features. A cluster capped below the
	// first version that had ApiVersions still answers v0.
	if maxVersion := max(c.maxVersion(18), 0); resp.Version > maxVersion {
		resp.Version = 0
		resp.ErrorCode = kerr.UnsupportedVersion.Code
		key := kmsg.NewApiVersionsResponseApiKey()
		key.ApiKey = 18
		key.MinVersion = apiVersionsKeys[18].MinVersion
		key.MaxVersion = maxVersion
		resp.ApiKeys = append(resp.ApiKeys, key)
		return resp, nil
	}

	// v3+ carries the client software name and version; a real broker
	// validates both against [a-zA-Z0-9](?:[a-zA-Z0-9\-.]*[a-zA-Z0-9])?
	// and answers INVALID_REQUEST on a mismatch. kgo validates at
	// NewClient, but raw kmsg users may not; without this, kfake accepted
	// values every real broker rejects.
	if resp.ErrorCode == 0 && req.Version >= 3 &&
		(!validSoftwareNameVersion(req.ClientSoftwareName) || !validSoftwareNameVersion(req.ClientSoftwareVersion)) {
		resp.ErrorCode = kerr.InvalidRequest.Code
		return resp, nil
	}

	// v5+ names the cluster and node the client expects to have reached
	// (KIP-1242). Both are set or neither is, else INVALID_REQUEST; if
	// either is not us, REBOOTSTRAP_REQUIRED tells the client its metadata
	// routed it to the wrong broker and it should start over from its
	// seeds.
	if resp.ErrorCode == 0 && req.Version >= 5 {
		hasCluster, hasNode := req.ClusterID != nil, req.NodeID != -1
		switch {
		case hasCluster != hasNode:
			resp.ErrorCode = kerr.InvalidRequest.Code
			return resp, nil
		case hasCluster && (*req.ClusterID != c.cfg.clusterID || req.NodeID != creq.cc.b.node):
			resp.ErrorCode = kerr.RebootstrapRequired.Code
			return resp, nil
		}
	}

	// We do not checkReqVersion for ApiVersions; if the client uses a
	// version larger than we support, we auto-downgrade.

	// If we are handling ApiVersions, our package is initialized and we
	// build our response once.
	apiVersionsOnce.Do(func() {
		for _, v := range apiVersionsKeys {
			apiVersionsSorted = append(apiVersionsSorted, v)
		}
		sort.Slice(apiVersionsSorted, func(i, j int) bool {
			return apiVersionsSorted[i].ApiKey < apiVersionsSorted[j].ApiKey
		})
	})

	// If maxVersions is configured, we need to cap the versions we
	// advertise. We build a new slice with capped versions.
	if c.cfg.maxVersions != nil {
		capped := make([]kmsg.ApiVersionsResponseApiKey, 0, len(apiVersionsSorted))
		for _, v := range apiVersionsSorted {
			cfgMax, ok := c.cfg.maxVersions.LookupMaxKeyVersion(v.ApiKey)
			if !ok {
				continue // key not in configured versions, don't advertise it
			}
			if cfgMax < v.MaxVersion {
				v.MaxVersion = cfgMax
			}
			capped = append(capped, v)
		}
		resp.ApiKeys = capped
	} else {
		// Clone: the response is handed to ControlKey interceptors,
		// which may mutate it. Handing out the shared package-global
		// slice would let one interceptor corrupt every later
		// ApiVersions response process-wide.
		resp.ApiKeys = slices.Clone(apiVersionsSorted)
	}

	// Features come from the kversion table for the cluster's version:
	// what a broker of that release supports, and what a cluster it
	// formats starts at, with any level UpdateFeatures set on top. A real
	// broker lists a finalized feature only above level 0, with min and
	// max both at the level.
	vs := c.featureVersions()
	vs.EachSupportedFeature(func(name string, min, max int16) {
		sf := kmsg.NewApiVersionsResponseSupportedFeature()
		sf.Name = name
		sf.MinVersion = min
		sf.MaxVersion = max
		resp.SupportedFeatures = append(resp.SupportedFeatures, sf)
	})
	vs.EachFinalizedFeature(func(name string, level int16) {
		if set, ok := c.features[name]; ok {
			level = set
		}
		if level == 0 {
			return
		}
		ff := kmsg.NewApiVersionsResponseFinalizedFeature()
		ff.Name = name
		ff.MinVersionLevel = level
		ff.MaxVersionLevel = level
		resp.FinalizedFeatures = append(resp.FinalizedFeatures, ff)
	})
	if len(resp.FinalizedFeatures) > 0 {
		resp.FinalizedFeaturesEpoch = 1
	}

	return resp, nil
}

// Called at the beginning of every request, this validates that the client
// is sending requests within version ranges we can handle.
func (c *Cluster) checkReqVersion(key, version int16) error {
	v, exists := apiVersionsKeys[key]
	if !exists {
		return fmt.Errorf("unsupported request key %d", key)
	}
	maxVersion := v.MaxVersion
	if c.cfg.maxVersions != nil {
		cfgMax, ok := c.cfg.maxVersions.LookupMaxKeyVersion(key)
		if !ok {
			return fmt.Errorf("unsupported request key %d (not in configured max versions)", key)
		}
		if cfgMax < maxVersion {
			maxVersion = cfgMax
		}
	}
	if version < v.MinVersion {
		return fmt.Errorf("%s version %d below min supported version %d", kmsg.NameForKey(key), version, v.MinVersion)
	}
	if version > maxVersion {
		return fmt.Errorf("%s version %d above max supported version %d", kmsg.NameForKey(key), version, maxVersion)
	}
	return nil
}

// featureVersions is the version the cluster answers features from:
// MaxVersions when configured, else Stable.
func (c *Cluster) featureVersions() *kversion.Versions {
	if c.cfg.maxVersions != nil {
		return c.cfg.maxVersions
	}
	return stableVersions()
}

var stableVersions = sync.OnceValue(kversion.Stable)

// maxVersion returns the max version we advertise for a request key, or -1 if
// we do not advertise the key at all.
func (c *Cluster) maxVersion(key int16) int16 {
	v, exists := apiVersionsKeys[key]
	if !exists {
		return -1
	}
	if c.cfg.maxVersions == nil {
		return v.MaxVersion
	}
	cfgMax, ok := c.cfg.maxVersions.LookupMaxKeyVersion(key)
	if !ok {
		return -1
	}
	return min(cfgMax, v.MaxVersion)
}

// rejectsNeverWrittenNonzeroSeq returns whether we model KAFKA-15591
// (apache/kafka pull request 23234), which ships in Kafka 4.5. If a
// partition's log has never held a record, we accept only a first sequence of
// zero from a producer we have no state for.
//
// kversion has no 4.5 table yet, so no API version separates 4.5 from 4.2. An
// uncapped cluster is how we model the newest broker, so it applies the check.
// A cluster capped with MaxVersions is a released version that lacks the
// check, so it does not. Once kversion gains the 4.5 API versions, gate on one
// of them the way the 2.5 check gates on InitProducerID v3.
func (c *Cluster) rejectsNeverWrittenNonzeroSeq() bool {
	return c.cfg.maxVersions == nil
}

var (
	apiVersionsMu   sync.Mutex
	apiVersionsKeys = make(map[int16]kmsg.ApiVersionsResponseApiKey)

	apiVersionsOnce   sync.Once
	apiVersionsSorted []kmsg.ApiVersionsResponseApiKey
)

// Every request we implement calls regKey in an init function, allowing us to
// fully correctly build our ApiVersions response.
func regKey(key, min, max int16) {
	apiVersionsMu.Lock()
	defer apiVersionsMu.Unlock()

	if key < 0 || min < 0 || max < 0 || max < min {
		panic(fmt.Sprintf("invalid registration, key: %d, min: %d, max: %d", key, min, max))
	}
	if _, exists := apiVersionsKeys[key]; exists {
		panic(fmt.Sprintf("doubly registered key %d", key))
	}
	apiVersionsKeys[key] = kmsg.ApiVersionsResponseApiKey{
		ApiKey:     key,
		MinVersion: min,
		MaxVersion: max,
	}
}

// validSoftwareNameVersion mirrors the broker's ApiVersions validation
// pattern: non-empty alphanumeric ends with dashes and dots allowed
// internally.
func validSoftwareNameVersion(s string) bool {
	alnum := func(c byte) bool {
		return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
	}
	if len(s) == 0 || !alnum(s[0]) || !alnum(s[len(s)-1]) {
		return false
	}
	for i := 1; i < len(s)-1; i++ {
		if c := s[i]; !alnum(c) && c != '-' && c != '.' {
			return false
		}
	}
	return true
}
