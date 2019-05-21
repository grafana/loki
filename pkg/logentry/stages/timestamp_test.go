package stages

import (
	"testing"
	"time"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
)

var (
	source1 = "source1"
	rfc3339 = time.RFC3339
	custom  = "2006-01-23"
)

func TestTimestampValidation(t *testing.T) {
	tests := map[string]struct {
		config         *TimestampConfig
		err            error
		expectedFormat string
	}{
		"missing config": {
			config: nil,
			err:    errors.New(ErrEmptyTimestampStageConfig),
		},
		"missing source": {
			config: &TimestampConfig{},
			err:    errors.New(ErrTimestampSourceRequired),
		},
		"missing format": {
			config: &TimestampConfig{
				Source: &source1,
			},
			err: errors.New(ErrTimestampFormatRequired),
		},
		"standard format": {
			config: &TimestampConfig{
				Source: &source1,
				Format: &rfc3339,
			},
			err:            nil,
			expectedFormat: time.RFC3339,
		},
		"custom format": {
			config: &TimestampConfig{
				Source: &source1,
				Format: &custom,
			},
			err:            nil,
			expectedFormat: "2006-01-23",
		},
	}
	for name, test := range tests {
		test := test
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			format, err := validateTimestampConfig(test.config)
			if (err != nil) != (test.err != nil) {
				t.Errorf("validateOutputConfig() expected error = %v, actual error = %v", test.err, err)
				return
			}
			if (err != nil) && (err.Error() != test.err.Error()) {
				t.Errorf("validateOutputConfig() expected error = %v, actual error = %v", test.err, err)
				return
			}
			if test.expectedFormat != "" {
				assert.Equal(t, test.expectedFormat, format)
			}
		})
	}
}

//TODO process tests
