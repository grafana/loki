package aws

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateStorageClass(t *testing.T) {
	tests := map[string]struct {
		storageClass  string
		expectedError error
	}{
		"should return error if storage class is invalid": {
			"foo",
			fmt.Errorf("unsupported S3 storage class: foo. Supported values: %s", strings.Join(SupportedStorageClasses, ", ")),
		},
		"should not return error if storage class is is within supported values": {
			StorageClassStandardInfrequentAccess,
			nil,
		},
	}

	for name, test := range tests {
		actual := ValidateStorageClass(test.storageClass)
		assert.Equal(t, test.expectedError, actual, name)
	}
}
