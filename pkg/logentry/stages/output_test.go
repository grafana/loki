package stages

import (
	"testing"

	"github.com/pkg/errors"
)

func TestOutputValidation(t *testing.T) {
	tests := map[string]struct {
		config *OutputConfig
		err    error
	}{
		"missing config": {
			config: nil,
			err:    errors.New(ErrEmptyOutputStageConfig),
		},
		"missing source": {
			config: &OutputConfig{
				Source: nil,
			},
			err: errors.New(ErrOutputSourceRequired),
		},
	}
	for name, test := range tests {
		test := test
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := validateOutputConfig(test.config)
			if (err != nil) != (test.err != nil) {
				t.Errorf("validateOutputConfig() expected error = %v, actual error = %v", test.err, err)
				return
			}
			if (err != nil) && (err.Error() != test.err.Error()) {
				t.Errorf("validateOutputConfig() expected error = %v, actual error = %v", test.err, err)
				return
			}
		})
	}
}

//TODO test label processing
