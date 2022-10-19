//go:build go1.18
// +build go1.18

// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package azblob

import (
	"net/url"
	"strings"
)

func serializeBlobTagsToStrPtr(tagsMap map[string]string) *string {
	if tagsMap == nil {
		return nil
	}
	tags := make([]string, 0)
	for key, val := range tagsMap {
		tags = append(tags, url.QueryEscape(key)+"="+url.QueryEscape(val))
	}
	//tags = tags[:len(tags)-1]
	blobTagsString := strings.Join(tags, "&")
	return &blobTagsString
}

func serializeBlobTags(tagsMap map[string]string) *BlobTags {
	if tagsMap == nil {
		return nil
	}
	blobTagSet := make([]*BlobTag, 0)
	for key, val := range tagsMap {
		newKey, newVal := key, val
		blobTagSet = append(blobTagSet, &BlobTag{Key: &newKey, Value: &newVal})
	}
	return &BlobTags{BlobTagSet: blobTagSet}
}

func deserializeORSPolicies(policies map[string]string) (objectReplicationPolicies []ObjectReplicationPolicy) {
	if policies == nil {
		return nil
	}
	// For source blobs (blobs that have policy ids and rule ids applied to them),
	// the header will be formatted as "x-ms-or-<policy_id>_<rule_id>: {Complete, Failed}".
	// The value of this header is the status of the replication.
	orPolicyStatusHeader := make(map[string]string)
	for key, value := range policies {
		if strings.Contains(key, "or-") && key != "x-ms-or-policy-id" {
			orPolicyStatusHeader[key] = value
		}
	}

	parsedResult := make(map[string][]ObjectReplicationRules)
	for key, value := range orPolicyStatusHeader {
		policyAndRuleIDs := strings.Split(strings.Split(key, "or-")[1], "_")
		policyId, ruleId := policyAndRuleIDs[0], policyAndRuleIDs[1]

		parsedResult[policyId] = append(parsedResult[policyId], ObjectReplicationRules{RuleId: ruleId, Status: value})
	}

	for policyId, rules := range parsedResult {
		objectReplicationPolicies = append(objectReplicationPolicies, ObjectReplicationPolicy{
			PolicyId: &policyId,
			Rules:    &rules,
		})
	}
	return
}
