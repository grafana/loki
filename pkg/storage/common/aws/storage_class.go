package aws

import (
	"fmt"
	"strings"

	"github.com/grafana/loki/v3/pkg/util"
)

const (

	// S3 Storage Class options which define the data access, resiliency & cost requirements of objects
	// https://docs.aws.amazon.com/AmazonS3/latest/API/API_PutObject.html#API_PutObject_RequestSyntax
	StorageClassGlacier                  = "GLACIER"
	StorageClassDeepArchive              = "DEEP_ARCHIVE"
	StorageClassGlacierInstantRetrieval  = "GLACIER_IR"
	StorageClassIntelligentTiering       = "INTELLIGENT_TIERING"
	StorageClassOneZoneInfrequentAccess  = "ONEZONE_IA"
	StorageClassOutposts                 = "OUTPOSTS"
	StorageClassReducedRedundancy        = "REDUCED_REDUNDANCY"
	StorageClassStandard                 = "STANDARD"
	StorageClassStandardInfrequentAccess = "STANDARD_IA"
)

var (
	SupportedStorageClasses = []string{StorageClassGlacier, StorageClassDeepArchive, StorageClassGlacierInstantRetrieval, StorageClassIntelligentTiering, StorageClassOneZoneInfrequentAccess, StorageClassOutposts, StorageClassReducedRedundancy, StorageClassStandard, StorageClassStandardInfrequentAccess}
)

func ValidateStorageClass(storageClass string) error {
	if !util.StringsContain(SupportedStorageClasses, storageClass) {
		return fmt.Errorf("unsupported S3 storage class: %s. Supported values: %s", storageClass, strings.Join(SupportedStorageClasses, ", "))
	}

	return nil
}
