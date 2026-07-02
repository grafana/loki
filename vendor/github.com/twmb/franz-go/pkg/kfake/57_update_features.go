package kfake

import (
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// UpdateFeatures: v0-2
//
// KIP-584: mutate finalized feature levels. kfake stores levels per
// Cluster and ApiVersions reports them; changing a level does not
// alter request behavior beyond what ApiVersions advertises.
//
// Behavior:
// * Unknown features => FEATURE_UPDATE_FAILED (per feature)
// * MaxVersionLevel above kfake's supported max => FEATURE_UPDATE_FAILED
// * MaxVersionLevel < 1 deletes the feature (returns it to level 0)
// * Downgrades require AllowDowngrade (v0) or UpgradeType != Upgrade (v1+)
// * ValidateOnly (v1+) returns what would happen without mutating state
// * v2 drops the per-feature Results list (errors go to top-level)
//
// Version notes:
// * v1: UpgradeType replaces AllowDowngrade, ValidateOnly
// * v2: Results removed (errors reported at top-level only)

func init() { regKey(57, 0, 2) }

type featureSpec struct {
	minSupported int16
	maxSupported int16
}

// kfakeFeatureSpecs is the set of features ApiVersions advertises plus
// their supported ranges. Keys must match the names in 18_api_versions.go.
var kfakeFeatureSpecs = map[string]featureSpec{
	"transaction.version": {0, 2},
	"group.version":       {0, 1},
	"share.version":       {0, 1},
}

func defaultFinalizedFeatures() map[string]int16 {
	m := make(map[string]int16, len(kfakeFeatureSpecs))
	for name, spec := range kfakeFeatureSpecs {
		m[name] = spec.maxSupported
	}
	return m
}

func (c *Cluster) handleUpdateFeatures(creq *clientReq) (kmsg.Response, error) {
	req := creq.kreq.(*kmsg.UpdateFeaturesRequest)
	resp := req.ResponseKind().(*kmsg.UpdateFeaturesResponse)

	if err := c.checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	if !c.allowedClusterACL(creq, kmsg.ACLOperationAlter) {
		resp.ErrorCode = kerr.ClusterAuthorizationFailed.Code
		return resp, nil
	}

	// Build per-feature results, then collapse into the top-level error
	// for v2 (which no longer carries per-feature Results).
	results := make([]kmsg.UpdateFeaturesResponseResult, 0, len(req.FeatureUpdates))
	pending := make(map[string]int16, len(req.FeatureUpdates))
	seen := make(map[string]bool, len(req.FeatureUpdates))

	fail := func(name string, err *kerr.Error, msg string) {
		r := kmsg.NewUpdateFeaturesResponseResult()
		r.Feature = name
		r.ErrorCode = err.Code
		if msg != "" {
			r.ErrorMessage = kmsg.StringPtr(msg)
		}
		results = append(results, r)
	}
	ok := func(name string) {
		r := kmsg.NewUpdateFeaturesResponseResult()
		r.Feature = name
		results = append(results, r)
	}

	for _, fu := range req.FeatureUpdates {
		if seen[fu.Feature] {
			fail(fu.Feature, kerr.InvalidRequest, "duplicate feature update")
			continue
		}
		seen[fu.Feature] = true

		spec, known := kfakeFeatureSpecs[fu.Feature]
		if !known {
			fail(fu.Feature, kerr.FeatureUpdateFailed, "unknown feature")
			continue
		}

		newLevel := fu.MaxVersionLevel
		deletion := newLevel < 1
		if deletion {
			newLevel = 0
		} else if newLevel > spec.maxSupported {
			fail(fu.Feature, kerr.FeatureUpdateFailed, "level above supported max")
			continue
		}

		curLevel := c.features[fu.Feature]
		downgrade := newLevel < curLevel

		allowDowngrade := fu.AllowDowngrade
		if req.Version >= 1 {
			// UpgradeType: 1=upgrade only, 2=safe downgrade, 3=unsafe downgrade
			allowDowngrade = fu.UpgradeType >= 2
		}
		if downgrade && !allowDowngrade {
			fail(fu.Feature, kerr.InvalidUpdateVersion, "downgrade not allowed")
			continue
		}

		pending[fu.Feature] = newLevel
		ok(fu.Feature)
	}

	validateOnly := req.Version >= 1 && req.ValidateOnly
	if !validateOnly {
		for name, level := range pending {
			c.features[name] = level
		}
	}

	// v2 dropped the per-feature Results list. Promote the first error
	// (if any) to the top-level code to preserve the failure signal.
	if req.Version >= 2 {
		for _, r := range results {
			if r.ErrorCode != 0 {
				resp.ErrorCode = r.ErrorCode
				resp.ErrorMessage = r.ErrorMessage
				break
			}
		}
	} else {
		resp.Results = results
	}

	return resp, nil
}
