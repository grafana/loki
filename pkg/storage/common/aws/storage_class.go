package aws

import (
	"fmt"
	"slices"
	"strings"

	s3_types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

const (

	// S3 Storage Class options which define the data access, resiliency & cost requirements of objects
	// https://docs.aws.amazon.com/AmazonS3/latest/API/API_PutObject.html#API_PutObject_RequestSyntax
	StorageClassGlacier                  = string(s3_types.StorageClassGlacier)
	StorageClassDeepArchive              = string(s3_types.StorageClassDeepArchive)
	StorageClassGlacierInstantRetrieval  = string(s3_types.StorageClassGlacierIr)
	StorageClassIntelligentTiering       = string(s3_types.StorageClassIntelligentTiering)
	StorageClassOneZoneInfrequentAccess  = string(s3_types.StorageClassOnezoneIa)
	StorageClassOutposts                 = string(s3_types.StorageClassOutposts)
	StorageClassReducedRedundancy        = string(s3_types.StorageClassReducedRedundancy)
	StorageClassStandard                 = string(s3_types.StorageClassStandard)
	StorageClassStandardInfrequentAccess = string(s3_types.StorageClassStandardIa)

	// StorageClassExpressOneZone is only valid for S3 Express One Zone directory
	// buckets, which in turn accept no other storage class.
	StorageClassExpressOneZone = string(s3_types.StorageClassExpressOnezone)
)

// SupportedStorageClasses is derived from the storage class enum of the AWS SDK
// PutObject API so that newly released storage classes are picked up on SDK
// upgrades instead of having to be added here by hand. This mirrors what the
// thanos-objstore based S3 client in pkg/storage/bucket/s3 does.
var SupportedStorageClasses = supportedStorageClasses()

func supportedStorageClasses() []string {
	values := s3_types.StorageClassStandard.Values()

	out := make([]string, 0, len(values))
	for _, v := range values {
		out = append(out, string(v))
	}

	// The SDK makes no guarantee about the ordering of Values(), so sort to keep
	// the flag help text and the generated docs stable across SDK upgrades.
	slices.Sort(out)
	return out
}

func ValidateStorageClass(storageClass string) error {
	if !slices.Contains(SupportedStorageClasses, storageClass) {
		return fmt.Errorf("unsupported S3 storage class: %s. Supported values: %s", storageClass, strings.Join(SupportedStorageClasses, ", "))
	}

	return nil
}
