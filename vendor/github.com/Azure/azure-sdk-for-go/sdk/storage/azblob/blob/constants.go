//go:build go1.18
// +build go1.18

// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License. See License.txt in the project root for license information.

package blob

import (
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/internal/exported"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/internal/generated"
)

const (
	CountToEnd = 0

	SnapshotTimeFormat = exported.SnapshotTimeFormat

	// DefaultDownloadBlockSize is default block size
	DefaultDownloadBlockSize = int64(4 * 1024 * 1024) // 4MB
)

// BlobType defines values for BlobType
type BlobType = generated.BlobType

const (
	BlobTypeBlockBlob  BlobType = generated.BlobTypeBlockBlob
	BlobTypePageBlob   BlobType = generated.BlobTypePageBlob
	BlobTypeAppendBlob BlobType = generated.BlobTypeAppendBlob
)

// PossibleBlobTypeValues returns the possible values for the BlobType const type.
func PossibleBlobTypeValues() []BlobType {
	return generated.PossibleBlobTypeValues()
}

// DeleteSnapshotsOptionType defines values for DeleteSnapshotsOptionType
type DeleteSnapshotsOptionType = generated.DeleteSnapshotsOptionType

const (
	DeleteSnapshotsOptionTypeInclude DeleteSnapshotsOptionType = generated.DeleteSnapshotsOptionTypeInclude
	DeleteSnapshotsOptionTypeOnly    DeleteSnapshotsOptionType = generated.DeleteSnapshotsOptionTypeOnly
)

// PossibleDeleteSnapshotsOptionTypeValues returns the possible values for the DeleteSnapshotsOptionType const type.
func PossibleDeleteSnapshotsOptionTypeValues() []DeleteSnapshotsOptionType {
	return generated.PossibleDeleteSnapshotsOptionTypeValues()
}

// AccessTier defines values for Blob Access Tier
type AccessTier = generated.AccessTier

const (
	AccessTierArchive AccessTier = generated.AccessTierArchive
	AccessTierCool    AccessTier = generated.AccessTierCool
	AccessTierHot     AccessTier = generated.AccessTierHot
	AccessTierP10     AccessTier = generated.AccessTierP10
	AccessTierP15     AccessTier = generated.AccessTierP15
	AccessTierP20     AccessTier = generated.AccessTierP20
	AccessTierP30     AccessTier = generated.AccessTierP30
	AccessTierP4      AccessTier = generated.AccessTierP4
	AccessTierP40     AccessTier = generated.AccessTierP40
	AccessTierP50     AccessTier = generated.AccessTierP50
	AccessTierP6      AccessTier = generated.AccessTierP6
	AccessTierP60     AccessTier = generated.AccessTierP60
	AccessTierP70     AccessTier = generated.AccessTierP70
	AccessTierP80     AccessTier = generated.AccessTierP80
	AccessTierPremium AccessTier = generated.AccessTierPremium
)

// PossibleAccessTierValues returns the possible values for the AccessTier const type.
func PossibleAccessTierValues() []AccessTier {
	return generated.PossibleAccessTierValues()
}

// RehydratePriority - If an object is in rehydrate pending state then this header is returned with priority of rehydrate.
// Valid values are High and Standard.
type RehydratePriority = generated.RehydratePriority

const (
	RehydratePriorityHigh     RehydratePriority = generated.RehydratePriorityHigh
	RehydratePriorityStandard RehydratePriority = generated.RehydratePriorityStandard
)

// PossibleRehydratePriorityValues returns the possible values for the RehydratePriority const type.
func PossibleRehydratePriorityValues() []RehydratePriority {
	return generated.PossibleRehydratePriorityValues()
}

// ImmutabilityPolicyMode defines values for ImmutabilityPolicyMode
type ImmutabilityPolicyMode = generated.ImmutabilityPolicyMode

const (
	ImmutabilityPolicyModeMutable  ImmutabilityPolicyMode = generated.ImmutabilityPolicyModeMutable
	ImmutabilityPolicyModeUnlocked ImmutabilityPolicyMode = generated.ImmutabilityPolicyModeUnlocked
	ImmutabilityPolicyModeLocked   ImmutabilityPolicyMode = generated.ImmutabilityPolicyModeLocked
)

// PossibleImmutabilityPolicyModeValues returns the possible values for the ImmutabilityPolicyMode const type.
func PossibleImmutabilityPolicyModeValues() []ImmutabilityPolicyMode {
	return generated.PossibleImmutabilityPolicyModeValues()
}

// ImmutabilityPolicySetting returns the possible values for the ImmutabilityPolicySetting const type.
type ImmutabilityPolicySetting = generated.ImmutabilityPolicySetting

const (
	ImmutabilityPolicySettingUnlocked ImmutabilityPolicySetting = generated.ImmutabilityPolicySettingUnlocked
	ImmutabilityPolicySettingLocked   ImmutabilityPolicySetting = generated.ImmutabilityPolicySettingLocked
)

// PossibleImmutabilityPolicySettingValues returns the possible values for the ImmutabilityPolicySetting const type.
func PossibleImmutabilityPolicySettingValues() []ImmutabilityPolicySetting {
	return generated.PossibleImmutabilityPolicySettingValues()
}

// CopyStatusType defines values for CopyStatusType
type CopyStatusType = generated.CopyStatusType

const (
	CopyStatusTypePending CopyStatusType = generated.CopyStatusTypePending
	CopyStatusTypeSuccess CopyStatusType = generated.CopyStatusTypeSuccess
	CopyStatusTypeAborted CopyStatusType = generated.CopyStatusTypeAborted
	CopyStatusTypeFailed  CopyStatusType = generated.CopyStatusTypeFailed
)

