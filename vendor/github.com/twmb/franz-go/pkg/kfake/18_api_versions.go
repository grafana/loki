package kfake

import (
	"fmt"
	"sort"
	"sync"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// ApiVersions: v0-4
//
// Behavior:
// * Returns all registered API keys and their version ranges
// * Advertises transaction.version feature for KIP-890 support
// * Auto-downgrades to v0 response on unknown version
//
// Version notes:
// * v1: ThrottleMillis
// * v3: ClientSoftwareName, ClientSoftwareVersion, flexible versions
// * v3+: FinalizedFeatures, SupportedFeatures (KIP-584)

func init() { regKey(18, 0, 4) }

func (c *Cluster) handleApiVersions(kreq kmsg.Request) (kmsg.Response, error) {
	req := kreq.(*kmsg.ApiVersionsRequest)
	resp := req.ResponseKind().(*kmsg.ApiVersionsResponse)

	if resp.Version > 3 && resp.Version > apiVersionsKeys[18].MaxVersion {
		resp.Version = 0 // downgrades to 0 if the version is unknown
		resp.ErrorCode = kerr.UnsupportedVersion.Code
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
		resp.ApiKeys = apiVersionsSorted
	}

	// Build SupportedFeatures (what we can support) and FinalizedFeatures
	// (what is active), gated on whether the relevant API keys are available.
	produceMax := apiVersionsKeys[0].MaxVersion
	if c.cfg.maxVersions != nil {
		if cfgMax, ok := c.cfg.maxVersions.LookupMaxKeyVersion(0); ok && cfgMax < produceMax {
			produceMax = cfgMax
		}
	}
	hasTxn := produceMax >= 12
	_, hasGroup := apiVersionsKeys[68] // ConsumerGroupHeartbeat
	if hasGroup && c.cfg.maxVersions != nil {
		_, hasGroup = c.cfg.maxVersions.LookupMaxKeyVersion(68)
	}
	_, hasShare := apiVersionsKeys[76] // ShareGroupHeartbeat
	if hasShare && c.cfg.maxVersions != nil {
		_, hasShare = c.cfg.maxVersions.LookupMaxKeyVersion(76)
	}
	addFeature := func(name string) {
		spec := kfakeFeatureSpecs[name]
		sf := kmsg.NewApiVersionsResponseSupportedFeature()
		sf.Name = name
		sf.MinVersion = spec.minSupported
		sf.MaxVersion = spec.maxSupported
		resp.SupportedFeatures = append(resp.SupportedFeatures, sf)

		ff := kmsg.NewApiVersionsResponseFinalizedFeature()
		ff.Name = name
		ff.MinVersionLevel = 0
		ff.MaxVersionLevel = c.features[name]
		resp.FinalizedFeatures = append(resp.FinalizedFeatures, ff)
	}
	if hasTxn {
		addFeature("transaction.version")
	}
	if hasGroup {
		addFeature("group.version")
	}
	if hasShare {
		addFeature("share.version")
	}
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
