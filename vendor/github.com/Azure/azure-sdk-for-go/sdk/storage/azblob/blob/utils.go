// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License. See License.txt in the project root for license information.

package blob

import (
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/internal/generated"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/sas"
)

// ObjectReplicationRules struct
type ObjectReplicationRules struct {
	RuleID string
	Status string
}

// ObjectReplicationPolicy are deserialized attributes.
type ObjectReplicationPolicy struct {
	PolicyID *string
	Rules    *[]ObjectReplicationRules
}

func convertDownloadResponse(dr generated.BlobClientDownloadResponseInternal) DownloadResponse {
	return DownloadResponse{
		AcceptRanges:                dr.AcceptRanges,
		AccessTier:                  dr.AccessTier,
		AccessTierChangeTime:        dr.AccessTierChangeTime,
		BlobCommittedBlockCount:     dr.BlobCommittedBlockCount,
		BlobContentMD5:              dr.BlobContentMD5,
		BlobSequenceNumber:          dr.BlobSequenceNumber,
		BlobType:                    dr.BlobType,
		Body:                        dr.Body,
		CacheControl:                dr.CacheControl,
		ClientRequestID:             dr.ClientRequestID,
		ContentCRC64:                dr.ContentCRC64,
		ContentDisposition:          dr.ContentDisposition,
		ContentEncoding:             dr.ContentEncoding,
		ContentLanguage:             dr.ContentLanguage,
		ContentLength:               dr.ContentLength,
		ContentMD5:                  dr.ContentMD5,
		ContentRange:                dr.ContentRange,
		ContentType:                 dr.ContentType,
		CopyCompletionTime:          dr.CopyCompletionTime,
		CopyID:                      dr.CopyID,
		CopyProgress:                dr.CopyProgress,
		CopySource:                  dr.CopySource,
		CopyStatus:                  dr.CopyStatus,
		CopyStatusDescription:       dr.CopyStatusDescription,
		CreationTime:                dr.CreationTime,
		Date:                        dr.Date,
		ETag:                        dr.ETag,
		EncryptionKeySHA256:         dr.EncryptionKeySHA256,
		EncryptionScope:             dr.EncryptionScope,
		ImmutabilityPolicyExpiresOn: dr.ImmutabilityPolicyExpiresOn,
		ImmutabilityPolicyMode:      dr.ImmutabilityPolicyMode,
		IsCurrentVersion:            dr.IsCurrentVersion,
		IsSealed:                    dr.IsSealed,
		IsServerEncrypted:           dr.IsServerEncrypted,
		LastAccessed:                dr.LastAccessed,
		LeaseDuration:               dr.LeaseDuration,
		LastModified:                dr.LastModified,
		LeaseState:                  dr.LeaseState,
		LeaseStatus:                 dr.LeaseStatus,
		LegalHold:                   dr.LegalHold,
		Metadata:                    dr.Metadata,
		ObjectReplicationPolicyID:   dr.ObjectReplicationPolicyID,
		ObjectReplicationRules:      dr.ObjectReplicationRules,
		RequestID:                   dr.RequestID,
		StructuredBodyType:          dr.StructuredBodyType,
		StructuredContentLength:     dr.StructuredContentLength,
		TagCount:                    dr.TagCount,
		Version:                     dr.Version,
		VersionID:                   dr.VersionID,
	}
}

// deserializeORSPolicies is utility function to deserialize ORS Policies.
func deserializeORSPolicies(policies map[string]*string) (objectReplicationPolicies []ObjectReplicationPolicy) {
	if policies == nil {
		return nil
	}
	// For source blobs (blobs that have policy ids and rule ids applied to them),
	// the header will be formatted as "x-ms-or-<policy_id>_<rule_id>: {Complete, Failed}".
	// The value of this header is the status of the replication.
	orPolicyStatusHeader := make(map[string]*string)
	for key, value := range policies {
		if strings.Contains(key, "or-") && key != "x-ms-or-policy-id" {
			orPolicyStatusHeader[key] = value
		}
	}

	parsedResult := make(map[string][]ObjectReplicationRules)
	for key, value := range orPolicyStatusHeader {
		policyAndRuleIDs := strings.Split(strings.Split(key, "or-")[1], "_")
		policyId, ruleId := policyAndRuleIDs[0], policyAndRuleIDs[1]

		parsedResult[policyId] = append(parsedResult[policyId], ObjectReplicationRules{RuleID: ruleId, Status: *value})
	}

	for policyId, rules := range parsedResult {
		objectReplicationPolicies = append(objectReplicationPolicies, ObjectReplicationPolicy{
			PolicyID: &policyId,
			Rules:    &rules,
		})
	}
	return
}

// ParseHTTPHeaders parses GetPropertiesResponse and returns HTTPHeaders.
func ParseHTTPHeaders(resp GetPropertiesResponse) HTTPHeaders {
	return HTTPHeaders{
		BlobContentType:        resp.ContentType,
		BlobContentEncoding:    resp.ContentEncoding,
		BlobContentLanguage:    resp.ContentLanguage,
		BlobContentDisposition: resp.ContentDisposition,
		BlobCacheControl:       resp.CacheControl,
		BlobContentMD5:         resp.ContentMD5,
	}
}

// URLParts object represents the components that make up an Azure Storage Container/Blob URL.
// NOTE: Changing any SAS-related field requires computing a new SAS signature.
type URLParts = sas.URLParts

// ParseURL parses a URL initializing URLParts' fields including any SAS-related & snapshot query parameters. Any other
// query parameters remain in the UnparsedParams field. This method overwrites all fields in the URLParts object.
func ParseURL(u string) (URLParts, error) {
	return sas.ParseURL(u)
}