// PossibleCopyStatusTypeValues returns the possible values for the CopyStatusType const type.
func PossibleCopyStatusTypeValues() []CopyStatusType {
	return generated.PossibleCopyStatusTypeValues()
}

// EncryptionAlgorithmType defines values for EncryptionAlgorithmType
type EncryptionAlgorithmType = generated.EncryptionAlgorithmType

const (
	EncryptionAlgorithmTypeNone   EncryptionAlgorithmType = generated.EncryptionAlgorithmTypeNone
	EncryptionAlgorithmTypeAES256 EncryptionAlgorithmType = generated.EncryptionAlgorithmTypeAES256
)

// PossibleEncryptionAlgorithmTypeValues returns the possible values for the EncryptionAlgorithmType const type.
func PossibleEncryptionAlgorithmTypeValues() []EncryptionAlgorithmType {
	return generated.PossibleEncryptionAlgorithmTypeValues()
}

// ArchiveStatus defines values for ArchiveStatus
type ArchiveStatus = generated.ArchiveStatus

const (
	ArchiveStatusRehydratePendingToCool ArchiveStatus = generated.ArchiveStatusRehydratePendingToCool
	ArchiveStatusRehydratePendingToHot  ArchiveStatus = generated.ArchiveStatusRehydratePendingToHot
)

// PossibleArchiveStatusValues returns the possible values for the ArchiveStatus const type.
func PossibleArchiveStatusValues() []ArchiveStatus {
	return generated.PossibleArchiveStatusValues()
}

// DeleteType defines values for DeleteType
type DeleteType = generated.DeleteType

const (
	DeleteTypeNone      DeleteType = generated.DeleteTypeNone
	DeleteTypePermanent DeleteType = generated.DeleteTypePermanent
)

// PossibleDeleteTypeValues returns the possible values for the DeleteType const type.
func PossibleDeleteTypeValues() []DeleteType {
	return generated.PossibleDeleteTypeValues()
}

// ExpiryOptions defines values for ExpiryOptions
type ExpiryOptions = generated.ExpiryOptions

const (
	ExpiryOptionsAbsolute           ExpiryOptions = generated.ExpiryOptionsAbsolute
	ExpiryOptionsNeverExpire        ExpiryOptions = generated.ExpiryOptionsNeverExpire
	ExpiryOptionsRelativeToCreation ExpiryOptions = generated.ExpiryOptionsRelativeToCreation
	ExpiryOptionsRelativeToNow      ExpiryOptions = generated.ExpiryOptionsRelativeToNow
)

// PossibleExpiryOptionsValues returns the possible values for the ExpiryOptions const type.
func PossibleExpiryOptionsValues() []ExpiryOptions {
	return generated.PossibleExpiryOptionsValues()
}

// QueryFormatType - The quick query format type.
type QueryFormatType = generated.QueryFormatType

const (
	QueryFormatTypeDelimited QueryFormatType = generated.QueryFormatTypeDelimited
	QueryFormatTypeJSON      QueryFormatType = generated.QueryFormatTypeJSON
	QueryFormatTypeArrow     QueryFormatType = generated.QueryFormatTypeArrow
	QueryFormatTypeParquet   QueryFormatType = generated.QueryFormatTypeParquet
)

// PossibleQueryFormatTypeValues returns the possible values for the QueryFormatType const type.
func PossibleQueryFormatTypeValues() []QueryFormatType {
	return generated.PossibleQueryFormatTypeValues()
}

// LeaseDurationType defines values for LeaseDurationType
type LeaseDurationType = generated.LeaseDurationType

const (
	LeaseDurationTypeInfinite LeaseDurationType = generated.LeaseDurationTypeInfinite
	LeaseDurationTypeFixed    LeaseDurationType = generated.LeaseDurationTypeFixed
)

// PossibleLeaseDurationTypeValues returns the possible values for the LeaseDurationType const type.
func PossibleLeaseDurationTypeValues() []LeaseDurationType {
	return generated.PossibleLeaseDurationTypeValues()
}

// LeaseStateType defines values for LeaseStateType
type LeaseStateType = generated.LeaseStateType

const (
	LeaseStateTypeAvailable LeaseStateType = generated.LeaseStateTypeAvailable
	LeaseStateTypeLeased    LeaseStateType = generated.LeaseStateTypeLeased
	LeaseStateTypeExpired   LeaseStateType = generated.LeaseStateTypeExpired
	LeaseStateTypeBreaking  LeaseStateType = generated.LeaseStateTypeBreaking
	LeaseStateTypeBroken    LeaseStateType = generated.LeaseStateTypeBroken
)

// PossibleLeaseStateTypeValues returns the possible values for the LeaseStateType const type.
func PossibleLeaseStateTypeValues() []LeaseStateType {
	return generated.PossibleLeaseStateTypeValues()
}

// LeaseStatusType defines values for LeaseStatusType
type LeaseStatusType = generated.LeaseStatusType

const (
	LeaseStatusTypeLocked   LeaseStatusType = generated.LeaseStatusTypeLocked
	LeaseStatusTypeUnlocked LeaseStatusType = generated.LeaseStatusTypeUnlocked
)

// PossibleLeaseStatusTypeValues returns the possible values for the LeaseStatusType const type.
func PossibleLeaseStatusTypeValues() []LeaseStatusType {
	return generated.PossibleLeaseStatusTypeValues()
}
