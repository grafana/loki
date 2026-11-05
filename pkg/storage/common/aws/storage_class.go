package aws

import (
	"fmt"
	"strings"

	s3_types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/thanos-io/objstore/providers/s3"

	"github.com/grafana/loki/v3/pkg/util"
)

var (
	SupportedSSETypes       = []string{s3.SSEKMS, s3.SSES3}
	SupportedStorageClasses = []s3_types.ObjectStorageClass{
		s3_types.ObjectStorageClassStandard,
		s3_types.ObjectStorageClassStandardIa,
	}
)

func ToStringSlice[T ~string](in []T) []string {
	var out []string
	for _, o := range in {
		out = append(out, string(o))
	}
	return out
}

func ValidateStorageClass(storageClass string) error {
	storageClasses := ToStringSlice(SupportedStorageClasses)
	if !util.StringsContain(storageClasses, storageClass) {
		return fmt.Errorf("unsupported S3 storage class: %s. Supported values: %s", storageClass, strings.Join(storageClasses, ", "))
	}

	return nil
}
