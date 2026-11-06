package aws

import (
	"testing"

	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/stretchr/testify/assert"
)

func TestValidateStorageClass(t *testing.T) {
	tests := map[string]struct {
		storageClass  string
		expectedError error
	}{
		"should return error if storage class is invalid": {
			"foo",
			ErrUnsupportedStorageClass("foo"),
		},
		"should not return error if storage class is within supported values": {
			string(s3types.StorageClassStandardIa),
			nil,
		},
	}

	for name, test := range tests {
		actual := ValidateStorageClass(test.storageClass)
		assert.Equal(t, test.expectedError, actual, name)
	}
}
