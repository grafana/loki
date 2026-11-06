package aws

import (
	"errors"
	"fmt"
	"strings"
)

var (
	ErrUnsupportedSSEType    = errors.New("unsupported S3 SSE type")
	ErrInvalidSSEContext     = errors.New("invalid S3 SSE encryption context")
	ErrInvalidEndpointPrefix = errors.New("the endpoint must not prefixed with the bucket name")
	ErrInvalidSTSEndpoint    = errors.New("sts-endpoint must be a valid url")
)

func ErrUnsupportedStorageClass(storageClass string) error {
	return fmt.Errorf(
		"unsupported S3 storage class: %s (supported values: %s)",
		storageClass,
		strings.Join(ToStringSlice(SupportedStorageClasses), ", "),
	)
}
