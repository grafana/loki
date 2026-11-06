package aws

import (
	"fmt"
	"strings"

	s3_types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/thanos-io/objstore/providers/s3"

	"github.com/grafana/loki/v4/pkg/util"
)

var (
	SupportedSSETypes       = []string{s3.SSEKMS, s3.SSES3}
	SupportedStorageClasses = []s3_types.ObjectStorageClass{
		s3_types.ObjectStorageClassStandard,
		s3_types.ObjectStorageClassStandardIa,
		s3_types.ObjectStorageClassReducedRedundancy,
	}
)

// StorageClassUsage returns the description of the supported storage classes for use in flag help texts.
func StorageClassUsage() string {
	return fmt.Sprintf(
		"Supported values are: %s. %s is only intended for use with MinIO.",
		strings.Join(ToStringSlice(SupportedStorageClasses), ", "),
		s3_types.ObjectStorageClassReducedRedundancy,
	)
}

func ToStringSlice[T ~string](in []T) []string {
	var out []string
	for _, o := range in {
		out = append(out, string(o))
	}
	return out
}

func ValidateStorageClass(storageClass string) error {
	if storageClass == "" {
		return nil
	}
	storageClasses := ToStringSlice(SupportedStorageClasses)
	if !util.StringsContain(storageClasses, storageClass) {
		return ErrUnsupportedStorageClass(storageClass)
	}
	return nil
}
